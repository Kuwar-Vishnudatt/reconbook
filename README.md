# reconbook

A cross-system reconciliation tool for a bookkeeper: matches POS sales
against bank deposits when neither side shares an ID and a single deposit
is really a card processor's batched payout of several sales net of fees —
solved with a real bounded subset-sum search, not a heuristic guess, with
every proposed match reviewable (plain-English explanation + confidence
level) and every decision audited.

```
pos_export.csv ──▶ import ──┐
                             ├──▶ matcher.Reconcile ──▶ review queue ──▶ dashboard
bank_export.csv ──▶ import ──┘        │                  (SQLite,          (approve/
                                exact match first,         audit trail)      reject,
                                then subset-sum batch                       mandatory
                                match for the rest                         reason)
```

## Why this project

The FDE hiring research pointed at exactly this kind of problem: messy
real-world integration pain (two systems, no shared IDs, batched deposits)
where the differentiator is getting the hard matching logic genuinely
right and building something a non-technical reviewer can actually trust
and use — not a demo that only handles the easy 1:1 case. See
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full reasoning,
including why money is integer cents everywhere (never float64), how the
subset-sum reconstruction is specifically tested against the classic
backtracking-direction bug, and how cross-deposit claim conflicts are
avoided.

## Quickstart

```bash
make build
make test        # 40+ tests, no external services
make demo         # reconciles the bundled example exports, opens the review dashboard
```

```bash
./bin/reconbook reconcile -pos your_pos_export.csv -bank your_bank_export.csv
./bin/reconbook review   # opens http://localhost:8090/
```

CSV format (matched by header name, so column order doesn't matter):
```
# POS export
transaction_id,date,amount,description

# bank export
entry_id,date,amount,memo
```

## What's actually solved, concretely

- **Batched-deposit matching via real subset-sum search**: a deposit that's
  the sum of several same-period sales (net of estimated card-processing
  fees) is found by a bounded dynamic-programming search, with a dedicated
  test that independently re-sums the reconstructed match to catch the
  classic backtracking bug this kind of algorithm is prone to.
- **No float rounding traps**: money is integer cents from CSV parsing
  onward; a regression test runs known float-tricky decimal values (like
  `49.99`) through the parser to keep this honest.
- **No cross-deposit double-claiming**: two deposits with overlapping
  candidate-sale windows never end up claiming the same sale — verified
  directly, not just assumed from the algorithm's design.
- **Nothing auto-applied**: every match is a reviewable proposal with a
  plain-English explanation and a confidence level; a decision requires a
  named reviewer, and a rejection requires a reason — and a decided item
  can't be silently redecided, keeping the audit trail trustworthy.

## Project layout

```
internal/model/       POSTransaction, BankEntry — money always in int64 cents
internal/importer/     CSV parsing (column-name-robust, no float rounding traps)
internal/matcher/       exact + subset-sum batch matching, confidence scoring
internal/review/        SQLite-backed approve/reject workflow + audit trail
internal/httpapi/        bookkeeper-facing review dashboard
cmd/reconbook/            CLI: reconcile, review
testdata/                 example POS/bank exports exercising every match kind
test/integration/         full pipeline against the example exports, hand-verified
docs/ARCHITECTURE.md      design rationale
```

## What's deliberately out of scope (v1)

See docs/ARCHITECTURE.md — no globally optimal cross-deposit assignment
(date-ordered greedy claiming is the documented tradeoff), no
configurable/learned fee model, no multi-currency support.
