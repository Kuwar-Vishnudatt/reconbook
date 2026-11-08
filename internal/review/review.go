// Package review persists proposed matches for a human bookkeeper to
// approve or reject, with an audit trail (who, when, and — for a
// rejection — why). A reconciliation tool that silently auto-applies its
// own matches isn't trustworthy for financial records; this package is
// what makes every match reviewable and the review itself accountable.
package review

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/matcher"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/model"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

type Item struct {
	BankEntryID       string
	POSTransactionIDs []string
	Kind              matcher.MatchKind
	Confidence        matcher.Confidence
	ExpectedNetCents  int64
	ActualBankCents   int64
	DiscrepancyCents  int64
	Explanation       string
	Status            Status
	Reviewer          string
	ReviewedAt        *time.Time
	RejectionReason   string
	CreatedAt         time.Time
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	schema := `
	CREATE TABLE IF NOT EXISTS review_items (
		bank_entry_id TEXT PRIMARY KEY,
		pos_transaction_ids TEXT NOT NULL,
		kind TEXT NOT NULL,
		confidence TEXT NOT NULL,
		expected_net_cents INTEGER NOT NULL,
		actual_bank_cents INTEGER NOT NULL,
		discrepancy_cents INTEGER NOT NULL,
		explanation TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		reviewer TEXT NOT NULL DEFAULT '',
		reviewed_at TEXT,
		rejection_reason TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS unmatched_bank (
		entry_id TEXT PRIMARY KEY,
		date TEXT NOT NULL,
		amount_cents INTEGER NOT NULL,
		memo TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS unmatched_pos (
		transaction_id TEXT PRIMARY KEY,
		date TEXT NOT NULL,
		amount_cents INTEGER NOT NULL,
		description TEXT NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SaveResult persists an entire matcher.Result: proposed matches as
// pending review items, and both unmatched lists for visibility. Existing
// rows for the same keys are replaced — this models re-running
// reconciliation after importing corrected or additional data, not
// accumulating stale duplicate proposals.
func (s *Store) SaveResult(res matcher.Result, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, m := range res.Matches {
		idsJSON, _ := json.Marshal(m.POSTransactionIDs)
		_, err := tx.Exec(
			`INSERT OR REPLACE INTO review_items
			 (bank_entry_id, pos_transaction_ids, kind, confidence, expected_net_cents, actual_bank_cents, discrepancy_cents, explanation, status, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.BankEntryID, string(idsJSON), string(m.Kind), string(m.Confidence),
			m.ExpectedNetCents, m.ActualBankCents, m.DiscrepancyCents, m.Explanation,
			StatusPending, now.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("save match for %s: %w", m.BankEntryID, err)
		}
	}

	for _, b := range res.UnmatchedBank {
		_, err := tx.Exec(`INSERT OR REPLACE INTO unmatched_bank (entry_id, date, amount_cents, memo) VALUES (?, ?, ?, ?)`,
			b.ID, b.Date.Format("2006-01-02"), b.AmountCents, b.Memo)
		if err != nil {
			return fmt.Errorf("save unmatched bank %s: %w", b.ID, err)
		}
	}

	for _, p := range res.UnmatchedPOS {
		_, err := tx.Exec(`INSERT OR REPLACE INTO unmatched_pos (transaction_id, date, amount_cents, description) VALUES (?, ?, ?, ?)`,
			p.ID, p.Date.Format("2006-01-02"), p.AmountCents, p.Description)
		if err != nil {
			return fmt.Errorf("save unmatched pos %s: %w", p.ID, err)
		}
	}

	return tx.Commit()
}

// Approve records a bookkeeper's approval of a proposed match. Only a
// pending item can be approved — approving an already-decided item would
// silently overwrite who made the original call and when, corrupting the
// audit trail.
func (s *Store) Approve(bankEntryID, reviewer string, now time.Time) error {
	return s.decide(bankEntryID, StatusApproved, reviewer, "", now)
}

// Reject records a bookkeeper's rejection, with a reason — a rejection
// with no explanation is far less useful when someone looks at this audit
// trail in six months wondering why a deposit was never reconciled.
func (s *Store) Reject(bankEntryID, reviewer, reason string, now time.Time) error {
	if reason == "" {
		return fmt.Errorf("a rejection reason is required")
	}
	return s.decide(bankEntryID, StatusRejected, reviewer, reason, now)
}

func (s *Store) decide(bankEntryID string, status Status, reviewer, reason string, now time.Time) error {
	res, err := s.db.Exec(
		`UPDATE review_items SET status = ?, reviewer = ?, reviewed_at = ?, rejection_reason = ?
		 WHERE bank_entry_id = ? AND status = ?`,
		status, reviewer, now.UTC().Format(time.RFC3339Nano), reason, bankEntryID, StatusPending,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no pending review item for bank entry %s (already decided, or does not exist)", bankEntryID)
	}
	return nil
}

func (s *Store) ListByStatus(status Status) ([]Item, error) {
	rows, err := s.db.Query(
		`SELECT bank_entry_id, pos_transaction_ids, kind, confidence, expected_net_cents, actual_bank_cents, discrepancy_cents, explanation, status, reviewer, reviewed_at, rejection_reason, created_at
		 FROM review_items WHERE status = ? ORDER BY created_at ASC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (s *Store) ListAll() ([]Item, error) {
	rows, err := s.db.Query(
		`SELECT bank_entry_id, pos_transaction_ids, kind, confidence, expected_net_cents, actual_bank_cents, discrepancy_cents, explanation, status, reviewer, reviewed_at, rejection_reason, created_at
		 FROM review_items ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	var items []Item
	for rows.Next() {
		var it Item
		var idsJSON, kind, confidence, status, createdAt string
		var reviewedAt sql.NullString // NULL for a still-pending item — a plain string Scan target would fail on that NULL
		if err := rows.Scan(&it.BankEntryID, &idsJSON, &kind, &confidence, &it.ExpectedNetCents, &it.ActualBankCents,
			&it.DiscrepancyCents, &it.Explanation, &status, &it.Reviewer, &reviewedAt, &it.RejectionReason, &createdAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(idsJSON), &it.POSTransactionIDs); err != nil {
			return nil, err
		}
		it.Kind = matcher.MatchKind(kind)
		it.Confidence = matcher.Confidence(confidence)
		it.Status = Status(status)
		var err error
		if it.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
			return nil, err
		}
		if reviewedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, reviewedAt.String)
			if err != nil {
				return nil, err
			}
			it.ReviewedAt = &t
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Store) UnmatchedBank() ([]model.BankEntry, error) {
	rows, err := s.db.Query(`SELECT entry_id, date, amount_cents, memo FROM unmatched_bank ORDER BY date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.BankEntry
	for rows.Next() {
		var b model.BankEntry
		var dateStr string
		if err := rows.Scan(&b.ID, &dateStr, &b.AmountCents, &b.Memo); err != nil {
			return nil, err
		}
		if b.Date, err = time.Parse("2006-01-02", dateStr); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) UnmatchedPOS() ([]model.POSTransaction, error) {
	rows, err := s.db.Query(`SELECT transaction_id, date, amount_cents, description FROM unmatched_pos ORDER BY date ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.POSTransaction
	for rows.Next() {
		var p model.POSTransaction
		var dateStr string
		if err := rows.Scan(&p.ID, &dateStr, &p.AmountCents, &p.Description); err != nil {
			return nil, err
		}
		if p.Date, err = time.Parse("2006-01-02", dateStr); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type Summary struct {
	Pending  int
	Approved int
	Rejected int
}

func (s *Store) Summary() (Summary, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM review_items GROUP BY status`)
	if err != nil {
		return Summary{}, err
	}
	defer rows.Close()

	var sm Summary
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return Summary{}, err
		}
		switch Status(status) {
		case StatusPending:
			sm.Pending = count
		case StatusApproved:
			sm.Approved = count
		case StatusRejected:
			sm.Rejected = count
		}
	}
	return sm, rows.Err()
}
