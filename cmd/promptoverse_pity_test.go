package cmd

import (
	"path/filepath"
	"testing"
)

func TestPityAdjustedChance_EscalatesWithMisses(t *testing.T) {
	base := pityAdjustedChance(1.0/7.0, 0)
	after10 := pityAdjustedChance(1.0/7.0, 10)
	after100 := pityAdjustedChance(1.0/7.0, 100)
	if !(base < after10 && after10 < after100) {
		t.Errorf("expected strictly increasing chance with more misses, got base=%v after10=%v after100=%v", base, after10, after100)
	}
}

func TestPityAdjustedChance_CapsAtOne(t *testing.T) {
	got := pityAdjustedChance(1.0/7.0, 100000)
	if got != 1.0 {
		t.Errorf("expected the chance to cap at 1.0 (guaranteed), got %v", got)
	}
}

func TestFibonacci_KnownSequence(t *testing.T) {
	want := []int64{0, 1, 1, 2, 3, 5, 8, 13, 21, 34, 55}
	for n, w := range want {
		if got := fibonacci(n); got != w {
			t.Errorf("fibonacci(%d) = %d, want %d", n, got, w)
		}
	}
}

func TestFibonacci_NegativeIsZero(t *testing.T) {
	if got := fibonacci(-1); got != 0 {
		t.Errorf("fibonacci(-1) = %d, want 0", got)
	}
}

func TestPityAdjustedChance_EarlyMissesStayCloseToBase(t *testing.T) {
	// The whole point of Fibonacci over flat linear: early misses barely
	// move the needle (fib(1)=1, fib(2)=1, fib(3)=2 are tiny), it's a long
	// drought that closes out fast, not a foreseeable flat climb.
	base := 1.0 / 7.0
	got := pityAdjustedChance(base, 2)
	if got-base > 0.05 {
		t.Errorf("expected the 2nd miss to barely move the chance above base %.4f, got %.4f", base, got)
	}
}

func TestPityState_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pity.json")
	want := pityState{RareStyleRunsSinceTrigger: 3, DiscoveryRunsSinceTrigger: 7}
	if err := savePityState(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadPityState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round-trip mismatch: want %+v, got %+v", want, got)
	}
}

func TestPityState_LoadNonexistentFileReturnsZeroValue(t *testing.T) {
	got, err := loadPityState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file should not error: %v", err)
	}
	if got != (pityState{}) {
		t.Errorf("expected a zero-value state, got %+v", got)
	}
}
