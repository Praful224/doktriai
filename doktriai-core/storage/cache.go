package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

// RedisClient represents standard Redis operational methods needed for Cache-aside.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

type CachedStorage struct {
	underlying Storage
	rdb        RedisClient
	ttl        time.Duration
}

func NewCachedStorage(underlying Storage, rdb RedisClient, ttl time.Duration) *CachedStorage {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &CachedStorage{
		underlying: underlying,
		rdb:        rdb,
		ttl:        ttl,
	}
}

func workloadKey(name string) string {
	return fmt.Sprintf("doktri:workload:%s", name)
}

const workloadListKey = "doktri:workloads:list"

func (c *CachedStorage) ListWorkloads(ctx context.Context) ([]packages.WorkloadSpec, error) {
	// Try Cache
	val, err := c.rdb.Get(ctx, workloadListKey)
	if err == nil && val != "" {
		var specs []packages.WorkloadSpec
		if err := json.Unmarshal([]byte(val), &specs); err == nil {
			return specs, nil
		}
	}

	// Cache Miss
	specs, err := c.underlying.ListWorkloads(ctx)
	if err != nil {
		return nil, err
	}

	// Write back to Cache
	if data, err := json.Marshal(specs); err == nil {
		_ = c.rdb.Set(ctx, workloadListKey, string(data), c.ttl)
	}

	return specs, nil
}

func (c *CachedStorage) GetWorkload(ctx context.Context, name string) (packages.WorkloadSpec, bool, error) {
	key := workloadKey(name)
	// Try Cache
	val, err := c.rdb.Get(ctx, key)
	if err == nil && val != "" {
		var spec packages.WorkloadSpec
		if err := json.Unmarshal([]byte(val), &spec); err == nil {
			return spec, true, nil
		}
	}

	// Cache Miss
	spec, ok, err := c.underlying.GetWorkload(ctx, name)
	if err != nil {
		return spec, false, err
	}
	if !ok {
		return spec, false, nil
	}

	// Write back to Cache
	if data, err := json.Marshal(spec); err == nil {
		_ = c.rdb.Set(ctx, key, string(data), c.ttl)
	}

	return spec, true, nil
}

func (c *CachedStorage) PutWorkload(ctx context.Context, spec packages.WorkloadSpec) error {
	err := c.underlying.PutWorkload(ctx, spec)
	if err != nil {
		return err
	}
	// Invalidate Cache
	_ = c.rdb.Del(ctx, workloadKey(spec.Name), workloadListKey)
	return nil
}

func (c *CachedStorage) DeleteWorkload(ctx context.Context, name string) error {
	err := c.underlying.DeleteWorkload(ctx, name)
	if err != nil {
		return err
	}
	// Invalidate Cache
	_ = c.rdb.Del(ctx, workloadKey(name), workloadListKey)
	return nil
}

// --- Delegate locks, audits, and plans to the underlying PostgreSQL Storage ---

func (c *CachedStorage) AddAudit(ctx context.Context, record packages.AuditRecord) (packages.AuditRecord, error) {
	return c.underlying.AddAudit(ctx, record)
}

func (c *CachedStorage) ListAudit(ctx context.Context) ([]packages.AuditRecord, error) {
	return c.underlying.ListAudit(ctx)
}

func (c *CachedStorage) GetAuditSince(ctx context.Context, sinceSeq int64) ([]packages.AuditRecord, error) {
	return c.underlying.GetAuditSince(ctx, sinceSeq)
}

func (c *CachedStorage) GetLock(ctx context.Context) (packages.LockState, error) {
	return c.underlying.GetLock(ctx)
}

func (c *CachedStorage) AcquireLock(ctx context.Context, actor, reason string) error {
	return c.underlying.AcquireLock(ctx, actor, reason)
}

func (c *CachedStorage) ReleaseLock(ctx context.Context) error {
	return c.underlying.ReleaseLock(ctx)
}

func (c *CachedStorage) SubmitPlan(ctx context.Context, plan *packages.PendingPlan) error {
	return c.underlying.SubmitPlan(ctx, plan)
}

func (c *CachedStorage) GetPlan(ctx context.Context, id string) (*packages.PendingPlan, error) {
	return c.underlying.GetPlan(ctx, id)
}

func (c *CachedStorage) ListPlans(ctx context.Context) ([]*packages.PendingPlan, error) {
	return c.underlying.ListPlans(ctx)
}

func (c *CachedStorage) UpdatePlanStatus(ctx context.Context, id string, status string, actor string, comment string) error {
	return c.underlying.UpdatePlanStatus(ctx, id, status, actor, comment)
}

// --- Workload History Delegation ---

func (c *CachedStorage) InsertHistory(ctx context.Context, name, specJSON, actor string) error {
	return c.underlying.InsertHistory(ctx, name, specJSON, actor)
}

func (c *CachedStorage) GetHistory(ctx context.Context, name string, limit int) ([]packages.WorkloadHistoryEntry, error) {
	return c.underlying.GetHistory(ctx, name, limit)
}

func (c *CachedStorage) GetHistoryVersion(ctx context.Context, name string, version int64) (packages.WorkloadSpec, error) {
	return c.underlying.GetHistoryVersion(ctx, name, version)
}
