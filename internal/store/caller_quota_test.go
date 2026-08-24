package store

import (
	"context"
	"errors"
	"testing"

	"github.com/yuluo688/credit-manager/internal/money"
)

// newCallerKey creates a plugin key bound to the given caller (newTestKey
// pins CallerID to the default "caller" record).
func newCallerKey(t *testing.T, ctx context.Context, st *Store, kid, callerID string) PluginKey {
	t.Helper()
	key, err := st.CreatePluginKey(ctx, PluginKeySpec{
		Kid:          kid,
		CallerID:     callerID,
		KeyHash:      []byte("test-key-hash-" + kid),
		PepperID:     "active",
		Fingerprint:  "fingerprint",
		Principal:    "credit-manager:" + kid,
		CallerScope:  "credit-manager:" + kid,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("create key %s: %v", kid, err)
	}
	return key
}

// Caller-level shared quota must aggregate spend across ALL keys of the caller:
// burning the pool via one key must reject reserves on a sibling key.
func TestCallerQuotaSharedAcrossKeys(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	// Re-create the caller with a shared quota (default test caller has 0 = unlimited).
	if _, err := st.CreateCaller(ctx, CallerSpec{ID: "shared", QuotaMicroUSD: 10, Enabled: true}); err != nil {
		t.Fatalf("create caller: %v", err)
	}
	keyA := newCallerKey(t, ctx, st, "key-alice-01", "shared")
	keyB := newCallerKey(t, ctx, st, "key-bob-002", "shared")

	// key-a consumes 6 of the caller's 10.
	if _, err := st.Reserve(ctx, reserveRequest(keyA, "a-1", 6)); err != nil {
		t.Fatalf("reserve on key-a: %v", err)
	}
	// key-a alone can still afford 4 with unlimited own quota.
	// key-b (fresh key, own quota untouched) must be rejected: caller pool only has 4 left.
	if _, err := st.Reserve(ctx, reserveRequest(keyB, "b-1", 5)); !errors.Is(err, ErrCallerQuotaExceeded) {
		t.Fatalf("reserve on key-b error = %v, want ErrCallerQuotaExceeded", err)
	}
	// A smaller request on key-b within the remaining pool succeeds.
	if _, err := st.Reserve(ctx, reserveRequest(keyB, "b-2", 4)); err != nil {
		t.Fatalf("reserve 4 on key-b: %v", err)
	}
}

// Caller quota 0 (default) keeps the historical behaviour: no caller cap.
func TestCallerQuotaZeroMeansUnlimited(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	key := newTestKey(t, ctx, st, PluginKeySpec{})
	if _, err := st.Reserve(ctx, reserveRequest(key, "r1", 1000)); err != nil {
		t.Fatalf("reserve with unlimited caller quota: %v", err)
	}
}

// Settlement must move spend from held to settled on the caller ledger.
func TestSettleUpdatesCallerLedger(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	if _, err := st.CreateCaller(ctx, CallerSpec{ID: "shared", QuotaMicroUSD: 10, Enabled: true}); err != nil {
		t.Fatalf("create caller: %v", err)
	}
	key := newCallerKey(t, ctx, st, "key-alice-01", "shared")
	reservation, err := st.Reserve(ctx, reserveRequest(key, "r1", 6))
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	caller, err := st.GetCaller(ctx, "shared")
	if err != nil {
		t.Fatalf("get caller after reserve: %v", err)
	}
	if caller.HeldAmountMicroUSD != 6 {
		t.Fatalf("caller held after reserve = %d, want 6", caller.HeldAmountMicroUSD)
	}

	if _, err := st.Settle(ctx, Settlement{
		ReservationID: reservation.ID,
		Model:         "test-model",
		CostMicroUSD:  5,
		Usage:         money.TokenUsage{Input: 1, Output: 1},
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	caller, err = st.GetCaller(ctx, "shared")
	if err != nil {
		t.Fatalf("get caller after settle: %v", err)
	}
	if caller.HeldAmountMicroUSD != 0 {
		t.Fatalf("caller held after settle = %d, want 0", caller.HeldAmountMicroUSD)
	}
	if caller.SettledSpendMicroUSD != 5 {
		t.Fatalf("caller settled spend after settle = %d, want 5", caller.SettledSpendMicroUSD)
	}
}

// Release must give the held amount back to the caller pool.
func TestReleaseRestoresCallerPool(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	if _, err := st.CreateCaller(ctx, CallerSpec{ID: "shared", QuotaMicroUSD: 10, Enabled: true}); err != nil {
		t.Fatalf("create caller: %v", err)
	}
	keyA := newCallerKey(t, ctx, st, "key-alice-01", "shared")
	keyB := newCallerKey(t, ctx, st, "key-bob-002", "shared")

	reservation, err := st.Reserve(ctx, reserveRequest(keyA, "a-1", 6))
	if err != nil {
		t.Fatalf("reserve on key-a: %v", err)
	}
	if _, err := st.Reserve(ctx, reserveRequest(keyB, "b-1", 5)); !errors.Is(err, ErrCallerQuotaExceeded) {
		t.Fatalf("reserve on key-b error = %v, want ErrCallerQuotaExceeded", err)
	}
	if _, err := st.Release(ctx, reservation.ID, "test"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Pool restored: key-b's 5 now fits again.
	if _, err := st.Reserve(ctx, reserveRequest(keyB, "b-2", 5)); err != nil {
		t.Fatalf("reserve on key-b after release: %v", err)
	}
}
