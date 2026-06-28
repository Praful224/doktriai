package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

type PostgresStorage struct {
	db *sql.DB
}

func NewPostgresStorage(db *sql.DB) *PostgresStorage {
	return &PostgresStorage{db: db}
}

// Migrate executes schema migrations for storage.
func (p *PostgresStorage) Migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS workloads (
			name VARCHAR(253) PRIMARY KEY,
			image TEXT NOT NULL,
			replicas INTEGER NOT NULL CHECK (replicas >= 0 AND replicas <= 100),
			port INTEGER NOT NULL CHECK (port >= 0 AND port <= 65535),
			container_port INTEGER NOT NULL CHECK (container_port >= 0 AND container_port <= 65535),
			runtime VARCHAR(50) NOT NULL,
			env JSONB NOT NULL DEFAULT '{}'::jsonb,
			resources JSONB NOT NULL DEFAULT '{}'::jsonb,
			volumes JSONB NOT NULL DEFAULT '[]'::jsonb,
			labels JSONB NOT NULL DEFAULT '{}'::jsonb,
			security_mode VARCHAR(50) NOT NULL DEFAULT 'dev',
			deploy_strategy VARCHAR(50) NOT NULL DEFAULT 'recreate',
			max_surge INTEGER NOT NULL DEFAULT 1,
			max_unavailable INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS audit_records (
			id BIGSERIAL PRIMARY KEY,
			seq_id BIGINT UNIQUE NOT NULL,
			time TIMESTAMP WITH TIME ZONE NOT NULL,
			actor VARCHAR(255) NOT NULL,
			action VARCHAR(100) NOT NULL,
			workload VARCHAR(253) NOT NULL,
			allowed BOOLEAN NOT NULL,
			reason TEXT,
			plan_id VARCHAR(50),
			state_hash_before VARCHAR(64),
			state_hash_after VARCHAR(64),
			agent_id VARCHAR(255),
			agent_scope TEXT,
			agent_goal TEXT,
			signature_verified BOOLEAN DEFAULT FALSE
		);`,
		`CREATE TABLE IF NOT EXISTS environment_locks (
			id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
			locked BOOLEAN NOT NULL DEFAULT FALSE,
			acquired_by VARCHAR(255),
			acquired_at TIMESTAMP WITH TIME ZONE,
			reason TEXT
		);`,
		`INSERT INTO environment_locks (id, locked) VALUES (1, false) ON CONFLICT DO NOTHING;`,
		`CREATE TABLE IF NOT EXISTS pte_plans (
			id VARCHAR(64) PRIMARY KEY,
			requested_by VARCHAR(255) NOT NULL,
			agent_id VARCHAR(255),
			agent_goal TEXT,
			spec JSONB NOT NULL,
			approval_reason TEXT NOT NULL,
			status VARCHAR(50) NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			approved_by VARCHAR(255),
			approved_at TIMESTAMP WITH TIME ZONE,
			rejected_by VARCHAR(255),
			rejected_at TIMESTAMP WITH TIME ZONE,
			rejection_comment TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS workload_history (
			id          BIGSERIAL PRIMARY KEY,
			name        VARCHAR(253) NOT NULL,
			spec_json   TEXT NOT NULL,
			applied_by  VARCHAR(255) NOT NULL DEFAULT '',
			applied_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, q := range queries {
		if _, err := p.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration query failed: %w", err)
		}
	}
	
	// Upgrade existing postgres table columns if they don't exist
	_, _ = p.db.ExecContext(ctx, "ALTER TABLE workloads ADD COLUMN IF NOT EXISTS deploy_strategy VARCHAR(50) DEFAULT 'recreate'")
	_, _ = p.db.ExecContext(ctx, "ALTER TABLE workloads ADD COLUMN IF NOT EXISTS max_surge INTEGER DEFAULT 1")
	_, _ = p.db.ExecContext(ctx, "ALTER TABLE workloads ADD COLUMN IF NOT EXISTS max_unavailable INTEGER DEFAULT 0")

	// Seed initial workloads if table is empty
	var count int
	_ = p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workloads").Scan(&count)
	if count == 0 {
		_, _ = p.db.ExecContext(ctx, `INSERT INTO workloads (name, image, replicas, port, container_port, runtime, env, resources, volumes, labels, security_mode, deploy_strategy, max_surge, max_unavailable) VALUES 
		('secure-ingress', 'nginx:alpine', 2, 8080, 80, 'docker', '{}'::jsonb, '{}'::jsonb, '[]'::jsonb, '{}'::jsonb, 'dev', 'recreate', 1, 0),
		('reconciler-daemon', 'busybox:latest', 1, 0, 0, 'docker', '{}'::jsonb, '{}'::jsonb, '[]'::jsonb, '{}'::jsonb, 'dev', 'recreate', 1, 0),
		('agent-gateway', 'python:3.11-alpine', 1, 9000, 9000, 'docker', '{}'::jsonb, '{}'::jsonb, '[]'::jsonb, '{}'::jsonb, 'dev', 'recreate', 1, 0)`)
	}

	return nil
}

// --- Workloads ---

func (p *PostgresStorage) ListWorkloads(ctx context.Context) ([]packages.WorkloadSpec, error) {
	rows, err := p.db.QueryContext(ctx, "SELECT name, image, replicas, port, container_port, runtime, env, resources, volumes, labels, security_mode, deploy_strategy, max_surge, max_unavailable FROM workloads ORDER BY name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var specs []packages.WorkloadSpec
	for rows.Next() {
		var spec packages.WorkloadSpec
		var envJSON, resJSON, volJSON, lblJSON []byte
		err := rows.Scan(
			&spec.Name, &spec.Image, &spec.Replicas, &spec.Port, &spec.ContainerPort, &spec.Runtime,
			&envJSON, &resJSON, &volJSON, &lblJSON, &spec.SecurityMode,
			&spec.DeployStrategy, &spec.MaxSurge, &spec.MaxUnavailable,
		)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal(envJSON, &spec.Env)
		_ = json.Unmarshal(resJSON, &spec.Resources)
		_ = json.Unmarshal(volJSON, &spec.Volumes)
		_ = json.Unmarshal(lblJSON, &spec.Labels)
		specs = append(specs, spec)
	}
	return specs, nil
}

func (p *PostgresStorage) GetWorkload(ctx context.Context, name string) (packages.WorkloadSpec, bool, error) {
	var spec packages.WorkloadSpec
	var envJSON, resJSON, volJSON, lblJSON []byte
	err := p.db.QueryRowContext(ctx, "SELECT name, image, replicas, port, container_port, runtime, env, resources, volumes, labels, security_mode, deploy_strategy, max_surge, max_unavailable FROM workloads WHERE name = $1", name).Scan(
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
	_ = json.Unmarshal(envJSON, &spec.Env)
	_ = json.Unmarshal(resJSON, &spec.Resources)
	_ = json.Unmarshal(volJSON, &spec.Volumes)
	_ = json.Unmarshal(lblJSON, &spec.Labels)
	return spec, true, nil
}

func (p *PostgresStorage) PutWorkload(ctx context.Context, spec packages.WorkloadSpec) error {
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, CURRENT_TIMESTAMP)
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
	_, err := p.db.ExecContext(ctx, query,
		spec.Name, spec.Image, spec.Replicas, spec.Port, spec.ContainerPort, spec.Runtime,
		envJSON, resJSON, volJSON, lblJSON, spec.SecurityMode,
		spec.DeployStrategy, spec.MaxSurge, spec.MaxUnavailable,
	)
	return err
}

func (p *PostgresStorage) DeleteWorkload(ctx context.Context, name string) error {
	_, err := p.db.ExecContext(ctx, "DELETE FROM workloads WHERE name = $1", name)
	return err
}

// --- Audit ---

func (p *PostgresStorage) AddAudit(ctx context.Context, record packages.AuditRecord) (packages.AuditRecord, error) {
	record.Time = time.Now().UTC()
	// Obtain a monotonic sequence ID using PostgreSQL counter or max value query inside a transaction.
	tx, err := p.db.BeginTx(ctx, nil)
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

	query := `
		INSERT INTO audit_records (seq_id, time, actor, action, workload, allowed, reason, plan_id, state_hash_before, state_hash_after, agent_id, agent_scope, agent_goal, signature_verified)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id
	`
	err = tx.QueryRowContext(ctx, query,
		record.SeqID, record.Time, record.Actor, record.Action, record.Workload, record.Allowed,
		record.Reason, record.PlanID, record.StateHashBefore, record.StateHashAfter,
		record.AgentID, record.AgentScope, record.AgentGoal, record.SignatureVerified,
	).Scan(&record.ID)
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

func (p *PostgresStorage) ListAudit(ctx context.Context) ([]packages.AuditRecord, error) {
	rows, err := p.db.QueryContext(ctx, "SELECT id, seq_id, time, actor, action, workload, allowed, reason, plan_id, state_hash_before, state_hash_after, agent_id, agent_scope, agent_goal, signature_verified FROM audit_records ORDER BY seq_id DESC LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []packages.AuditRecord
	for rows.Next() {
		var r packages.AuditRecord
		err := rows.Scan(
			&r.ID, &r.SeqID, &r.Time, &r.Actor, &r.Action, &r.Workload, &r.Allowed,
			&r.Reason, &r.PlanID, &r.StateHashBefore, &r.StateHashAfter,
			&r.AgentID, &r.AgentScope, &r.AgentGoal, &r.SignatureVerified,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

func (p *PostgresStorage) GetAuditSince(ctx context.Context, sinceSeq int64) ([]packages.AuditRecord, error) {
	rows, err := p.db.QueryContext(ctx, "SELECT id, seq_id, time, actor, action, workload, allowed, reason, plan_id, state_hash_before, state_hash_after, agent_id, agent_scope, agent_goal, signature_verified FROM audit_records WHERE seq_id >= $1 ORDER BY seq_id DESC", sinceSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []packages.AuditRecord
	for rows.Next() {
		var r packages.AuditRecord
		err := rows.Scan(
			&r.ID, &r.SeqID, &r.Time, &r.Actor, &r.Action, &r.Workload, &r.Allowed,
			&r.Reason, &r.PlanID, &r.StateHashBefore, &r.StateHashAfter,
			&r.AgentID, &r.AgentScope, &r.AgentGoal, &r.SignatureVerified,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// --- Environment Lock ---

func (p *PostgresStorage) GetLock(ctx context.Context) (packages.LockState, error) {
	var l packages.LockState
	err := p.db.QueryRowContext(ctx, "SELECT locked, COALESCE(acquired_by, ''), acquired_at, COALESCE(reason, '') FROM environment_locks WHERE id = 1").Scan(
		&l.Locked, &l.AcquiredBy, &l.Time, &l.Reason,
	)
	if err != nil {
		return l, err
	}
	return l, nil
}

func (p *PostgresStorage) AcquireLock(ctx context.Context, actor, reason string) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE environment_locks 
		SET locked = true, acquired_by = $1, acquired_at = CURRENT_TIMESTAMP, reason = $2 
		WHERE id = 1
	`, actor, reason)
	return err
}

func (p *PostgresStorage) ReleaseLock(ctx context.Context) error {
	_, err := p.db.ExecContext(ctx, `
		UPDATE environment_locks 
		SET locked = false, acquired_by = NULL, acquired_at = NULL, reason = NULL 
		WHERE id = 1
	`)
	return err
}

// --- PTE Plans ---

func (p *PostgresStorage) SubmitPlan(ctx context.Context, plan *packages.PendingPlan) error {
	specJSON, _ := json.Marshal(plan.Spec)
	query := `
		INSERT INTO pte_plans (id, requested_by, agent_id, agent_goal, spec, approval_reason, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := p.db.ExecContext(ctx, query,
		plan.ID, plan.RequestedBy, plan.AgentID, plan.AgentGoal, specJSON, plan.ApprovalReason, plan.Status, plan.CreatedAt, plan.ExpiresAt,
	)
	return err
}

func (p *PostgresStorage) GetPlan(ctx context.Context, id string) (*packages.PendingPlan, error) {
	var plan packages.PendingPlan
	var specJSON []byte
	var appTime, rejTime sql.NullTime
	var appBy, rejBy, rejComm sql.NullString

	query := `
		SELECT id, requested_by, COALESCE(agent_id, ''), COALESCE(agent_goal, ''), spec, approval_reason, status, created_at, expires_at,
		       approved_by, approved_at, rejected_by, rejected_at, rejection_comment
		FROM pte_plans WHERE id = $1
	`
	err := p.db.QueryRowContext(ctx, query, id).Scan(
		&plan.ID, &plan.RequestedBy, &plan.AgentID, &plan.AgentGoal, &specJSON, &plan.ApprovalReason, &plan.Status, &plan.CreatedAt, &plan.ExpiresAt,
		&appBy, &appTime, &rejBy, &rejTime, &rejComm,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("plan %q not found", id)
	}
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(specJSON, &plan.Spec)
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

func (p *PostgresStorage) ListPlans(ctx context.Context) ([]*packages.PendingPlan, error) {
	query := `
		SELECT id, requested_by, COALESCE(agent_id, ''), COALESCE(agent_goal, ''), spec, approval_reason, status, created_at, expires_at,
		       approved_by, approved_at, rejected_by, rejected_at, rejection_comment
		FROM pte_plans ORDER BY created_at DESC
	`
	rows, err := p.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []*packages.PendingPlan
	for rows.Next() {
		var plan packages.PendingPlan
		var specJSON []byte
		var appTime, rejTime sql.NullTime
		var appBy, rejBy, rejComm sql.NullString

		err := rows.Scan(
			&plan.ID, &plan.RequestedBy, &plan.AgentID, &plan.AgentGoal, &specJSON, &plan.ApprovalReason, &plan.Status, &plan.CreatedAt, &plan.ExpiresAt,
			&appBy, &appTime, &rejBy, &rejTime, &rejComm,
		)
		if err != nil {
			return nil, err
		}

		_ = json.Unmarshal(specJSON, &plan.Spec)
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

func (p *PostgresStorage) UpdatePlanStatus(ctx context.Context, id string, status string, actor string, comment string) error {
	now := time.Now().UTC()
	if status == "approved" {
		_, err := p.db.ExecContext(ctx, `
			UPDATE pte_plans 
			SET status = $2, approved_by = $3, approved_at = $4 
			WHERE id = $1 AND status = 'pending'
		`, id, status, actor, now)
		return err
	} else if status == "rejected" {
		_, err := p.db.ExecContext(ctx, `
			UPDATE pte_plans 
			SET status = $2, rejected_by = $3, rejected_at = $4, rejection_comment = $5 
			WHERE id = $1 AND status = 'pending'
		`, id, status, actor, now, comment)
		return err
	}
	return fmt.Errorf("unsupported update status %q", status)
}

// --- Workload History ---

func (p *PostgresStorage) InsertHistory(ctx context.Context, name, specJSON, actor string) error {
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO workload_history (name, spec_json, applied_by, applied_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
	`, name, specJSON, actor)
	return err
}

func (p *PostgresStorage) GetHistory(ctx context.Context, name string, limit int) ([]packages.WorkloadHistoryEntry, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT id, spec_json, applied_by, applied_at 
		FROM workload_history 
		WHERE name = $1 
		ORDER BY id DESC 
		LIMIT $2
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

func (p *PostgresStorage) GetHistoryVersion(ctx context.Context, name string, version int64) (packages.WorkloadSpec, error) {
	var spec packages.WorkloadSpec
	var specJSON string
	err := p.db.QueryRowContext(ctx, `
		SELECT spec_json 
		FROM workload_history 
		WHERE name = $1 AND id = $2
	`, name, version).Scan(&specJSON)
	if err != nil {
		return spec, err
	}
	err = json.Unmarshal([]byte(specJSON), &spec)
	return spec, err
}
