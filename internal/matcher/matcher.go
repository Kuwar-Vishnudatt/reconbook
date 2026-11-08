// Package matcher reconciles POS sales against bank deposits. The central
// difficulty this package solves: a bank deposit is almost never the exact
// amount of one POS sale — it's the processor's batched payout of SEVERAL
// sales from a day or two, net of card-processing fees. Finding "which
// subset of these unclaimed sales nets out to this deposit" is a subset-
// sum problem, and this package solves it with a real bounded dynamic-
// programming search rather than a greedy approximation, because a wrong
// match in bookkeeping software is a correctness bug with financial
// consequences, not a UX nitpick.
package matcher

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/model"
)

type MatchKind string

const (
	MatchExact MatchKind = "exact"
	MatchBatch MatchKind = "batch"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

// Config tunes the fee model and search bounds.
type Config struct {
	DateWindowDays int     // how many days before the deposit date a POS sale may fall and still be considered
	FeeRate        float64 // card-processing percentage fee, e.g. 0.029 for 2.9%
	FixedFeeCents  int64   // fixed per-transaction processing fee, e.g. 30 for $0.30
	ToleranceCents int64   // slack beyond the fee model, absorbing rounding across many transactions
	MaxCandidates  int     // safety cap on subset-sum search input size — see docs/ARCHITECTURE.md
}

func (c Config) withDefaults() Config {
	if c.DateWindowDays == 0 {
		c.DateWindowDays = 3
	}
	if c.ToleranceCents == 0 {
		c.ToleranceCents = 50 // 50 cents
	}
	if c.MaxCandidates == 0 {
		c.MaxCandidates = 80
	}
	return c
}

// NetOf returns a sale's amount after estimated card-processing fees — what
// we'd expect it to contribute to a batched bank deposit.
func (c Config) NetOf(amountCents int64) int64 {
	fee := int64(math.Round(float64(amountCents)*c.FeeRate)) + c.FixedFeeCents
	return amountCents - fee
}

// Match is one proposed correspondence between a bank deposit and the POS
// sale(s) that (after fees) should net out to it.
type Match struct {
	BankEntryID       string
	POSTransactionIDs []string
	Kind              MatchKind
	Confidence        Confidence
	ExpectedNetCents  int64
	ActualBankCents   int64
	DiscrepancyCents  int64 // ActualBankCents - ExpectedNetCents; 0 is a perfect match
	Explanation       string
}

// Result is the full reconciliation outcome.
type Result struct {
	Matches       []Match
	UnmatchedBank []model.BankEntry
	UnmatchedPOS  []model.POSTransaction
}

// Reconcile matches every bank entry against a subset of the POS sales,
// claiming POS transactions greedily in order of deposit date so an
// earlier (and therefore tightest date-windowed) deposit can't have its
// natural match stolen by a later one considered first. Refunds (negative
// POS amounts) are only matched via the single-transaction exact pass —
// see docs/ARCHITECTURE.md for why batch (subset-sum) matching is scoped
// to sales only.
func Reconcile(pos []model.POSTransaction, bank []model.BankEntry, cfg Config) Result {
	cfg = cfg.withDefaults()

	bankSorted := append([]model.BankEntry(nil), bank...)
	sort.Slice(bankSorted, func(i, j int) bool { return bankSorted[i].Date.Before(bankSorted[j].Date) })

	claimed := make(map[string]bool, len(pos))

	var matches []Match
	var unmatchedBank []model.BankEntry

	for _, b := range bankSorted {
		candidates := unclaimedWithinWindow(pos, claimed, b.Date, cfg.DateWindowDays)

		if m, ok := tryExactMatch(b, candidates, cfg); ok {
			matches = append(matches, m)
			claimed[m.POSTransactionIDs[0]] = true
			continue
		}

		if m, ok := trySubsetMatch(b, candidates, cfg); ok {
			matches = append(matches, m)
			for _, id := range m.POSTransactionIDs {
				claimed[id] = true
			}
			continue
		}

		unmatchedBank = append(unmatchedBank, b)
	}

	var unmatchedPOS []model.POSTransaction
	for _, p := range pos {
		if !claimed[p.ID] {
			unmatchedPOS = append(unmatchedPOS, p)
		}
	}

	return Result{Matches: matches, UnmatchedBank: unmatchedBank, UnmatchedPOS: unmatchedPOS}
}

func unclaimedWithinWindow(pos []model.POSTransaction, claimed map[string]bool, bankDate time.Time, windowDays int) []model.POSTransaction {
	var out []model.POSTransaction
	earliest := bankDate.AddDate(0, 0, -windowDays)
	for _, p := range pos {
		if claimed[p.ID] {
			continue
		}
		if p.Date.After(bankDate) || p.Date.Before(earliest) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func tryExactMatch(b model.BankEntry, candidates []model.POSTransaction, cfg Config) (Match, bool) {
	var best *model.POSTransaction
	var bestDiscrepancy int64 = math.MaxInt64

	for i, p := range candidates {
		net := cfg.NetOf(p.AmountCents)
		d := b.AmountCents - net
		if abs64(d) <= cfg.ToleranceCents && abs64(d) < abs64(bestDiscrepancy) {
			best = &candidates[i]
			bestDiscrepancy = d
		}
	}
	if best == nil {
		return Match{}, false
	}

	net := cfg.NetOf(best.AmountCents)
	return Match{
		BankEntryID:       b.ID,
		POSTransactionIDs: []string{best.ID},
		Kind:              MatchExact,
		Confidence:        confidenceFor(bestDiscrepancy, cfg.ToleranceCents, 1, cfg.MaxCandidates),
		ExpectedNetCents:  net,
		ActualBankCents:   b.AmountCents,
		DiscrepancyCents:  bestDiscrepancy,
		Explanation: fmt.Sprintf(
			"Deposit of %s matches single sale %s (%s) on %s, net of estimated fees ~%s.",
			formatCents(b.AmountCents), best.ID, formatCents(best.AmountCents), best.Date.Format("2006-01-02"), formatCents(best.AmountCents-net),
		),
	}, true
}

func trySubsetMatch(b model.BankEntry, candidates []model.POSTransaction, cfg Config) (Match, bool) {
	// Only positive (sale) amounts participate in batch/subset matching —
	// see the package doc comment for why refunds are out of scope here.
	var sales []model.POSTransaction
	for _, p := range candidates {
		if p.AmountCents > 0 {
			sales = append(sales, p)
		}
	}
	if len(sales) == 0 || len(sales) > cfg.MaxCandidates {
		return Match{}, false
	}

	nets := make([]int64, len(sales))
	for i, p := range sales {
		nets[i] = cfg.NetOf(p.AmountCents)
	}

	target := b.AmountCents
	upper := target + cfg.ToleranceCents
	if upper < 0 {
		return Match{}, false
	}

	indices, achieved, ok := subsetSumClosest(nets, target, upper, cfg.ToleranceCents)
	if !ok || len(indices) < 2 {
		// len < 2 means no genuine BATCH is needed here (a single-item
		// result would have already been found by tryExactMatch, and this
		// package only calls trySubsetMatch after that fails) — treat as
		// no match rather than silently duplicating the exact-match path.
		return Match{}, false
	}

	ids := make([]string, len(indices))
	for i, idx := range indices {
		ids[i] = sales[idx].ID
	}
	discrepancy := target - achieved

	return Match{
		BankEntryID:       b.ID,
		POSTransactionIDs: ids,
		Kind:              MatchBatch,
		Confidence:        confidenceFor(discrepancy, cfg.ToleranceCents, len(indices), cfg.MaxCandidates),
		ExpectedNetCents:  achieved,
		ActualBankCents:   b.AmountCents,
		DiscrepancyCents:  discrepancy,
		Explanation: fmt.Sprintf(
			"Deposit of %s matches a batch of %d sales totaling %s net of estimated fees (discrepancy %s).",
			formatCents(b.AmountCents), len(indices), formatCents(achieved), formatCents(discrepancy),
		),
	}, true
}

func confidenceFor(discrepancyCents, toleranceCents int64, subsetSize, maxCandidates int) Confidence {
	d := abs64(discrepancyCents)
	switch {
	case d == 0 && subsetSize <= 3:
		return ConfidenceHigh
	case d <= toleranceCents/2 && subsetSize < maxCandidates/2:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func formatCents(c int64) string {
	neg := ""
	if c < 0 {
		neg = "-"
		c = -c
	}
	return fmt.Sprintf("%s$%d.%02d", neg, c/100, c%100)
}
