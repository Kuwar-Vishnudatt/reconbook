package importer

import (
	"strings"
	"testing"
)

func TestParseDollarsToCentsBasic(t *testing.T) {
	cases := map[string]int64{
		"49.99":   4999,
		"0.01":    1,
		"1234":    123400,
		"1234.5":  123450,
		"1234.50": 123450,
		"-12.50":  -1250,
		"+5.00":   500,
		"$9.99":   999,
		"0":       0,
		"0.00":    0,
	}
	for in, want := range cases {
		got, err := parseDollarsToCents(in)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", in, err)
		}
		if got != want {
			t.Errorf("%q: got %d, want %d", in, got, want)
		}
	}
}

func TestParseDollarsToCentsNoFloatRoundingTrap(t *testing.T) {
	// The classic float trap: 49.99 * 100 in float64 arithmetic can land
	// on 4998.999999999999 before truncation, silently becoming 4998
	// cents instead of 4999. Run many values that are known float-tricky
	// and confirm the string-based parser never falls into it.
	tricky := []string{"49.99", "19.99", "0.29", "0.1", "0.3", "2.33", "19.37", "99.99"}
	wantCents := []int64{4999, 1999, 29, 10, 30, 233, 1937, 9999}
	for i, s := range tricky {
		got, err := parseDollarsToCents(s)
		if err != nil {
			t.Fatal(err)
		}
		if got != wantCents[i] {
			t.Errorf("%q: got %d cents, want %d", s, got, wantCents[i])
		}
	}
}

func TestParseDollarsToCentsRejectsTooManyDecimalPlaces(t *testing.T) {
	if _, err := parseDollarsToCents("1.999"); err == nil {
		t.Fatal("expected an error for 3 decimal places")
	}
}

func TestParseDollarsToCentsRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "abc", "1.2.3", "$"} {
		if _, err := parseDollarsToCents(s); err == nil {
			t.Fatalf("expected an error for %q", s)
		}
	}
}

func TestParsePOSValidCSV(t *testing.T) {
	csv := `transaction_id,date,amount,description
p1,2026-01-05,49.99,Latte
p2,2026-01-05,-5.00,Refund on p1`
	txns, err := ParsePOS(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns))
	}
	if txns[0].AmountCents != 4999 || txns[1].AmountCents != -500 {
		t.Fatalf("unexpected amounts: %+v", txns)
	}
}

func TestParsePOSColumnsCanBeReordered(t *testing.T) {
	// A real-world export's column order varies by provider; matching by
	// header name (not position) is what makes this robust to that.
	csv := `description,amount,transaction_id,date
Latte,49.99,p1,2026-01-05`
	txns, err := ParsePOS(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if txns[0].ID != "p1" || txns[0].AmountCents != 4999 {
		t.Fatalf("unexpected parse with reordered columns: %+v", txns)
	}
}

func TestParsePOSRejectsMissingColumn(t *testing.T) {
	csv := `transaction_id,date,amount
p1,2026-01-05,49.99`
	if _, err := ParsePOS(strings.NewReader(csv)); err == nil {
		t.Fatal("expected an error for a missing description column")
	}
}

func TestParsePOSRejectsBadDate(t *testing.T) {
	csv := `transaction_id,date,amount,description
p1,01/05/2026,49.99,Latte`
	if _, err := ParsePOS(strings.NewReader(csv)); err == nil {
		t.Fatal("expected an error for a non-ISO date")
	}
}

func TestParseBankValidCSV(t *testing.T) {
	csv := `entry_id,date,amount,memo
b1,2026-01-06,150.25,Daily card batch`
	entries, err := ParseBank(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].AmountCents != 15025 {
		t.Fatalf("unexpected parse: %+v", entries)
	}
}
