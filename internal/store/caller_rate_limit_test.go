package store

import (
	"context"
	"errors"
	"testing"
)

// Caller RPM must count requests across ALL keys of the caller.
func TestCallerRPMSharedAcrossKeys(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	if _, err := st.CreateCaller(ctx, CallerSpec{ID: "rpm", QuotaMicroUSD: 0, Enabled: true, RPMLimit: 3}); err != nil {
		t.Fatalf("create caller: %v", err)
	}
	keyA := newCallerKey(t, ctx, st, "key-rpm-alice", "rpm")
	keyB := newCallerKey(t, ctx, st, "key-rpm-bob-2", "rpm")

	// key-a uses 2 of the 3 requests in the current minute.
	for _, id := range []string{"a-1", "a-2"} {
		if _, err := st.Reserve(ctx, reserveRequest(keyA, id, 1)); err != nil {
			t.Fatalf("reserve %s on key-a: %v", id, err)
		}
	}
	// key-b uses the 3rd: allowed.
	if _, err := st.Reserve(ctx, reserveRequest(keyB, "b-1", 1)); err != nil {
		t.Fatalf("reserve b-1 on key-b: %v", err)
	}
	// key-a again must be rejected: caller minute window exhausted.
	if _, err := st.Reserve(ctx, reserveRequest(keyA, "a-3", 1)); !errors.Is(err, ErrCallerRPMExceeded) {
		t.Fatalf("reserve a-3 error = %v, want ErrCallerRPMExceeded", err)
	}
}

// Caller TPM must sum request token estimates across ALL keys of the caller.
func TestCallerTPMSharedAcrossKeys(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	if _, err := st.CreateCaller(ctx, CallerSpec{ID: "tpm", QuotaMicroUSD: 0, Enabled: true, TPMLimit: 1000}); err != nil {
		t.Fatalf("create caller: %v", err)
	}
	keyA := newCallerKey(t, ctx, st, "key-tpm-alice", "tpm")
	keyB := newCallerKey(t, ctx, st, "key-tpm-bob-2", "tpm")

	// key-a burns 600 of the 1000 token budget.
	req := reserveRequest(keyA, "a-1", 1)
	req.RequestTokenEstimate = 600
	if _, err := st.Reserve(ctx, req); err != nil {
		t.Fatalf("reserve 600 tokens on key-a: %v", err)
	}
	// key-b with 300 more tokens fits (600+300 <= 1000).
	req = reserveRequest(keyB, "b-1", 1)
	req.RequestTokenEstimate = 300
	if _, err := st.Reserve(ctx, req); err != nil {
		t.Fatalf("reserve 300 tokens on key-b: %v", err)
	}
	// key-b with 200 more must be rejected (600+300+200 > 1000).
	req = reserveRequest(keyB, "b-2", 1)
	req.RequestTokenEstimate = 200
	if _, err := st.Reserve(ctx, req); !errors.Is(err, ErrCallerTPMExceeded) {
		t.Fatalf("reserve 200 tokens error = %v, want ErrCallerTPMExceeded", err)
	}
}

// Caller concurrency must count in-flight reservations across ALL keys.
func TestCallerConcurrencySharedAcrossKeys(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	if _, err := st.CreateCaller(ctx, CallerSpec{ID: "conc", QuotaMicroUSD: 0, Enabled: true, MaxConcurrentRequests: 1}); err != nil {
		t.Fatalf("create caller: %v", err)
	}
	keyA := newCallerKey(t, ctx, st, "key-cc-alice2", "conc")
	keyB := newCallerKey(t, ctx, st, "key-cc-bob-22", "conc")

	reservation, err := st.Reserve(ctx, reserveRequest(keyA, "a-1", 1))
	if err != nil {
		t.Fatalf("reserve on key-a: %v", err)
	}
	// key-b is a different key but shares the caller's single concurrency slot.
	if _, err := st.Reserve(ctx, reserveRequest(keyB, "b-1", 1)); !errors.Is(err, ErrCallerConcurrentLimit) {
		t.Fatalf("reserve on key-b error = %v, want ErrCallerConcurrentLimit", err)
	}
	// After key-a settles, key-b can run.
	if _, err := st.Settle(ctx, Settlement{ReservationID: reservation.ID, Model: "test-model", CostMicroUSD: 1}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if _, err := st.Reserve(ctx, reserveRequest(keyB, "b-2", 1)); err != nil {
		t.Fatalf("reserve on key-b after settle: %v", err)
	}
}

// Zero rate limits (default) keep historical behaviour: no caller rate caps.
func TestCallerRateLimitsZeroMeansUnlimited(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	defer st.Close()

	// default caller created by newTestStore has all limits at 0
	key := newTestKey(t, ctx, st, PluginKeySpec{})
	for i := 0; i < 5; i++ {
		if _, err := st.Reserve(ctx, reserveRequest(key, "r-"+string(rune('a'+i)), 1)); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
}
