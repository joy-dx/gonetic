package utils

import (
	"testing"
	"time"
)

func TestConstantDelay_Wait_Golden(t *testing.T) {
	t.Parallel()

	start := time.Now()
	ConstantDelay{Period: 1}.Wait("task", 1)
	elapsed := time.Since(start)

	// Allow some timing variance.
	if elapsed < 900*time.Millisecond {
		t.Fatalf("elapsed=%v too short", elapsed)
	}
}

func TestExponentialBackoff_Wait_Golden(t *testing.T) {
	t.Parallel()

	// Attempt 0 -> backoff min(2*2^0,10)=2 seconds (plus jitter 0..1s)
	start := time.Now()
	ExponentialBackoff{}.Wait("task", 0)
	elapsed := time.Since(start)

	if elapsed < 1800*time.Millisecond {
		t.Fatalf("elapsed=%v too short", elapsed)
	}
	if elapsed > 3200*time.Millisecond {
		t.Fatalf("elapsed=%v too long (unexpected)", elapsed)
	}
}
