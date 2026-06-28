package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/praful224/doktriai/doktriai-packages"
)

type SQLiteStorage struct {
	db *sql.DB
}

func NewSQLiteStorage(db *sql.DB) *SQLiteStorage {
	return &SQLiteStorage{db: db}
}

// Migrate executes SQLite schema migrations.
func (s *SQLiteStorage) Migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS workloads (
			name TEXT PRIMARY KEY,
			image TEXT NOT NULL,
			replicas INTEGER NOT NULL CHECK (replicas >= 0 AND replicas <= 100),
			port INTEGER NOT NULL CHECK (port >= 0 AND port <= 65535),
			container_port INTEGER NOT NULL CHECK (container_port >= 0 AND container_port <= 65535),
			runtime TEXT NOT NULL,
			env TEXT NOT NULL DEFAULT '{}',
			resources TEXT NOT NULL DEFAULT '{}',
			volumes TEXT NOT NULL DEFAULT '[]',
			labels TEXT NOT NULL DEFAULT '{}',
			security_mode TEXT NOT NULL DEFAULT 'dev',
			deploy_strategy TEXT NOT NULL DEFAULT 'recreate',
			max_surge INTEGER NOT NULL DEFAULT 1,
			max_unavailable INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS audit_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			seq_id INTEGER UNIQUE NOT NULL,
			time DATETIME NOT NULL,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			workload TEXT NOT NULL,
			allowed INTEGER NOT NULL,
			reason TEXT,
			plan_id TEXT,
			state_hash_before TEXT,
			state_hash_after TEXT,
			agent_id TEXT,
			agent_scope TEXT,
			agent_goal TEXT,
			signature_verified INTEGER DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS environment_locks (
			id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			locked INTEGER NOT NULL DEFAULT 0,
			acquired_by TEXT,
			acquired_at DATETIME,
			reason TEXT
		);`,
		`INSERT OR IGNORE INTO environment_locks (id, locked) VALUES (1, 0);`,
		`CREATE TABLE IF NOT EXISTS pte_plans (
			id TEXT PRIMARY KEY,
			requested_by TEXT NOT NULL,
			agent_id TEXT,
			agent_goal TEXT,
			spec TEXT NOT NULL,
			approval_reason TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			approved_by TEXT,
			approved_at DATETIME,
			rejected_by TEXT,
			rejected_at DATETIME,
			rejection_comment TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			time DATETIME NOT NULL,
			level TEXT NOT NULL,
			source TEXT NOT NULL,
			workload TEXT,
			message TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS workload_history (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			spec_json   TEXT NOT NULL,
			applied_by  TEXT NOT NULL DEFAULT '',
			applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, q := range queries {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("sqlite migration query failed: %w", err)
		}
	}
	
	// Upgrade existing database columns if they don't exist
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE workloads ADD COLUMN deploy_strategy TEXT DEFAULT 'recreate'")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE workloads ADD COLUMN max_surge INTEGER DEFAULT 1")
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE workloads ADD COLUMN max_unavailable INTEGER DEFAULT 0")

	// Seed initial workloads if table is empty
	var count int
	_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workloads").Scan(&count)
	if count == 0 {
		_, _ = s.db.ExecContext(ctx, `INSERT INTO workloads (name, image, replicas, port, container_port, runtime, env, resources, volumes, labels, security_mode, deploy_strategy, max_surge, max_unavailable) VALUES 
		('secure-ingress', 'nginx:alpine', 2, 8080, 80, 'docker', '{}', '{}', '[]', '{}', 'dev', 'recreate', 1, 0),
		('reconciler-daemon', 'busybox:latest', 1, 0, 0, 'docker', '{}', '{}', '[]', '{}', 'dev', 'recreate', 1, 0),
		('agent-gateway', 'python:3.11-alpine', 1, 9000, 9000, 'docker', '{}', '{}', '[]', '{}', 'dev', 'recreate', 1, 0)`)
	}

	return nil
}

// --- Workloads ---

func (s *SQLiteStorage) ListWorkloads(ctx context.Context) ([]packages.WorkloadSpec, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name, image, replicas, port, container_port, runtime, env, resources, volumes, labels, security_mode, deploy_strategy, max_surge, max_unavailable FROM workloads ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var specs []packages.WorkloadSpec
	for rows.Next() {
		var spec packages.WorkloadSpec
		var envJSON, resJSON, volJSON, lblJSON string
		err := rows.Scan(
			&spec.Name, &spec.Image, &spec.Replicas, &spec.Port, &spec.ContainerPort, &spec.Runtime,
			&envJSON, &resJSON, &volJSON, &lblJSON, &spec.SecurityMode,
			&spec.DeployStrategy, &spec.MaxSurge, &spec.MaxUnavailable,
		)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(envJSON), &spec.Env)
		_ = json.Unmarshal([]byte(resJSON), &spec.Resources)
		_ = json.Unmarshal([]byte(volJSON), &spec.Volumes)
		_ = json.Unmarshal([]byte(lblJSON), &spec.Labels)
		specs = append(specs, spec)
	}
	return specs, nil
}

func (s *SQLiteStorage) GetWorkload(ctx context.Context, name string) (packages.WorkloadSpec, bool, error) {
	var spec packages.WorkloadSpec
	var envJSON, resJSON, volJSON, lblJSON string
	err := s.db.QueryRowContext(ctx, "SELECT name, image, replicas, port, container_port, runtime, env, resources, volumes, labels, security_mode, deploy_strategy, max_surge, max_unavailable FROM workloads WHERE name = ?", name).Scan(
		&spec.Name, &spec.Image, &spec.Replicas, &spec.Port, &spec.ContainerPort, &spec.Runtime,
		&envJSON, &resJSON, &volJSON, &lblJSON, &spec.SecurityMode,
		&spec.DeployStrategy, &spec.MaxSurge, &spec.MaxUnavailable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return spec, false, nil
	}
	if err != nil {
		return spec, false, err
	}
	_ = json.Unmarshal([]byte(envJSON), &spec.Env)
	_ = json.Unmarshal([]byte(resJSON), &spec.Resources)
	_ = json.Unmarshal([]byte(volJSON), &spec.Volumes)
	_ = json.Unmarshal([]byte(lblJSON), &spec.Labels)
	return spec, true, nil
}

func (s *SQLiteStorage) PutWorkload(ctx context.Context, spec packages.WorkloadSpec) error {
	envJSON, _ := json.Marshal(spec.Env)
	resJSON, _ := json.Marshal(spec.Resources)
	volJSON, _ := json.Marshal(spec.Volumes)
	lblJSON, _ := json.Marshal(spec.Labels)

	if spec.DeployStrategy == "" {
		spec.DeployStrategy = "recreate"
	}
	if spec.MaxSurge == 0 && spec.DeployStrategy == "rolling" {
		spec.MaxSurge = 1
	}

	query := `
		INSERT INTO workloads (name, image, replicas, port, container_port, runtime, env, resources, volumes, labels, security_mode, deploy_strategy, max_surge, max_unavailable, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT (name) DO UPDATE SET
			image = EXCLUDED.image,
			replicas = EXCLUDED.replicas,
			port = EXCLUDED.port,
			container_port = EXCLUDED.container_port,
			runtime = EXCLUDED.runtime,
			env = EXCLUDED.env,
			resources = EXCLUDED.resources,
			volumes = EXCLUDED.volumes,
			labels = EXCLUDED.labels,
			security_mode = EXCLUDED.security_mode,
			deploy_strategy = EXCLUDED.deploy_strategy,
			max_surge = EXCLUDED.max_surge,
			max_unavailable = EXCLUDED.max_unavailable,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := s.db.ExecContext(ctx, query,
		spec.Name, spec.Image, spec.Replicas, spec.Port, spec.ContainerPort, spec.Runtime,
		string(envJSON), string(resJSON), string(volJSON), string(lblJSON), spec.SecurityMode,
		spec.DeployStrategy, spec.MaxSurge, spec.MaxUnavailable,
	)
	return err
}

func (s *SQLiteStorage) DeleteWorkload(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM workloads WHERE name = ?", name)
	return err
}

// --- Audit ---

func (s *SQLiteStorage) AddAudit(ctx context.Context, record packages.AuditRecord) (packages.AuditRecord, error) {
	record.Time = time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return record, err
	}
	defer tx.Rollback()

	var nextSeq int64
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq_id), 0) + 1 FROM audit_records").Scan(&nextSeq)
	if err != nil {
		return record, err
	}
	record.SeqID = nextSeq

	allowedInt := 0
	if record.Allowed {
		allowedInt = 1
	}
	sigVerifiedInt := 0
	if record.SignatureVerified {
		sigVerifiedInt = 1
	}

	query := `
		INSERT INTO audit_records (seq_id, time, actor, action, workload, allowed, reason, plan_id, state_hash_before, state_hash_after, agent_id, agent_scope, agent_goal, signature_verified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	res, err := tx.ExecContext(ctx, query,
		record.SeqID, record.Time, record.Actor, record.Action, record.Workload, allowedInt,
		record.Reason, record.PlanID, record.StateHashBefore, record.StateHashAfter,
		record.AgentID, record.AgentScope, record.AgentGoal, sigVerifiedInt,
	)
	if err != nil {
		return record, err
	}
	
	record.ID, err = res.LastInsertId()
	if err != nil {
		return record, err
	}

	// Cap audit records to 500
	_, err = tx.ExecContext(ctx, `
		DELETE FROM audit_records 
		WHERE id NOT IN (
			SELECT id FROM audit_records 
			ORDER BY seq_id DESC 
			LIMIT 500
		)
	`)
	if err != nil {
		return record, err
	}

	return record, tx.Commit()
}

func (s *SQLiteStorage) ListAudit(ctx context.Context) ([]packages.AuditRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, seq_id, time, actor, action, workload, allowed, reason, plan_id, state_hash_before, state_hash_after, agent_id, agent_scope, agent_goal, signature_verified FROM audit_records ORDER BY seq_id DESC LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []packages.AuditRecord
	for rows.Next() {
		var r packages.AuditRecord
		var allowedInt, sigVerifiedInt int
		err := rows.Scan(
			&r.ID, &r.SeqID, &r.Time, &r.Actor, &r.Action, &r.Workload, &allowedInt,
			&r.Reason, &r.PlanID, &r.StateHashBefore, &r.StateHashAfter,
			&r.AgentID, &r.AgentScope, &r.AgentGoal, &sigVerifiedInt,
		)
		if err != nil {
			return nil, err
		}
		r.Allowed = allowedInt != 0
		r.SignatureVerified = sigVerifiedInt != 0
		records = append(records, r)
	}
	return records, nil
}

func (s *SQLiteStorage) GetAuditSince(ctx context.Context, sinceSeq int64) ([]packages.AuditRecord, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, seq_id, time, actor, action, workload, allowed, reason, plan_id, state_hash_before, state_hash_after, agent_id, agent_scope, agent_goal, signature_verified FROM audit_records WHERE seq_id >= ? ORDER BY seq_id DESC", sinceSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []packages.AuditRecord
	for rows.Next() {
		var r packages.AuditRecord
		var allowedInt, sigVerifiedInt int
		err := rows.Scan(
			&r.ID, &r.SeqID, &r.Time, &r.Actor, &r.Action, &r.Workload, &allowedInt,
			&r.Reason, &r.PlanID, &r.StateHashBefore, &r.StateHashAfter,
			&r.AgentID, &r.AgentScope, &r.AgentGoal, &sigVerifiedInt,
		)
		if err != nil {
			return nil, err
		}
		r.Allowed = allowedInt != 0
		r.SignatureVerified = sigVerifiedInt != 0
		records = append(records, r)
	}
	return records, nil
}

// --- Environment Lock ---

func (s *SQLiteStorage) GetLock(ctx context.Context) (packages.LockState, error) {
	var l packages.LockState
	var lockedInt int
	var acqBy, reason sql.NullString
	var acqAt sql.NullTime

	err := s.db.QueryRowContext(ctx, "SELECT locked, acquired_by, acquired_at, reason FROM environment_locks WHERE id = 1").Scan(
		&lockedInt, &acqBy, &acqAt, &reason,
	)
	if err != nil {
		return l, err
	}
	l.Locked = lockedInt != 0
	if acqBy.Valid {
		l.AcquiredBy = acqBy.String
	}
	if acqAt.Valid {
		l.Time = acqAt.Time
	}
	if reason.Valid {
		l.Reason = reason.String
	}
	return l, nil
}

func (s *SQLiteStorage) AcquireLock(ctx context.Context, actor, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE environment_locks 
		SET locked = 1, acquired_by = ?, acquired_at = CURRENT_TIMESTAMP, reason = ? 
		WHERE id = 1
	`, actor, reason)
	return err
}

func (s *SQLiteStorage) ReleaseLock(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE environment_locks 
		SET locked = 0, acquired_by = NULL, acquired_at = NULL, reason = NULL 
		WHERE id = 1
	`)
	return err
}

// --- PTE Plans ---

func (s *SQLiteStorage) SubmitPlan(ctx context.Context, plan *packages.PendingPlan) error {
	specJSON, _ := json.Marshal(plan.Spec)
	query := `
		INSERT INTO pte_plans (id, requested_by, agent_id, agent_goal, spec, approval_reason, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		plan.ID, plan.RequestedBy, plan.AgentID, plan.AgentGoal, string(specJSON), plan.ApprovalReason, plan.Status, plan.CreatedAt, plan.ExpiresAt,
	)
	return err
}

func (s *SQLiteStorage) GetPlan(ctx context.Context, id string) (*packages.PendingPlan, error) {
	var plan packages.PendingPlan
	var specJSON string
	var appTime, rejTime sql.NullTime
	var appBy, rejBy, rejComm sql.NullString

	query := `
		SELECT id, requested_by, COALESCE(agent_id, ''), COALESCE(agent_goal, ''), spec, approval_reason, status, created_at, expires_at,
		       approved_by, approved_at, rejected_by, rejected_at, rejection_comment
		FROM pte_plans WHERE id = ?
	`
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&plan.ID, &plan.RequestedBy, &plan.AgentID, &plan.AgentGoal, &specJSON, &plan.ApprovalReason, &plan.Status, &plan.CreatedAt, &plan.ExpiresAt,
		&appBy, &appTime, &rejBy, &rejTime, &rejComm,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("plan %q not found", id)
	}
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal([]byte(specJSON), &plan.Spec)
	if appBy.Valid {
		plan.ApprovedBy = appBy.String
	}
	if appTime.Valid {
		t := appTime.Time
		plan.ApprovedAt = &t
	}
	if rejBy.Valid {
		plan.RejectedBy = rejBy.String
	}
	if rejComm.Valid {
		plan.RejectionComment = rejComm.String
	}

	return &plan, nil
}

func (s *SQLiteStorage) ListPlans(ctx context.Context) ([]*packages.PendingPlan, error) {
	query := `
		SELECT id, requested_by, COALESCE(agent_id, ''), COALESCE(agent_goal, ''), spec, approval_reason, status, created_at, expires_at,
		       approved_by, approved_at, rejected_by, rejected_at, rejection_comment
		FROM pte_plans ORDER BY created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []*packages.PendingPlan
	for rows.Next() {
		var plan packages.PendingPlan
		var specJSON string
		var appTime, rejTime sql.NullTime
		var appBy, rejBy, rejComm sql.NullString

		err := rows.Scan(
			&plan.ID, &plan.RequestedBy, &plan.AgentID, &plan.AgentGoal, &specJSON, &plan.ApprovalReason, &plan.Status, &plan.CreatedAt, &plan.ExpiresAt,
			&appBy, &appTime, &rejBy, &rejTime, &rejComm,
		)
		if err != nil {
			return nil, err
		}

		_ = json.Unmarshal([]byte(specJSON), &plan.Spec)
		if appBy.Valid {
			plan.ApprovedBy = appBy.String
		}
		if appTime.Valid {
			t := appTime.Time
			plan.ApprovedAt = &t
		}
		if rejBy.Valid {
			plan.RejectedBy = rejBy.String
		}
		if rejComm.Valid {
			plan.RejectionComment = rejComm.String
		}
		plans = append(plans, &plan)
	}
	return plans, nil
}

func (s *SQLiteStorage) UpdatePlanStatus(ctx context.Context, id string, status string, actor string, comment string) error {
	now := time.Now().UTC()
	switch status {
	case "approved":
		_, err := s.db.ExecContext(ctx, `
			UPDATE pte_plans 
			SET status = ?, approved_by = ?, approved_at = ? 
			WHERE id = ? AND status = 'pending'
		`, status, actor, now, id)
		return err
	case "rejected":
		_, err := s.db.ExecContext(ctx, `
			UPDATE pte_plans 
			SET status = ?, rejected_by = ?, rejected_at = ?, rejection_comment = ? 
			WHERE id = ? AND status = 'pending'
		`, status, actor, now, comment, id)
		return err
	default:
		return fmt.Errorf("unsupported update status %q", status)
	}
}

// --- Events ---

func (s *SQLiteStorage) AddEvent(ctx context.Context, event packages.Event) (packages.Event, error) {
	event.Time = time.Now().UTC()
	query := `
		INSERT INTO events (time, level, source, workload, message)
		VALUES (?, ?, ?, ?, ?)
	`
	res, err := s.db.ExecContext(ctx, query,
		event.Time, event.Level, event.Source, event.Workload, event.Message,
	)
	if err != nil {
		return event, err
	}
	event.ID, err = res.LastInsertId()
	return event, err
}

func (s *SQLiteStorage) ListEvents(ctx context.Context) ([]packages.Event, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, time, level, source, COALESCE(workload, ''), message FROM events ORDER BY id DESC LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []packages.Event
	for rows.Next() {
		var e packages.Event
		err := rows.Scan(&e.ID, &e.Time, &e.Level, &e.Source, &e.Workload, &e.Message)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

// --- Workload History ---

func (s *SQLiteStorage) InsertHistory(ctx context.Context, name, specJSON, actor string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workload_history (name, spec_json, applied_by, applied_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
	`, name, specJSON, actor)
	return err
}

func (s *SQLiteStorage) GetHistory(ctx context.Context, name string, limit int) ([]packages.WorkloadHistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, spec_json, applied_by, applied_at 
		FROM workload_history 
		WHERE name = ? 
		ORDER BY id DESC 
		LIMIT ?
	`, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []packages.WorkloadHistoryEntry
	for rows.Next() {
		var entry packages.WorkloadHistoryEntry
		var specJSON string
		err := rows.Scan(&entry.ID, &specJSON, &entry.AppliedBy, &entry.AppliedAt)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(specJSON), &entry.Spec)
		history = append(history, entry)
	}
	return history, nil
}

func (s *SQLiteStorage) GetHistoryVersion(ctx context.Context, name string, version int64) (packages.WorkloadSpec, error) {
	var spec packages.WorkloadSpec
	var specJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT spec_json 
		FROM workload_history 
		WHERE name = ? AND id = ?
	`, name, version).Scan(&specJSON)
	if err != nil {
		return spec, err
	}
	err = json.Unmarshal([]byte(specJSON), &spec)
	return spec, err
}
