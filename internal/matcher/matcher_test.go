package matcher

import (
	"testing"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/model"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func testConfig() Config {
	return Config{DateWindowDays: 3, FeeRate: 0.029, FixedFeeCents: 30, ToleranceCents: 50, MaxCandidates: 80}
}

func TestExactSingleSaleMatch(t *testing.T) {
	cfg := testConfig()
	sale := model.POSTransaction{ID: "p1", Date: d("2026-01-05"), AmountCents: 10000}
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-06"), AmountCents: cfg.NetOf(10000)}

	res := Reconcile([]model.POSTransaction{sale}, []model.BankEntry{deposit}, cfg)
	if len(res.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(res.Matches))
	}
	m := res.Matches[0]
	if m.Kind != MatchExact {
		t.Fatalf("expected an exact match, got %s", m.Kind)
	}
	if len(res.UnmatchedPOS) != 0 || len(res.UnmatchedBank) != 0 {
		t.Fatalf("expected nothing left unmatched, got %+v", res)
	}
}

func TestBatchMatchAcrossMultipleSales(t *testing.T) {
	cfg := testConfig()
	sales := []model.POSTransaction{
		{ID: "p1", Date: d("2026-01-05"), AmountCents: 5000},
		{ID: "p2", Date: d("2026-01-05"), AmountCents: 7500},
		{ID: "p3", Date: d("2026-01-05"), AmountCents: 3200},
	}
	totalNet := cfg.NetOf(5000) + cfg.NetOf(7500) + cfg.NetOf(3200)
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-06"), AmountCents: totalNet}

	res := Reconcile(sales, []model.BankEntry{deposit}, cfg)
	if len(res.Matches) != 1 {
		t.Fatalf("expected 1 batch match, got %d: %+v", len(res.Matches), res)
	}
	m := res.Matches[0]
	if m.Kind != MatchBatch {
		t.Fatalf("expected a batch match, got %s", m.Kind)
	}
	if len(m.POSTransactionIDs) != 3 {
		t.Fatalf("expected all 3 sales in the batch, got %v", m.POSTransactionIDs)
	}
	if len(res.UnmatchedPOS) != 0 {
		t.Fatalf("expected no unmatched sales, got %+v", res.UnmatchedPOS)
	}
}

func TestTwoDepositsDoNotStealEachOthersTransactions(t *testing.T) {
	// The conflict-avoidance property: two deposits, each claiming its own
	// disjoint batch of sales from an overlapping date window. Neither
	// deposit's match should include a sale the other deposit legitimately
	// needed.
	cfg := testConfig()
	sales := []model.POSTransaction{
		{ID: "p1", Date: d("2026-01-05"), AmountCents: 5000},
		{ID: "p2", Date: d("2026-01-05"), AmountCents: 5000},
		{ID: "p3", Date: d("2026-01-06"), AmountCents: 6000},
		{ID: "p4", Date: d("2026-01-06"), AmountCents: 6000},
	}
	deposit1 := model.BankEntry{ID: "b1", Date: d("2026-01-06"), AmountCents: cfg.NetOf(5000) + cfg.NetOf(5000)}
	deposit2 := model.BankEntry{ID: "b2", Date: d("2026-01-07"), AmountCents: cfg.NetOf(6000) + cfg.NetOf(6000)}

	res := Reconcile(sales, []model.BankEntry{deposit1, deposit2}, cfg)
	if len(res.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(res.Matches), res.Matches)
	}

	seen := map[string]string{} // pos id -> bank id that claimed it
	for _, m := range res.Matches {
		for _, id := range m.POSTransactionIDs {
			if other, dup := seen[id]; dup {
				t.Fatalf("POS transaction %s claimed by both %s and %s", id, other, m.BankEntryID)
			}
			seen[id] = m.BankEntryID
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected all 4 sales claimed exactly once total, got %d claims", len(seen))
	}
}

func TestUnmatchedBankEntrySurfacedWhenNoCandidateFits(t *testing.T) {
	cfg := testConfig()
	sale := model.POSTransaction{ID: "p1", Date: d("2026-01-05"), AmountCents: 5000}
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-06"), AmountCents: 999900} // wildly different amount

	res := Reconcile([]model.POSTransaction{sale}, []model.BankEntry{deposit}, cfg)
	if len(res.Matches) != 0 {
		t.Fatalf("expected no matches, got %+v", res.Matches)
	}
	if len(res.UnmatchedBank) != 1 || res.UnmatchedBank[0].ID != "b1" {
		t.Fatalf("expected b1 to be unmatched, got %+v", res.UnmatchedBank)
	}
	if len(res.UnmatchedPOS) != 1 || res.UnmatchedPOS[0].ID != "p1" {
		t.Fatalf("expected p1 to be unmatched, got %+v", res.UnmatchedPOS)
	}
}

func TestSaleOutsideDateWindowIsNotMatched(t *testing.T) {
	cfg := testConfig()
	cfg.DateWindowDays = 1
	sale := model.POSTransaction{ID: "p1", Date: d("2026-01-01"), AmountCents: 10000} // far before the deposit
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-10"), AmountCents: cfg.NetOf(10000)}

	res := Reconcile([]model.POSTransaction{sale}, []model.BankEntry{deposit}, cfg)
	if len(res.Matches) != 0 {
		t.Fatalf("expected no match across a 9-day gap with a 1-day window, got %+v", res.Matches)
	}
}

func TestRefundIsOnlyMatchedViaExactPassNotBatched(t *testing.T) {
	cfg := testConfig()
	refund := model.POSTransaction{ID: "r1", Date: d("2026-01-05"), AmountCents: -2000}
	sale := model.POSTransaction{ID: "p1", Date: d("2026-01-05"), AmountCents: 5000}
	// A deposit whose amount happens to equal sale+refund net would NOT be
	// found via subset-sum (refunds don't participate in batching) — this
	// deposit should go unmatched rather than silently including a refund
	// in a batch sum.
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-06"), AmountCents: cfg.NetOf(5000) + (-2000)}

	res := Reconcile([]model.POSTransaction{refund, sale}, []model.BankEntry{deposit}, cfg)
	if len(res.Matches) != 0 {
		t.Fatalf("expected refunds to never participate in batch matching, got %+v", res.Matches)
	}
}

func TestRefundMatchedExactly(t *testing.T) {
	cfg := testConfig()
	refund := model.POSTransaction{ID: "r1", Date: d("2026-01-05"), AmountCents: -2000}
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-06"), AmountCents: cfg.NetOf(-2000)}

	res := Reconcile([]model.POSTransaction{refund}, []model.BankEntry{deposit}, cfg)
	if len(res.Matches) != 1 || res.Matches[0].Kind != MatchExact {
		t.Fatalf("expected an exact match for a single refund, got %+v", res)
	}
}

func TestHighConfidenceForExactZeroDiscrepancy(t *testing.T) {
	cfg := testConfig()
	sale := model.POSTransaction{ID: "p1", Date: d("2026-01-05"), AmountCents: 10000}
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-05"), AmountCents: cfg.NetOf(10000)}

	res := Reconcile([]model.POSTransaction{sale}, []model.BankEntry{deposit}, cfg)
	if res.Matches[0].Confidence != ConfidenceHigh {
		t.Fatalf("expected high confidence for a zero-discrepancy match, got %s", res.Matches[0].Confidence)
	}
}

func TestLowConfidenceWhenUsingMostOfTolerance(t *testing.T) {
	cfg := testConfig()
	cfg.ToleranceCents = 100
	sale := model.POSTransaction{ID: "p1", Date: d("2026-01-05"), AmountCents: 10000}
	// Deposit off by 95 cents from the expected net — within the 100-cent
	// tolerance, but using nearly all of it.
	deposit := model.BankEntry{ID: "b1", Date: d("2026-01-05"), AmountCents: cfg.NetOf(10000) + 95}

	res := Reconcile([]model.POSTransaction{sale}, []model.BankEntry{deposit}, cfg)
	if len(res.Matches) != 1 {
		t.Fatalf("expected a match within tolerance, got %+v", res)
	}
	if res.Matches[0].Confidence != ConfidenceLow {
		t.Fatalf("expected low confidence when nearly exhausting the tolerance budget, got %s", res.Matches[0].Confidence)
	}
}
