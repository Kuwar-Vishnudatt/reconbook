// Package model defines the two transaction streams this tool reconciles.
// Amounts are always integer cents — never float64 — because reconciliation
// is fundamentally a sum-equality problem (does this subset of POS sales
// add up to this bank deposit?), and float accumulation error is exactly
// the kind of bug that would silently produce a wrong answer that LOOKS
// plausible: a deposit that's off by half a cent due to summing floats
// would either falsely match or falsely fail to match real money.
package model

import "time"

// POSTransaction is one sale (or refund) recorded by the point-of-sale
// system.
type POSTransaction struct {
	ID          string
	Date        time.Time
	AmountCents int64 // positive for a sale, negative for a refund
	Description string
}

// BankEntry is one deposit (or withdrawal) recorded on the accounting
// side — typically a bank statement line representing a batched payout
// of a day's (or several days') card sales, net of processor fees.
type BankEntry struct {
	ID          string
	Date        time.Time
	AmountCents int64
	Memo        string
}
