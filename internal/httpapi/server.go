// Package httpapi is the review dashboard a bookkeeper actually uses:
// proposed matches with plain-English explanations and confidence levels,
// one-click approve/reject, and the two unmatched lists that are the real
// payoff of running reconciliation at all — "these need a human to sort
// out."
package httpapi

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/review"
)

//go:embed dashboard.html
var dashboardFS embed.FS

type Server struct {
	Review *review.Store
	Now    func() time.Time
	mux    *http.ServeMux
}

func New(r *review.Store) *Server {
	s := &Server{Review: r, Now: time.Now, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	s.mux.HandleFunc("/api/summary", s.handleSummary)
	s.mux.HandleFunc("/api/items", s.handleItems)
	s.mux.HandleFunc("/api/unmatched", s.handleUnmatched)
	s.mux.HandleFunc("/api/decide", s.handleDecide)
	s.mux.HandleFunc("/", s.handleDashboard)
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	sm, err := s.Review.Summary()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sm)
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.Review.ListAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func (s *Server) handleUnmatched(w http.ResponseWriter, r *http.Request) {
	bank, err := s.Review.UnmatchedBank()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pos, err := s.Review.UnmatchedPOS()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"bank": bank, "pos": pos})
}

func (s *Server) handleDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		BankEntryID string `json:"bank_entry_id"`
		Decision    string `json:"decision"` // "approve" or "reject"
		Reviewer    string `json:"reviewer"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "malformed request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.BankEntryID = strings.TrimSpace(req.BankEntryID)
	req.Reviewer = strings.TrimSpace(req.Reviewer)
	if req.BankEntryID == "" || req.Reviewer == "" {
		http.Error(w, "bank_entry_id and reviewer are required", http.StatusBadRequest)
		return
	}

	var err error
	switch req.Decision {
	case "approve":
		err = s.Review.Approve(req.BankEntryID, req.Reviewer, s.Now())
	case "reject":
		err = s.Review.Reject(req.BankEntryID, req.Reviewer, req.Reason, s.Now())
	default:
		http.Error(w, `decision must be "approve" or "reject"`, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, _ := dashboardFS.ReadFile("dashboard.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
