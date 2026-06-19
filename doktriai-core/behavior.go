package core

import (
	"math"
	"sync"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

const (
	// Thresholds for anomaly detection — based on rolling 5-minute window.
	deployRateThreshold = 20  // > 20 deploys in 5 min = anomalous
	deleteRateThreshold = 10  // > 10 deletes in 5 min = anomalous
	rejectRateThreshold = 5   // > 5 policy rejects in 5 min = likely probing
	behaviorWindow      = 5 * time.Minute
)

// actionEvent is a timestamped action for rolling window calculations.
type actionEvent struct {
	action string
	at     time.Time
}

// actorState holds per-actor rolling event history and computed metrics.
type actorState struct {
	events []actionEvent
}

// BehaviorTracker maintains per-actor rolling behavioral metrics
// and flags actors whose behavior deviates from safe thresholds (ASI10 — Rogue Agent Detection).
type BehaviorTracker struct {
	mu     sync.Mutex
	actors map[string]*actorState
}

// NewBehaviorTracker creates an initialized BehaviorTracker.
func NewBehaviorTracker() *BehaviorTracker {
	return &BehaviorTracker{actors: make(map[string]*actorState)}
}

// Record registers a new action for the given actor.
func (bt *BehaviorTracker) Record(actor, action string) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if _, ok := bt.actors[actor]; !ok {
		bt.actors[actor] = &actorState{}
	}
	bt.actors[actor].events = append(bt.actors[actor].events, actionEvent{action: action, at: time.Now().UTC()})
}

// IsAnomalous returns true if the actor's behavior in the rolling window
// exceeds safe thresholds. Also returns an anomaly score (0.0 = normal, >1.0 = flagged).
func (bt *BehaviorTracker) IsAnomalous(actor string) (bool, float64) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	state, ok := bt.actors[actor]
	if !ok {
		return false, 0.0
	}

	cutoff := time.Now().UTC().Add(-behaviorWindow)
	var deploys, deletes, rejects int
	pruned := state.events[:0]
	for _, e := range state.events {
		if e.at.After(cutoff) {
			pruned = append(pruned, e)
			switch e.action {
			case "apply", "deploy":
				deploys++
			case "delete":
				deletes++
			case "reject", "policy_block":
				rejects++
			}
		}
	}
	state.events = pruned

	// Compute composite anomaly score as max normalized ratio
	score := math.Max(
		math.Max(
			float64(deploys)/float64(deployRateThreshold),
			float64(deletes)/float64(deleteRateThreshold),
		),
		float64(rejects)/float64(rejectRateThreshold),
	)

	return score > 1.0, score
}

// Metrics returns a snapshot of the current BehaviorMetric for an actor.
func (bt *BehaviorTracker) Metrics(actor string) packages.BehaviorMetric {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	state, ok := bt.actors[actor]
	m := packages.BehaviorMetric{Actor: actor}
	if !ok {
		return m
	}

	cutoff := time.Now().UTC().Add(-behaviorWindow)
	var deploys, deletes, rejects int64
	var lastSeen time.Time
	for _, e := range state.events {
		if e.at.After(cutoff) {
			if e.at.After(lastSeen) {
				lastSeen = e.at
			}
			switch e.action {
			case "apply", "deploy":
				deploys++
			case "delete":
				deletes++
			case "reject", "policy_block":
				rejects++
			}
		}
	}

	score := math.Max(
		math.Max(
			float64(deploys)/float64(deployRateThreshold),
			float64(deletes)/float64(deleteRateThreshold),
		),
		float64(rejects)/float64(rejectRateThreshold),
	)

	m.DeployCount = deploys
	m.DeleteCount = deletes
	m.RejectCount = rejects
	m.LastSeen = lastSeen
	m.AnomalyScore = math.Round(score*100) / 100
	m.Flagged = score > 1.0
	return m
}

// AllMetrics returns behavioral snapshots for all tracked actors.
func (bt *BehaviorTracker) AllMetrics() []packages.BehaviorMetric {
	bt.mu.Lock()
	actors := make([]string, 0, len(bt.actors))
	for a := range bt.actors {
		actors = append(actors, a)
	}
	bt.mu.Unlock()

	out := make([]packages.BehaviorMetric, 0, len(actors))
	for _, a := range actors {
		out = append(out, bt.Metrics(a))
	}
	return out
}
