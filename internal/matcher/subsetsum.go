package matcher

import "math"

// maxDPWidth bounds the subset-sum DP table's width (target+tolerance in
// cents) to keep memory bounded: the DP table is O(candidates * width)
// booleans. 2,000,000 cents ($20,000) is comfortably larger than a single
// day's card-sales deposit for the realistic small-business use case this
// tool targets; a deposit larger than that skips batch matching entirely
// (falls through to unmatched, flagged for manual review) rather than
// risking unbounded memory — see docs/ARCHITECTURE.md.
const maxDPWidth = 2_000_000

// subsetSumClosest finds a subset of nets whose sum lands as close as
// possible to target, within [target-tolerance, upper] (upper =
// target+tolerance), and returns the indices of the chosen elements (into
// the original nets slice) plus the sum actually achieved. Only
// strictly-positive values participate — see matcher.go's doc comment.
//
// This is the classic 0/1-subset-sum reachability DP (dp[i][s] = can the
// first i items sum to exactly s), bounded to s in [0, upper] since
// anything beyond upper can never land within tolerance of target.
// Reconstruction backtracks: at step i, item i-1 was necessary to reach
// sum s if-and-only-if s is reachable using the first i items but NOT
// reachable using just the first i-1 — a classic place to get the
// direction of this check backwards, which is why it has a dedicated
// adversarial test (TestSubsetSumReconstructionPicksARealSubset) that
// verifies the reconstructed indices actually sum to the reported amount,
// not just that AN amount was found.
func subsetSumClosest(nets []int64, target, upper, tolerance int64) (indices []int, achieved int64, ok bool) {
	n := len(nets)
	if upper < 0 || upper > maxDPWidth {
		return nil, 0, false
	}
	width := int(upper) + 1

	reachable := make([][]bool, n+1)
	reachable[0] = make([]bool, width)
	reachable[0][0] = true

	for i := 1; i <= n; i++ {
		reachable[i] = make([]bool, width)
		copy(reachable[i], reachable[i-1])
		amt := nets[i-1]
		if amt <= 0 || amt > upper {
			continue
		}
		for s := int(amt); s < width; s++ {
			if reachable[i-1][s-int(amt)] {
				reachable[i][s] = true
			}
		}
	}

	lower := target - tolerance
	if lower < 0 {
		lower = 0
	}

	bestSum := int64(-1)
	bestDiff := int64(math.MaxInt64)
	for s := int(lower); s <= int(upper); s++ {
		if reachable[n][s] {
			diff := abs64(target - int64(s))
			if diff < bestDiff {
				bestDiff = diff
				bestSum = int64(s)
			}
		}
	}
	if bestSum < 0 {
		return nil, 0, false
	}

	s := int(bestSum)
	for i := n; i >= 1; i-- {
		if reachable[i][s] && !reachable[i-1][s] {
			indices = append(indices, i-1)
			s -= int(nets[i-1])
		}
	}
	for l, r := 0, len(indices)-1; l < r; l, r = l+1, r-1 {
		indices[l], indices[r] = indices[r], indices[l]
	}

	return indices, bestSum, true
}
