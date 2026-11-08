package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/importer"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/matcher"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/review"
)

// TestEndToEndCSVImportReconcileReview runs the bundled example CSVs
// (testdata/pos_export.csv, testdata/bank_export.csv) through the full
// pipeline — CSV import, matching, persisting to a review store — and
// asserts on the exact expected outcome, hand-computed against the fee
// model: 2 exact matches (a single sale, and a single refund), 1 batch
// match (3 same-day sales netting to one deposit), 1 unmatched deposit
// with no fitting combination, and 1 unmatched sale not yet deposited.
func TestEndToEndCSVImportReconcileReview(t *testing.T) {
	root := findRepoRoot(t)

	posFile, err := os.Open(filepath.Join(root, "testdata", "pos_export.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer posFile.Close()
	pos, err := importer.ParsePOS(posFile)
	if err != nil {
		t.Fatal(err)
	}

	bankFile, err := os.Open(filepath.Join(root, "testdata", "bank_export.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer bankFile.Close()
	bank, err := importer.ParseBank(bankFile)
	if err != nil {
		t.Fatal(err)
	}

	cfg := matcher.Config{DateWindowDays: 3, FeeRate: 0.029, FixedFeeCents: 30, ToleranceCents: 50}
	res := matcher.Reconcile(pos, bank, cfg)

	if len(res.Matches) != 3 {
		t.Fatalf("expected 3 matches, got %d: %+v", len(res.Matches), res.Matches)
	}
	if len(res.UnmatchedBank) != 1 || res.UnmatchedBank[0].ID != "b3" {
		t.Fatalf("expected b3 (the $999 mystery deposit) unmatched, got %+v", res.UnmatchedBank)
	}
	if len(res.UnmatchedPOS) != 1 || res.UnmatchedPOS[0].ID != "p5" {
		t.Fatalf("expected p5 (not yet deposited) unmatched, got %+v", res.UnmatchedPOS)
	}

	byBankID := map[string]matcher.Match{}
	for _, m := range res.Matches {
		byBankID[m.BankEntryID] = m
	}

	batch, ok := byBankID["b1"]
	if !ok || batch.Kind != matcher.MatchBatch || len(batch.POSTransactionIDs) != 3 {
		t.Fatalf("expected b1 to be a 3-sale batch match, got %+v", batch)
	}

	exactSale, ok := byBankID["b2"]
	if !ok || exactSale.Kind != matcher.MatchExact || exactSale.POSTransactionIDs[0] != "p4" {
		t.Fatalf("expected b2 to exactly match sale p4, got %+v", exactSale)
	}

	exactRefund, ok := byBankID["b4"]
	if !ok || exactRefund.Kind != matcher.MatchExact || exactRefund.POSTransactionIDs[0] != "r1" {
		t.Fatalf("expected b4 to exactly match refund r1, got %+v", exactRefund)
	}

	// Persist and review end to end: approve the batch match, reject one
	// exact match with a reason, confirm the review store reflects both
	// decisions and the audit trail is intact.
	r, err := review.Open(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	now := time.Now()
	if err := r.SaveResult(res, now); err != nil {
		t.Fatal(err)
	}
	if err := r.Approve("b1", "jane-the-bookkeeper", now); err != nil {
		t.Fatal(err)
	}
	if err := r.Reject("b2", "jane-the-bookkeeper", "need to confirm with the catering vendor first", now); err != nil {
		t.Fatal(err)
	}

	summary, err := r.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Approved != 1 || summary.Rejected != 1 || summary.Pending != 1 {
		t.Fatalf("expected 1 approved, 1 rejected, 1 still pending, got %+v", summary)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}
