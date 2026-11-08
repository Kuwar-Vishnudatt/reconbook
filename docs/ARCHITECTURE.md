# Architecture

## What problem this solves

Real bookkeeping reconciliation is hard for a specific, structural reason:
a POS system and a bank/accounting ledger don't share IDs, and a bank
deposit is almost never the amount of one sale — it's a card processor's
*batched payout* of several sales from a day or two, net of processing
fees it doesn't itemize per transaction. A bookkeeper doing this by hand
is mentally solving a subset-sum problem against a decimal-money system
that punishes rounding mistakes. This project automates that matching with
a real bounded search (not a heuristic approximation), while keeping every
proposed match reviewable and every decision auditable — because a
reconciliation tool that silently auto-applies its own guesses to
financial records isn't a tool a bookkeeper could responsibly use.

## Integer cents, never float64, anywhere money is summed or compared

`internal/model` and every package downstream of it represent money as
`int64` cents. `internal/importer` is the one place a human-entered
decimal-dollar string (`"49.99"`) is parsed, and it's done via string
manipulation of the decimal point — not `float64(dollars) * 100` — because
that multiplication is a textbook floating-point trap: `49.99 * 100` does
not reliably land on exactly `4999.0` in IEEE 754 arithmetic.
`TestParseDollarsToCentsNoFloatRoundingTrap` runs a set of known-tricky
values through the parser specifically to keep this invariant from quietly
regressing. Reconciliation is fundamentally a sum-equality problem; a
rounding bug in money arithmetic is not a cosmetic issue, it's the
software silently getting the books wrong.

## The matching problem: subset-sum, solved with a real bounded DP

`internal/matcher` tries two passes against each unmatched bank deposit,
in order:

1. **Exact (single-transaction) match**: is there one sale whose
   fee-adjusted net falls within tolerance of the deposit? This also
   handles refunds, which are deliberately excluded from pass 2 — see
   below.
2. **Batch (subset-sum) match**: is there a *combination* of same-period
   sales whose fee-adjusted nets sum to within tolerance of the deposit?

Pass 2 is solved with a genuine 0/1 subset-sum reachability DP
(`internal/matcher/subsetsum.go`), not a greedy "add sales until close
enough" approximation — a greedy approach can miss the correct combination
or converge on a wrong one that merely looks plausible, which is a much
worse failure mode in bookkeeping software than simply failing to find a
match at all (an unmatched deposit gets a human's attention; a *wrong*
match might not). The DP is bounded to a table width of
`target + tolerance` cents (anything beyond can never land in range) and a
hard cap (`maxDPWidth`, 2,000,000 cents / $20,000) past which batch
matching is skipped entirely in favor of flagging the deposit for manual
review — an honest, documented scaling limit appropriate for the
realistic target (a small business's daily card-sales batch, tens of
transactions, not thousands).

### The reconstruction bug this design anticipates

Subset-sum reconstruction — backtracking through the DP table to recover
*which* items were chosen, not just that some sum was reachable — is a
classic place to get the backtrack direction wrong and silently report a
subset that doesn't actually sum to the claimed total.
`TestSubsetSumReconstructionPicksARealSubset` guards against exactly this:
it doesn't just check that an amount was found, it independently re-sums
the reconstructed indices and asserts they equal the reported amount,
across dozens of target values. This test would have caught a real version
of the bug it's named for; it happened to pass on first implementation
here, which the test still exists to keep true going forward.

## Why refunds don't participate in batch matching

A refund is a negative amount. Allowing negative values into the subset-
sum search would mean "closer to target" doesn't monotonically relate to
"more items included" the way it does for an all-positive search — the DP
as implemented assumes contributions only move the running sum upward
within a bounded window, which is what keeps it both correct and cheap.
Refunds are common enough to matter but typically deposited (or debited)
individually, so they're fully handled by the single-transaction exact
pass; `TestRefundIsOnlyMatchedViaExactPassNotBatched` confirms a refund
sitting alongside sales in the same date window is never silently folded
into a batch sum.

## Avoiding cross-deposit conflicts: process by date, claim immediately

Two deposits whose date windows overlap could, in principle, both want to
claim the same sale. `matcher.Reconcile` avoids this by processing bank
entries in date order and removing (`claimed[id] = true`) every
transaction a match uses *before* moving to the next deposit — so a later
deposit's search pool is always genuinely unclaimed, not independently
computed against the full set and reconciled after the fact.
`TestTwoDepositsDoNotStealEachOthersTransactions` verifies this directly:
two deposits with overlapping windows, asserting every sale is claimed by
exactly one of them, never both, never neither when it should have been
claimed.

This is a greedy, date-ordered assignment, not a globally optimal one — a
pathological input could in principle be assembled where a different
global assignment produces a better overall fit. For the realistic case
(deposits in roughly the order their underlying sales occurred), date
ordering is both the natural and the correct heuristic, and is called out
here as a deliberate v1 scope choice rather than an oversight.

## Confidence scoring, for a human who isn't going to read the DP

A bookkeeper reviewing matches needs "how sure is this" in plain language,
not a discrepancy number they have to interpret themselves.
`confidenceFor` is a simple, legible rule: zero discrepancy on a small
match is `high`; using up to half the configured tolerance budget is
`medium`; anything using more of that budget is `low` — surfaced directly
in the explanation text on every match, not just in a raw number.

## The review workflow: every match proposed, nothing auto-applied

`internal/review` persists every proposed match as `pending` and requires
an explicit `Approve` or `Reject` (with a mandatory reason) from a named
reviewer before it's considered decided — `TestCannotDecideAnAlreadyDecidedItem`
confirms a decision, once made, can't be silently overwritten by a second
call, which is what keeps the audit trail (who decided this, and when)
trustworthy rather than merely advisory.

## Testing strategy

- `internal/importer`: CSV parsing including column-reordering robustness
  (match by header name, not position — real exports from different
  providers don't agree on column order) and the float-rounding-trap
  regression test.
- `internal/matcher`: exact and batch matching, the cross-deposit conflict
  test, date-window boundaries, refund scoping, and confidence-level
  assignment — plus `subsetsum_test.go`'s dedicated adversarial
  reconstruction test.
- `internal/review`: the approve/reject state machine, the mandatory
  rejection reason, idempotent re-saving after a reconciliation re-run,
  and that an already-decided item can't be silently redecided.
- `internal/httpapi`: the review dashboard's JSON endpoints and the
  decide-endpoint's input validation.
- `test/integration`: the bundled example CSVs
  (`testdata/pos_export.csv`, `testdata/bank_export.csv`) run through the
  complete pipeline — import, reconcile, persist, approve, reject — with
  every expected match, unmatched item, and final review-queue count
  asserted against a hand-computed expectation, not just "it ran without
  erroring."

## What's deliberately out of scope (v1)

- **Globally optimal cross-deposit assignment** — see above; date-ordered
  greedy claiming is the chosen, documented tradeoff.
- **A configurable/learned fee model.** `Config.FeeRate` and
  `FixedFeeCents` are flat, user-supplied estimates; a real processor's
  fee schedule can vary by card type, volume tier, or have its own
  itemized statement — this tool's tolerance band is what absorbs that
  variance rather than modeling it precisely.
- **Multi-currency support.** Amounts are a single implicit currency's
  cents throughout.
