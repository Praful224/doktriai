package core

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestLeaderElection_TakeoverAndExpiry(t *testing.T) {
	// Create temp file DB to avoid connection isolation in memory DBs
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()

	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	// Instance A
	os.Setenv("DOKTRIAI_NODE_ID", "node-A")
	leA := NewLeaderElection(db)
	leA.interval = 100 * time.Millisecond
	leA.leaseTTL = 300 * time.Millisecond

	// Instance B
	os.Setenv("DOKTRIAI_NODE_ID", "node-B")
	leB := NewLeaderElection(db)
	leB.interval = 100 * time.Millisecond
	leB.leaseTTL = 300 * time.Millisecond

	startedA := make(chan bool, 1)
	stoppedA := make(chan bool, 1)
	leA.Start(ctxA, func() { startedA <- true }, func() { stoppedA <- true })

	// Wait for A to acquire leadership
	select {
	case <-startedA:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("Node A failed to acquire leadership in time")
	}

	if !leA.IsLeader() {
		t.Error("expected node A to be leader")
	}

	// Start B
	startedB := make(chan bool, 1)
	stoppedB := make(chan bool, 1)
	leB.Start(ctxB, func() { startedB <- true }, func() { stoppedB <- true })

	// Give it some time: B should NOT acquire leadership since A is active
	time.Sleep(200 * time.Millisecond)
	if leB.IsLeader() {
		t.Error("expected node B to NOT be leader while A is actively renewing")
	}

	// Cancel A's context and release to simulate crash
	cancelA()
	leA.release()

	// A released leadership. Let's wait for B to take over
	select {
	case <-startedB:
		// success
	case <-time.After(1 * time.Second):
		t.Fatal("Node B failed to take over leadership after A released")
	}

	if !leB.IsLeader() {
		t.Error("expected node B to become the leader")
	}
}
