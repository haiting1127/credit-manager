package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yuluo688/credit-manager/internal/money"
)

type Caller struct {
	ID                   string
	DisplayName          string
	QuotaMicroUSD        money.MicroUSD
	SettledSpendMicroUSD money.MicroUSD
	HeldAmountMicroUSD   money.MicroUSD
	Enabled              bool
	// Rate limits shared across all keys of this caller. 0 = unlimited.
	MaxConcurrentRequests int64
	RPMLimit              int64
	TPMLimit              int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (c Caller) RemainingMicroUSD() money.MicroUSD {
	remaining := int64(c.QuotaMicroUSD) - int64(c.SettledSpendMicroUSD) - int64(c.HeldAmountMicroUSD)
	return money.MicroUSD(remaining)
}

type CallerSpec struct {
	ID            string
	DisplayName   string
	QuotaMicroUSD money.MicroUSD
	Enabled       bool
	// Rate limits shared across all keys of this caller. 0 = unlimited.
	MaxConcurrentRequests int64
	RPMLimit              int64
	TPMLimit              int64
}

func (s *Store) CreateCaller(ctx context.Context, spec CallerSpec) (Caller, error) {
	if strings.TrimSpace(spec.ID) == "" || spec.QuotaMicroUSD < 0 {
		return Caller{}, fmt.Errorf("%w: caller id and non-negative quota are required", ErrInvalidArgument)
	}
	now := nowUnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO callers(
		id, display_name, quota_micro_usd, enabled, created_at_unix_ms, updated_at_unix_ms,
		max_concurrent_requests, rpm_limit, tpm_limit
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		spec.ID, spec.DisplayName, spec.QuotaMicroUSD, boolInt(spec.Enabled), now, now,
		spec.MaxConcurrentRequests, spec.RPMLimit, spec.TPMLimit)
	if err != nil {
		return Caller{}, fmt.Errorf("create caller: %w", err)
	}
	return s.GetCaller(ctx, spec.ID)
}

func (s *Store) GetCaller(ctx context.Context, callerID string) (Caller, error) {
	return scanCaller(s.db.QueryRowContext(ctx, `SELECT id, display_name, quota_micro_usd,
		settled_spend_micro_usd, held_amount_micro_usd, enabled, created_at_unix_ms, updated_at_unix_ms,
		max_concurrent_requests, rpm_limit, tpm_limit
		FROM callers WHERE id = ?`, callerID))
}

func (s *Store) ListCallers(ctx context.Context, limit int) ([]Caller, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, display_name, quota_micro_usd,
		settled_spend_micro_usd, held_amount_micro_usd, enabled, created_at_unix_ms, updated_at_unix_ms,
		max_concurrent_requests, rpm_limit, tpm_limit
		FROM callers ORDER BY created_at_unix_ms DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Caller
	for rows.Next() {
		caller, err := scanCaller(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, caller)
	}
	return out, rows.Err()
}

func (s *Store) SetCallerQuota(ctx context.Context, callerID string, quota money.MicroUSD) error {
	if quota < 0 {
		return fmt.Errorf("%w: quota must not be negative", ErrInvalidArgument)
	}
	// Allow raising/lowering quota freely; over-settled balances stay fail-closed on reserve.
	result, err := s.db.ExecContext(ctx, `UPDATE callers SET quota_micro_usd = ?, updated_at_unix_ms = ?
		WHERE id = ?`, quota, nowUnixMilli(), callerID)
	if err != nil {
		return fmt.Errorf("set caller quota: %w", err)
	}
	return requireOneRow(result, ErrCallerNotFound)
}

func (s *Store) SetCallerEnabled(ctx context.Context, callerID string, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE callers SET enabled = ?, updated_at_unix_ms = ? WHERE id = ?`,
		boolInt(enabled), nowUnixMilli(), callerID)
	if err != nil {
		return fmt.Errorf("set caller enabled: %w", err)
	}
	return requireOneRow(result, ErrCallerNotFound)
}

// CallerUpdate carries optional overrides for caller fields. A nil field keeps
// the current value; a non-nil field replaces it. Limits of 0 mean unlimited.
type CallerUpdate struct {
	QuotaMicroUSD         *money.MicroUSD
	MaxConcurrentRequests *int64
	RPMLimit              *int64
	TPMLimit              *int64
}

func (s *Store) UpdateCaller(ctx context.Context, callerID string, update CallerUpdate) (Caller, error) {
	if strings.TrimSpace(callerID) == "" {
		return Caller{}, fmt.Errorf("%w: caller id is required", ErrInvalidArgument)
	}
	if update.QuotaMicroUSD != nil && *update.QuotaMicroUSD < 0 {
		return Caller{}, fmt.Errorf("%w: quota must not be negative", ErrInvalidArgument)
	}
	if _, err := s.GetCaller(ctx, callerID); err != nil {
		return Caller{}, err
	}
	sets := []string{"updated_at_unix_ms = ?"}
	args := []any{nowUnixMilli()}
	if update.QuotaMicroUSD != nil {
		sets = append(sets, "quota_micro_usd = ?")
		args = append(args, int64(*update.QuotaMicroUSD))
	}
	if update.MaxConcurrentRequests != nil {
		sets = append(sets, "max_concurrent_requests = ?")
		args = append(args, *update.MaxConcurrentRequests)
	}
	if update.RPMLimit != nil {
		sets = append(sets, "rpm_limit = ?")
		args = append(args, *update.RPMLimit)
	}
	if update.TPMLimit != nil {
		sets = append(sets, "tpm_limit = ?")
		args = append(args, *update.TPMLimit)
	}
	args = append(args, callerID)
	result, err := s.db.ExecContext(ctx, "UPDATE callers SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return Caller{}, fmt.Errorf("update caller: %w", err)
	}
	if err := requireOneRow(result, ErrCallerNotFound); err != nil {
		return Caller{}, err
	}
	return s.GetCaller(ctx, callerID)
}
