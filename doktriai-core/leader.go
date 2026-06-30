package core

import (
	"context"
	"database/sql"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// LeaderElection manages the active-passive lease status of the control plane node.
type LeaderElection struct {
	mu       sync.RWMutex
	db       *sql.DB
	nodeID   string
	isLeader bool
	interval time.Duration
	leaseTTL time.Duration
}

// NewLeaderElection constructs a lease manager for this node.
func NewLeaderElection(db *sql.DB) *LeaderElection {
	nodeID := os.Getenv("DOKTRIAI_NODE_ID")
	if nodeID == "" {
		nodeID = uuid.New().String()
	}
	return &LeaderElection{
		db:       db,
		nodeID:   nodeID,
		interval: 1 * time.Second,
		leaseTTL: 3 * time.Second,
	}
}

// Start spawns the background lease renewal loop.
func (le *LeaderElection) Start(ctx context.Context, onStartLeadership, onStopLeadership func()) {
	// Ensure election table exists
	_, _ = le.db.Exec(`
		CREATE TABLE IF NOT EXISTS leader_election (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			leader_id TEXT NOT NULL,
			expires_at DATETIME NOT NULL
		);
	`)

	go le.loop(ctx, onStartLeadership, onStopLeadership)
}

// IsLeader checks if this instance is the active cluster leader.
func (le *LeaderElection) IsLeader() bool {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.isLeader
}

// NodeID returns the node's unique identification string.
func (le *LeaderElection) NodeID() string {
	return le.nodeID
}

func (le *LeaderElection) loop(ctx context.Context, onStart, onStop func()) {
	ticker := time.NewTicker(le.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			le.mu.Lock()
			if le.isLeader {
				le.release()
				if onStop != nil {
					onStop()
				}
			}
			le.mu.Unlock()
			return
		case <-ticker.C:
			le.mu.Lock()
			wasLeader := le.isLeader
			now := time.Now().UTC()
			expiresAt := now.Add(le.leaseTTL)

			tx, err := le.db.Begin()
			if err != nil {
				log.Printf("leader election: failed to start transaction: %v", err)
				le.mu.Unlock()
				continue
			}

			var currentLeader string
			var currentExpires time.Time
			row := tx.QueryRow(`SELECT leader_id, expires_at FROM leader_election WHERE id = 1`)
			err = row.Scan(&currentLeader, &currentExpires)

			acquired := false
			if err == sql.ErrNoRows {
				_, err = tx.Exec(`INSERT INTO leader_election (id, leader_id, expires_at) VALUES (1, ?, ?)`, le.nodeID, expiresAt)
				if err == nil {
					acquired = true
				}
			} else if err == nil {
				if currentLeader == le.nodeID {
					_, err = tx.Exec(`UPDATE leader_election SET expires_at = ? WHERE id = 1`, expiresAt)
					if err == nil {
						acquired = true
					}
				} else if now.After(currentExpires) {
					_, err = tx.Exec(`UPDATE leader_election SET leader_id = ?, expires_at = ? WHERE id = 1`, le.nodeID, expiresAt)
					if err == nil {
						acquired = true
					}
				}
			}

			if err != nil {
				_ = tx.Rollback()
				log.Printf("leader election: database transaction error: %v", err)
				le.mu.Unlock()
				continue
			}

			_ = tx.Commit()
			le.isLeader = acquired

			if le.isLeader && !wasLeader {
				log.Printf("Node %s acquired cluster leadership!", le.nodeID)
				if onStart != nil {
					go onStart()
				}
			} else if !le.isLeader && wasLeader {
				log.Printf("Node %s lost cluster leadership!", le.nodeID)
				if onStop != nil {
					go onStop()
				}
			}
			le.mu.Unlock()
		}
	}
}

func (le *LeaderElection) release() {
	_, _ = le.db.Exec(`DELETE FROM leader_election WHERE id = 1 AND leader_id = ?`, le.nodeID)
	le.isLeader = false
}
