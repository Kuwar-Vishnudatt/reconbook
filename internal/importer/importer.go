// Package importer reads POS and bank export CSVs — the format a
// bookkeeper actually has on hand (exported from Square, Clover, Toast,
// QuickBooks, a bank's own portal), not a bespoke format this tool
// invents. Amounts in the CSV are decimal dollars (e.g. "49.99"); this
// package is the one place that boundary between human-facing decimal
// money and the rest of the system's integer-cents representation is
// crossed, so a parsing mistake here can't silently become a float-
// accumulation bug three layers downstream.
package importer

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/model"
)

const dateLayout = "2006-01-02"

// ParsePOS reads a CSV with header: transaction_id,date,amount,description
func ParsePOS(r io.Reader) ([]model.POSTransaction, error) {
	rows, err := readCSV(r, []string{"transaction_id", "date", "amount", "description"})
	if err != nil {
		return nil, err
	}

	out := make([]model.POSTransaction, 0, len(rows))
	for i, row := range rows {
		date, err := time.Parse(dateLayout, row["date"])
		if err != nil {
			return nil, fmt.Errorf("row %d: parse date %q: %w", i+2, row["date"], err)
		}
		cents, err := parseDollarsToCents(row["amount"])
		if err != nil {
			return nil, fmt.Errorf("row %d: parse amount %q: %w", i+2, row["amount"], err)
		}
		if row["transaction_id"] == "" {
			return nil, fmt.Errorf("row %d: missing transaction_id", i+2)
		}
		out = append(out, model.POSTransaction{
			ID: row["transaction_id"], Date: date, AmountCents: cents, Description: row["description"],
		})
	}
	return out, nil
}

// ParseBank reads a CSV with header: entry_id,date,amount,memo
func ParseBank(r io.Reader) ([]model.BankEntry, error) {
	rows, err := readCSV(r, []string{"entry_id", "date", "amount", "memo"})
	if err != nil {
		return nil, err
	}

	out := make([]model.BankEntry, 0, len(rows))
	for i, row := range rows {
		date, err := time.Parse(dateLayout, row["date"])
		if err != nil {
			return nil, fmt.Errorf("row %d: parse date %q: %w", i+2, row["date"], err)
		}
		cents, err := parseDollarsToCents(row["amount"])
		if err != nil {
			return nil, fmt.Errorf("row %d: parse amount %q: %w", i+2, row["amount"], err)
		}
		if row["entry_id"] == "" {
			return nil, fmt.Errorf("row %d: missing entry_id", i+2)
		}
		out = append(out, model.BankEntry{ID: row["entry_id"], Date: date, AmountCents: cents, Memo: row["memo"]})
	}
	return out, nil
}

func readCSV(r io.Reader, requiredCols []string) ([]map[string]string, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	colIndex := make(map[string]int, len(header))
	for i, h := range header {
		colIndex[strings.TrimSpace(h)] = i
	}
	for _, c := range requiredCols {
		if _, ok := colIndex[c]; !ok {
			return nil, fmt.Errorf("missing required column %q (found: %v)", c, header)
		}
	}

	var rows []map[string]string
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		row := make(map[string]string, len(colIndex))
		for col, idx := range colIndex {
			if idx < len(record) {
				row[col] = strings.TrimSpace(record[idx])
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// parseDollarsToCents converts a human-entered decimal-dollar string (e.g.
// "49.99", "-12.50", "1234") into integer cents. Parsing via string
// manipulation of the decimal point — not via float64 multiplication by
// 100 — specifically avoids the classic "49.99 * 100 = 4998.999999999"
// floating-point representation trap that a naive conversion would hit.
func parseDollarsToCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	s = strings.TrimPrefix(s, "$")

	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid whole-dollar part %q: %w", parts[0], err)
	}

	var frac int64
	if len(parts) == 2 {
		fracStr := parts[1]
		switch len(fracStr) {
		case 0:
			fracStr = "00"
		case 1:
			fracStr += "0"
		case 2:
			// exact
		default:
			return 0, fmt.Errorf("amount %q has more than 2 decimal places", s)
		}
		frac, err = strconv.ParseInt(fracStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid fractional part %q: %w", fracStr, err)
		}
	}

	cents := whole*100 + frac
	if neg {
		cents = -cents
	}
	return cents, nil
}
