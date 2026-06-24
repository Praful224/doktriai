package core

import (
	"testing"
	"time"
)

// ─── BehaviorTracker Record & Metrics ─────────────────────────────────────────

func TestBehaviorTracker_Record(t *testing.T) {
	bt := NewBehaviorTracker()
	bt.Record("alice", "apply")
	bt.Record("alice", "apply")
	bt.Record("alice", "delete")

	m := bt.Metrics("alice")
	if m.DeployCount != 2 {
		t.Errorf("expected 2 deploys, got %d", m.DeployCount)
	}
	if m.DeleteCount != 1 {
		t.Errorf("expected 1 delete, got %d", m.DeleteCount)
	}
	if m.Actor != "alice" {
		t.Errorf("expected actor 'alice', got %q", m.Actor)
	}
}

func TestBehaviorTracker_Metrics_UnknownActor(t *testing.T) {
	bt := NewBehaviorTracker()
	m := bt.Metrics("unknown")
	if m.DeployCount != 0 || m.DeleteCount != 0 || m.Flagged {
		t.Error("expected zero metrics for unknown actor")
	}
}

// ─── Anomaly Detection ────────────────────────────────────────────────────────

func TestBehaviorTracker_NoAnomaly(t *testing.T) {
	bt := NewBehaviorTracker()
	for i := 0; i < 5; i++ {
		bt.Record("normal-user", "apply")
	}
	anomalous, score := bt.IsAnomalous("normal-user")
	if anomalous {
		t.Errorf("expected no anomaly for 5 deploys, score=%.2f", score)
	}
}

func TestBehaviorTracker_DeployAnomaly(t *testing.T) {
	bt := NewBehaviorTracker()
	// 21 deploys exceeds threshold of 20
	for i := 0; i < 21; i++ {
		bt.Record("flood-bot", "apply")
	}
	anomalous, score := bt.IsAnomalous("flood-bot")
	if !anomalous {
		t.Errorf("expected anomaly for 21 deploys, score=%.2f", score)
	}
	if score <= 1.0 {
		t.Errorf("expected score > 1.0, got %.2f", score)
	}
}

func TestBehaviorTracker_DeleteAnomaly(t *testing.T) {
	bt := NewBehaviorTracker()
	// 11 deletes exceeds threshold of 10
	for i := 0; i < 11; i++ {
		bt.Record("rogue-agent", "delete")
	}
	anomalous, _ := bt.IsAnomalous("rogue-agent")
	if !anomalous {
		t.Error("expected anomaly for 11 deletes")
	}
}

func TestBehaviorTracker_RejectAnomaly(t *testing.T) {
	bt := NewBehaviorTracker()
	// 6 rejects exceeds threshold of 5
	for i := 0; i < 6; i++ {
		bt.Record("probing-agent", "reject")
	}
	anomalous, _ := bt.IsAnomalous("probing-agent")
	if !anomalous {
		t.Error("expected anomaly for 6 rejects")
	}
}

func TestBehaviorTracker_ThresholdBoundary(t *testing.T) {
	bt := NewBehaviorTracker()
	// Exactly at threshold (20 deploys) should NOT be anomalous (score = 1.0, not > 1.0)
	for i := 0; i < 20; i++ {
		bt.Record("edge-case", "apply")
	}
	anomalous, score := bt.IsAnomalous("edge-case")
	if anomalous {
		t.Errorf("expected no anomaly at exactly threshold, score=%.2f", score)
	}
}

// ─── Window Pruning ───────────────────────────────────────────────────────────

func TestBehaviorTracker_WindowPruning(t *testing.T) {
	bt := NewBehaviorTracker()

	// Record events that would be outside the 5-min window
	bt.mu.Lock()
	bt.actors["old-actor"] = &actorState{
		events: []actionEvent{
			{action: "apply", at: time.Now().Add(-10 * time.Minute)},
			{action: "apply", at: time.Now().Add(-10 * time.Minute)},
		},
	}
	bt.mu.Unlock()

	// IsAnomalous triggers pruning
	anomalous, _ := bt.IsAnomalous("old-actor")
	if anomalous {
		t.Error("expected old events to be pruned, no anomaly")
	}

	m := bt.Metrics("old-actor")
	if m.DeployCount != 0 {
		t.Errorf("expected 0 deploys after pruning, got %d", m.DeployCount)
	}
}

// ─── AllMetrics ───────────────────────────────────────────────────────────────

func TestBehaviorTracker_AllMetrics(t *testing.T) {
	bt := NewBehaviorTracker()
	bt.Record("alice", "apply")
	bt.Record("bob", "delete")
	bt.Record("charlie", "reject")

	all := bt.AllMetrics()
	if len(all) != 3 {
		t.Errorf("expected 3 actors, got %d", len(all))
	}

	actors := make(map[string]bool)
	for _, m := range all {
		actors[m.Actor] = true
	}
	for _, expected := range []string{"alice", "bob", "charlie"} {
		if !actors[expected] {
			t.Errorf("expected actor %q in AllMetrics", expected)
		}
	}
}

// ─── AnomalyScore ─────────────────────────────────────────────────────────────

func TestBehaviorTracker_AnomalyScoreCalculation(t *testing.T) {
	bt := NewBehaviorTracker()
	// 10 deploys / 20 threshold = 0.5 score
	for i := 0; i < 10; i++ {
		bt.Record("half-rate", "apply")
	}
	m := bt.Metrics("half-rate")
	if m.AnomalyScore != 0.5 {
		t.Errorf("expected anomaly score 0.5, got %.2f", m.AnomalyScore)
	}
	if m.Flagged {
		t.Error("expected not flagged at 0.5 score")
	}
}
