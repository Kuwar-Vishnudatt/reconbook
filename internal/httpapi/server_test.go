package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/matcher"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/review"
)

func openTestServer(t *testing.T) (*Server, *review.Store) {
	t.Helper()
	r, err := review.Open(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	srv := New(r)

	res := matcher.Result{
		Matches: []matcher.Match{
			{BankEntryID: "b1", POSTransactionIDs: []string{"p1"}, Kind: matcher.MatchExact, Confidence: matcher.ConfidenceHigh, Explanation: "exact match"},
		},
	}
	if err := r.SaveResult(res, time.Now()); err != nil {
		t.Fatal(err)
	}
	return srv, r
}

func TestSummaryEndpoint(t *testing.T) {
	srv, _ := openTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var sm map[string]int
	json.Unmarshal(rec.Body.Bytes(), &sm)
	if sm["Pending"] != 1 {
		t.Fatalf("expected 1 pending, got %v", sm)
	}
}

func TestItemsEndpoint(t *testing.T) {
	srv, _ := openTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var items []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 1 || items[0]["BankEntryID"] != "b1" {
		t.Fatalf("unexpected items: %v", items)
	}
}

func TestDecideApproveEndpoint(t *testing.T) {
	srv, r := openTestServer(t)
	body, _ := json.Marshal(map[string]string{"bank_entry_id": "b1", "decision": "approve", "reviewer": "jane"})
	req := httptest.NewRequest(http.MethodPost, "/api/decide", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	items, _ := r.ListByStatus(review.StatusApproved)
	if len(items) != 1 {
		t.Fatalf("expected b1 approved, got %+v", items)
	}
}

func TestDecideRejectRequiresReason(t *testing.T) {
	srv, _ := openTestServer(t)
	body, _ := json.Marshal(map[string]string{"bank_entry_id": "b1", "decision": "reject", "reviewer": "jane"})
	req := httptest.NewRequest(http.MethodPost, "/api/decide", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a reject with no reason, got %d", rec.Code)
	}
}

func TestDecideRejectsMissingReviewer(t *testing.T) {
	srv, _ := openTestServer(t)
	body, _ := json.Marshal(map[string]string{"bank_entry_id": "b1", "decision": "approve"})
	req := httptest.NewRequest(http.MethodPost, "/api/decide", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing reviewer, got %d", rec.Code)
	}
}

func TestDecideRejectsUnknownDecision(t *testing.T) {
	srv, _ := openTestServer(t)
	body, _ := json.Marshal(map[string]string{"bank_entry_id": "b1", "decision": "maybe", "reviewer": "jane"})
	req := httptest.NewRequest(http.MethodPost, "/api/decide", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown decision value, got %d", rec.Code)
	}
}

func TestDashboardServesHTML(t *testing.T) {
	srv, _ := openTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "reconbook") {
		t.Fatalf("expected the dashboard HTML, got code=%d", rec.Code)
	}
}
