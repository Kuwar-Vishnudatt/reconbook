package review

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/matcher"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleResult() matcher.Result {
	return matcher.Result{
		Matches: []matcher.Match{
			{BankEntryID: "b1", POSTransactionIDs: []string{"p1"}, Kind: matcher.MatchExact, Confidence: matcher.ConfidenceHigh, ExpectedNetCents: 4969, ActualBankCents: 4969, Explanation: "exact"},
			{BankEntryID: "b2", POSTransactionIDs: []string{"p2", "p3"}, Kind: matcher.MatchBatch, Confidence: matcher.ConfidenceMedium, ExpectedNetCents: 9900, ActualBankCents: 9910, DiscrepancyCents: 10, Explanation: "batch"},
		},
		UnmatchedBank: []model.BankEntry{{ID: "b3", Date: time.Now(), AmountCents: 50000, Memo: "mystery deposit"}},
		UnmatchedPOS:  []model.POSTransaction{{ID: "p4", Date: time.Now(), AmountCents: 1500, Description: "unmatched sale"}},
	}
}

func TestSaveResultPersistsEverything(t *testing.T) {
	s := openTestStore(t)
	if err := s.SaveResult(sampleResult(), time.Now()); err != nil {
		t.Fatal(err)
	}

	items, err := s.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 review items, got %d", len(items))
	}
	for _, it := range items {
		if it.Status != StatusPending {
			t.Fatalf("expected new items to start pending, got %s", it.Status)
		}
	}

	unmatchedBank, err := s.UnmatchedBank()
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatchedBank) != 1 || unmatchedBank[0].ID != "b3" {
		t.Fatalf("expected b3 in unmatched bank, got %+v", unmatchedBank)
	}

	unmatchedPOS, err := s.UnmatchedPOS()
	if err != nil {
		t.Fatal(err)
	}
	if len(unmatchedPOS) != 1 || unmatchedPOS[0].ID != "p4" {
		t.Fatalf("expected p4 in unmatched pos, got %+v", unmatchedPOS)
	}
}

func TestApproveTransitionsStatusAndRecordsReviewer(t *testing.T) {
	s := openTestStore(t)
	s.SaveResult(sampleResult(), time.Now())

	now := time.Now()
	if err := s.Approve("b1", "jane-the-bookkeeper", now); err != nil {
		t.Fatal(err)
	}

	items, _ := s.ListByStatus(StatusApproved)
	if len(items) != 1 || items[0].BankEntryID != "b1" {
		t.Fatalf("expected b1 approved, got %+v", items)
	}
	if items[0].Reviewer != "jane-the-bookkeeper" {
		t.Fatalf("expected reviewer recorded, got %q", items[0].Reviewer)
	}
	if items[0].ReviewedAt == nil {
		t.Fatal("expected ReviewedAt to be set")
	}
}

func TestRejectRequiresAReason(t *testing.T) {
	s := openTestStore(t)
	s.SaveResult(sampleResult(), time.Now())

	if err := s.Reject("b1", "jane", "", time.Now()); err == nil {
		t.Fatal("expected an error for an empty rejection reason")
	}
}

func TestRejectRecordsReason(t *testing.T) {
	s := openTestStore(t)
	s.SaveResult(sampleResult(), time.Now())

	if err := s.Reject("b2", "jane", "wrong transactions grouped", time.Now()); err != nil {
		t.Fatal(err)
	}
	items, _ := s.ListByStatus(StatusRejected)
	if len(items) != 1 || items[0].RejectionReason != "wrong transactions grouped" {
		t.Fatalf("expected rejection reason recorded, got %+v", items)
	}
}

func TestCannotDecideAnAlreadyDecidedItem(t *testing.T) {
	s := openTestStore(t)
	s.SaveResult(sampleResult(), time.Now())
	s.Approve("b1", "jane", time.Now())

	if err := s.Approve("b1", "someone-else", time.Now()); err == nil {
		t.Fatal("expected an error re-approving an already-approved item (audit trail must not be overwritten)")
	}
	if err := s.Reject("b1", "someone-else", "changed my mind", time.Now()); err == nil {
		t.Fatal("expected an error rejecting an already-approved item")
	}
}

func TestDecidingNonexistentItemErrors(t *testing.T) {
	s := openTestStore(t)
	if err := s.Approve("does-not-exist", "jane", time.Now()); err == nil {
		t.Fatal("expected an error approving a review item that was never created")
	}
}

func TestSummaryCountsByStatus(t *testing.T) {
	s := openTestStore(t)
	s.SaveResult(sampleResult(), time.Now())
	s.Approve("b1", "jane", time.Now())
	s.Reject("b2", "jane", "no", time.Now())

	sm, err := s.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if sm.Approved != 1 || sm.Rejected != 1 || sm.Pending != 0 {
		t.Fatalf("unexpected summary: %+v", sm)
	}
}

func TestSaveResultIsIdempotentForRerunning(t *testing.T) {
	// Re-saving the same result (e.g. the operator re-ran reconciliation
	// after re-importing the same files) must not create duplicate rows.
	s := openTestStore(t)
	s.SaveResult(sampleResult(), time.Now())
	s.SaveResult(sampleResult(), time.Now())

	items, err := s.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected re-saving the same result to not duplicate rows, got %d items", len(items))
	}
}
