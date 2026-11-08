package matcher

import "testing"

func sumOf(nets []int64, indices []int) int64 {
	var s int64
	for _, i := range indices {
		s += nets[i]
	}
	return s
}

func TestSubsetSumFindsExactMatch(t *testing.T) {
	nets := []int64{1000, 2000, 3000}
	indices, achieved, ok := subsetSumClosest(nets, 5000, 5050, 50)
	if !ok {
		t.Fatal("expected a subset to be found")
	}
	if achieved != 5000 {
		t.Fatalf("expected exact 5000, got %d", achieved)
	}
	if sumOf(nets, indices) != achieved {
		t.Fatalf("reconstructed indices %v sum to %d, not the reported %d", indices, sumOf(nets, indices), achieved)
	}
}

// TestSubsetSumReconstructionPicksARealSubset is the adversarial test the
// package doc comment references: it doesn't just check that SOME amount
// was found, it verifies the reconstructed indices themselves actually
// sum to that amount — the exact class of bug a backtracking direction
// mistake would produce (reporting a plausible-looking total that the
// "chosen" items don't actually add up to).
func TestSubsetSumReconstructionPicksARealSubset(t *testing.T) {
	nets := []int64{1234, 5678, 999, 4321, 2500, 1111, 8888, 333}
	for target := int64(1000); target < 20000; target += 777 {
		indices, achieved, ok := subsetSumClosest(nets, target, target+100, 100)
		if !ok {
			continue
		}
		if sumOf(nets, indices) != achieved {
			t.Fatalf("target=%d: reconstructed indices %v sum to %d, reported achieved=%d", target, indices, sumOf(nets, indices), achieved)
		}
		// Indices must be unique (no item used twice).
		seen := map[int]bool{}
		for _, i := range indices {
			if seen[i] {
				t.Fatalf("target=%d: index %d used more than once in %v", target, i, indices)
			}
			seen[i] = true
		}
	}
}

func TestSubsetSumReturnsClosestWithinTolerance(t *testing.T) {
	nets := []int64{1000, 1040, 2000}
	// target 2030, tolerance 50: both 2000 (diff 30) and 2040 (1000+1040,
	// diff 10) are in range [1980, 2080] — must pick 2040, the closer one.
	indices, achieved, ok := subsetSumClosest(nets, 2030, 2080, 50)
	if !ok {
		t.Fatal("expected a match")
	}
	if achieved != 2040 {
		t.Fatalf("expected the closer sum 2040, got %d (indices=%v)", achieved, indices)
	}
}

func TestSubsetSumReturnsFalseWhenNothingWithinTolerance(t *testing.T) {
	nets := []int64{1000, 2000}
	_, _, ok := subsetSumClosest(nets, 500, 550, 50)
	if ok {
		t.Fatal("expected no match when nothing is reachable within tolerance")
	}
}

func TestSubsetSumIgnoresNonPositiveAmounts(t *testing.T) {
	nets := []int64{1000, -500, 0, 2000}
	indices, achieved, ok := subsetSumClosest(nets, 3000, 3050, 50)
	if !ok {
		t.Fatal("expected a match using only the positive entries")
	}
	if achieved != 3000 {
		t.Fatalf("expected 3000 (1000+2000), got %d", achieved)
	}
	for _, i := range indices {
		if nets[i] <= 0 {
			t.Fatalf("a non-positive entry (index %d, value %d) was included in the result", i, nets[i])
		}
	}
}

func TestSubsetSumRejectsOversizedTarget(t *testing.T) {
	nets := []int64{1000}
	_, _, ok := subsetSumClosest(nets, maxDPWidth+1000, maxDPWidth+1050, 50)
	if ok {
		t.Fatal("expected an oversized target to be rejected rather than attempt a huge DP table")
	}
}

func TestSubsetSumEmptyCandidatesNeverMatchesPositiveTarget(t *testing.T) {
	_, _, ok := subsetSumClosest(nil, 1000, 1050, 50)
	if ok {
		t.Fatal("expected no match with zero candidates")
	}
}

func TestSubsetSumZeroTargetMatchesEmptySubset(t *testing.T) {
	nets := []int64{1000, 2000}
	indices, achieved, ok := subsetSumClosest(nets, 0, 50, 50)
	if !ok {
		t.Fatal("expected the empty subset to satisfy a zero target")
	}
	if len(indices) != 0 || achieved != 0 {
		t.Fatalf("expected an empty subset summing to 0, got indices=%v achieved=%d", indices, achieved)
	}
}
