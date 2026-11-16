# reconbook

**A reconciliation tool that matches your point-of-sale sales against your bank deposits — and shows its work.**

If you've ever sat with a bank statement in one tab and a Square/Clover/Toast sales report in another, trying to figure out which deposit corresponds to which day's sales, this tool automates that process. "Reconciliation," in bookkeeping, just means confirming that two independent records of the same money — what the point-of-sale (POS) system says you sold, and what the bank says actually landed in your account — agree with each other. `reconbook` reads a CSV export from each side, proposes a match for every bank deposit it can explain, writes a plain-English reason for each proposed match, and then hands the whole list to you (the bookkeeper) in a small review dashboard. You click approve or reject on each one. Nothing is ever applied to your books automatically.

Under the hood it is a Go program with no external services — no database server to run, no cloud account to sign up for. It stores its review history in a single SQLite file next to wherever you run it.

---

## Why this exists

Here is the specific thing that makes this kind of reconciliation hard, and why doing it by hand is tedious and error-prone even for a careful bookkeeper:

**A bank deposit is almost never the exact amount of one sale.** A card processor (Square, Stripe, your merchant account provider, etc.) doesn't send your bank a separate deposit for every individual sale. Instead it bundles up everything it processed over a day — sometimes a couple of days — subtracts its own processing fees, and sends your bank *one lump sum*. So the $77.27 that shows up in your bank feed on Tuesday might actually be three Monday sales of $25.00, $40.00, and $15.50, each with a small percentage-plus-flat-fee skimmed off before the money was ever deposited. There is no transaction ID, invoice number, or other shared key connecting the bank line to the POS rows — you have to work it out from the dates and the arithmetic.

Doing that by hand means, for every deposit, mentally asking "which group of sales from the last day or two, minus some estimated fee, adds up to this number?" That's slow, and worse, it's easy to get subtly wrong — a wrong grouping that happens to look close enough is a mistake that can sit in your books for months.

`reconbook` automates exactly that matching step — using a real, bounded, provably-correct search algorithm rather than a guess — while keeping a human bookkeeper in control of every single decision it proposes. The tool never decides anything on your behalf; it only narrows "stare at two spreadsheets and do mental arithmetic" down to "read one sentence and click approve or reject."

---

## How it works

A reconciliation run flows through four steps, in this order:

**1. Load the two CSV exports.** You give the tool a POS export (one row per sale or refund: an ID, a date, a dollar amount, a description) and a bank export (one row per deposit or withdrawal: an ID, a date, a dollar amount, a memo). Columns are matched by *name*, not by position, so it doesn't matter if your bank's CSV happens to list `memo` before `amount` — the tool looks up each column by its header. As soon as a dollar amount like `"49.99"` is read from the CSV, it's converted into an integer number of cents (`4999`) and never touched as a decimal dollar figure again — more on why in the next section.

**2. Look for simple one-to-one matches first.** For each bank deposit (processed oldest-to-newest), the tool first asks the easy question: is there a *single* unclaimed sale, within a few days of the deposit date, whose amount — after subtracting an estimated processing fee — lands close enough to the deposit? If yes, that's an exact match, and it's the simplest, most confident kind. This pass also catches refunds (a sale with a negative amount), since a refund is typically deposited or debited on its own rather than folded into a batch.

**3. For everything else, search for a combination that adds up.** If no single sale explains a deposit, the tool looks for a *group* of same-period sales whose fee-adjusted total lands within tolerance of the deposit amount — the batched-deposit scenario described above. This is the hard part, and it's solved with a real search algorithm (explained in the next section), not a shortcut. Whichever sales that search settles on get "claimed" immediately, so no later deposit can ever accidentally claim the same sale a second time.

**4. Every proposed match gets an explanation, a confidence level, and a human reviewer.** Whether a match came from step 2 or step 3, it's written out as a sentence a bookkeeper can read at a glance — e.g. *"Deposit of $77.27 matches a batch of 3 sales totaling $77.26 net of estimated fees (discrepancy $0.01)."* — plus a confidence level of `high`, `medium`, or `low`, based on how small the leftover discrepancy was relative to the tolerance budget and how many sales had to be grouped together. All of this is saved to a small local review database and served on a web dashboard. A bookkeeper opens the dashboard, reads each proposal, and clicks **Approve** or **Reject**. A rejection *requires* typing a reason — there is no way to dismiss a match silently — so that anyone looking at the record six months later can see not just that a deposit was never reconciled, but *why* a human decided not to accept the tool's suggestion. Anything the tool couldn't explain at all — a deposit with no fitting combination, or a sale that hasn't shown up in the bank yet — is listed separately as unmatched, for the bookkeeper to investigate by hand.

---

## The hard part, explained simply

The core problem the tool is solving — "which combination of these sales adds up to this deposit?" — is a well-known problem in computer science called **subset-sum**: given a list of numbers, does some subset of them add up to exactly (or close enough to) a target number? It's the same category of puzzle as "can I make exact change for a dollar using only the coins in this jar?" — except the "coins" here are sale amounts, and the "jar" is every unclaimed sale within a few days of the deposit.

Here's a real example straight out of this project's bundled sample data. On February 1st, three sales come through the POS: $25.00, $40.00, and $15.50. On February 2nd, the bank shows a single deposit: $77.27. Add up the three sales: $80.50. That's not $77.27 — the difference is the card processor's cut. Once you subtract an estimated fee from each sale individually (a percentage plus a small flat amount per transaction, the way real processors charge), the three sales net out to about $77.26 — one cent away from the actual deposit, well within a small allowed slack (the "tolerance"). So the correct explanation for that $77.27 deposit is "all three of Monday's sales, minus fees" — and a bookkeeper staring at a bank feed with no transaction IDs would have to discover that by trial and error.

A handful of sales is easy enough to check by hand. But a few dozen sales over a few days, where any combination of them might be the right one, quickly becomes too many possibilities to check by trying every combination one at a time (the number of possible groups doubles with every extra sale). This tool instead uses **dynamic programming** — a technique that builds up the answer to "can some subset of the first *N* sales add up to amount *S*?" incrementally, reusing work it's already done, rather than re-checking every combination from scratch. The search is also deliberately *bounded*: it only ever considers sums up to the deposit amount plus a small tolerance (anything past that can never be a valid answer anyway) and gives up gracefully on an unrealistically large deposit rather than building an unbounded table. Because the algorithm checks reachability exhaustively within that bounded range, if a valid combination exists within tolerance, the search is guaranteed to find it — and if it reports "no match," that's a provable fact, not a guess that happened not to find one.

This matters because a wrong match in bookkeeping software has real financial consequences — it's the difference between a tool that occasionally (and visibly) says "I couldn't figure this one out, you look at it," which is annoying but safe, and one that occasionally reports a *confident but wrong* explanation for where money went, which is the kind of mistake that can go unnoticed. That's why this project chose a careful, provably-exhaustive search over a faster "keep adding sales until the running total looks close enough" shortcut, which can walk right past the correct combination or lock onto a wrong one that merely looks plausible.

---

## Architecture at a glance

```
pos_export.csv ──▶ importer ──┐
                                ├──▶ matcher.Reconcile ──▶ review store ──▶ dashboard
bank_export.csv ──▶ importer ──┘   (exact match first,      (SQLite,       (approve/reject,
                                     then subset-sum          audit trail)   reason required
                                     batch match)                            for a rejection)
```

- **`internal/model`** — the two record types everything else works with: `POSTransaction` (a sale or refund) and `BankEntry` (a deposit or withdrawal). Both store their amount as `int64` cents, never a decimal.
- **`internal/importer`** — reads the POS and bank CSV files. This is the *one* place in the whole program where a human-typed decimal dollar string like `"49.99"` is converted into cents — by splitting the string on the decimal point, not by multiplying a floating-point number by 100 (see the next section for why that distinction matters). Columns are looked up by header name, so a provider that orders its export columns differently still parses correctly.
- **`internal/matcher`** — the matching engine: the exact one-to-one pass, the batch subset-sum pass (`subsetsum.go` holds the actual dynamic-programming search), the date-ordered claiming logic that prevents double-claiming, and the confidence-level rule.
- **`internal/review`** — persists every proposed match to a local SQLite file as a `pending` item, and enforces the approve/reject workflow: an explicit reviewer name is required, a rejection reason is required, and a decision, once made, can never be silently overwritten.
- **`internal/httpapi`** — the small web dashboard a bookkeeper actually opens in a browser: a summary count, the list of proposed matches with their explanations and confidence levels, the two "couldn't explain this" lists, and the approve/reject buttons.
- **`cmd/reconbook`** — the command-line entry point, with two subcommands: `reconcile` (import, match, save for review) and `review` (serve the dashboard).
- **`testdata/`** — a small bundled example POS/bank export pair used both by the automated tests and by `make demo`.
- **`docs/ARCHITECTURE.md`** — a deeper, more technical write-up of the same design decisions covered here, including the exact tradeoffs around the fee model and the search's scaling limit.

---

## How we know the matching is actually correct

Getting this kind of logic *right* matters more than getting it to merely run without crashing, so three specific risks each have a dedicated test built to catch them, beyond the normal "does this function return the right answer" tests:

- **The classic floating-point rounding trap, deliberately tested against known-tricky values.** Computers normally store decimal numbers like `49.99` in a binary format (`float64`) that can't represent every decimal fraction exactly. If you take `49.99` and multiply it by 100 hoping to get a whole number of cents, IEEE 754 floating-point arithmetic can hand you back `4998.999999999999` instead of a clean `4999` — and if your code then truncates that to an integer, you silently get `4998` cents instead of `4999`: a one-cent error that would make a perfectly correct transaction look like it doesn't match anything. This tool avoids that trap entirely by never multiplying a float at all — it parses the dollar string by splitting on the decimal point, treating the digits before and after it as plain integers. `TestParseDollarsToCentsNoFloatRoundingTrap` in `internal/importer` runs a batch of known float-tricky values (`49.99`, `19.99`, `0.29`, `2.33`, and others) through the parser specifically to make sure this invariant can never quietly regress.

- **An adversarial check that the chosen sales actually add up to what's claimed.** The subset-sum search doesn't just report "yes, I found a combination that sums to this amount" — it reconstructs and returns the *specific list of sales* it picked, by backtracking through its search table. That backtracking step is a well-known place to get the direction of the logic backwards and end up reporting a plausible-looking total that the "chosen" items don't actually sum to. `TestSubsetSumReconstructionPicksARealSubset` guards against exactly that: for dozens of different target amounts, it independently re-adds the reconstructed sales from scratch and checks that they genuinely equal the amount the search claims to have found — not trusting the search's own bookkeeping about what it found.

- **A direct test that two deposits can never both claim the same sale.** Because sales are removed from the pool the instant one deposit's match claims them, a later deposit searching an overlapping date window can never pick up a sale that already belongs to an earlier one. `TestTwoDepositsDoNotStealEachOthersTransactions` sets up two deposits with overlapping candidate windows and checks, directly, that every sale ends up claimed by exactly one of them — not by assuming the claiming logic works because of how it's written, but by actually running it and inspecting the result.

Between these and the rest of the suite (42 tests across every package, no external services required — run `make test` to see them all pass), every one of the risky spots in this design is checked directly rather than assumed.

---

## The one real bug we found

While building the review dashboard's listing code, there was a real bug worth calling out, because it's a common trap in Go specifically: the `reviewed_at` column in the review database starts out empty (`NULL`) for any match that hasn't been reviewed yet, and only gets filled in once a bookkeeper approves or rejects it. The original listing code read that column straight into a plain Go `string` variable — which works fine once the column has a value, but a plain `string` has no way to represent "there is no value at all." The moment the dashboard tried to list a batch of matches that included even one still-pending item, reading that `NULL` into a plain string failed and brought the listing down.

The fix is to use `database/sql`'s dedicated type for exactly this situation, `sql.NullString`, which can represent "this column is NULL" instead of forcing every value into a plain string: `internal/review/review.go`'s `scanItems` now scans `reviewed_at` into a `sql.NullString` and only converts it to a real timestamp when that value is actually present. Worth calling out alongside the fix: a test nearby had been silently discarding the exact error this bug produced (checking a method's return value with `_` instead of asserting on it), which meant the second symptom of the same underlying problem — a listing call quietly coming back empty instead of loudly failing with the real `NULL`-scan error — went unnoticed for longer than it should have. Both the scanning code and the test's error handling were fixed together, since a bug and the test that should have caught it sooner are really one story.

---

## Try it yourself

Requires Go (any recent version — see `go.mod` for the minimum). No database server, no network access, no API keys.

```bash
make build        # builds ./bin/reconbook
make test         # runs the full test suite (42 tests, no external services)
make demo         # reconciles the bundled example exports and opens the review dashboard
```

`make demo` runs the bundled `testdata/pos_export.csv` and `testdata/bank_export.csv` through the whole pipeline and opens the dashboard at **http://localhost:8090/**. That sample data was built to deliberately include every interesting case in one small file: an exact one-to-one match, a three-sale batch matching a single deposit (the $77.27 example above), a refund, an unmatched "mystery" deposit that nothing explains, and an unmatched sale that hasn't been deposited yet — and `test/integration/reconcile_test.go` checks that the tool gets every single one of these right against a hand-calculated expected answer, not just that the program runs without error.

To reconcile your own exports instead of the bundled sample:

```bash
./bin/reconbook reconcile -pos your_pos_export.csv -bank your_bank_export.csv
./bin/reconbook review   # opens http://localhost:8090/
```

Your CSVs are matched by column name, so column order doesn't matter, as long as the right names are present:

```
# POS export
transaction_id,date,amount,description

# bank export
entry_id,date,amount,memo
```

Useful flags on `reconcile` (see `./bin/reconbook reconcile -h` for all of them): `-date-window-days` (how many days before a deposit a sale may fall and still be considered, default 3), `-fee-rate` (estimated card-processing percentage fee, default 2.9%), `-fixed-fee-cents` (estimated flat per-transaction fee, default 30¢), `-tolerance-cents` (slack allowed beyond the fee estimate, default 50¢).

---

## What this deliberately doesn't do (yet)

- **It doesn't search for the single best way to split sales across *all* deposits at once.** It processes deposits oldest-to-newest and permanently claims whatever sales the best match for each one uses, before moving to the next deposit. For the realistic case — deposits showing up in roughly the order their underlying sales happened — that's both the natural and the correct approach, but in principle a contrived input could exist where a different, global assignment across every deposit at once would fit better. This is a documented, deliberate tradeoff, not an oversight (see `docs/ARCHITECTURE.md`).
- **It doesn't learn or fetch your processor's real fee schedule.** The percentage-plus-flat-fee estimate is a flat number you supply; a real processor's fees can vary by card type or volume tier. The tolerance band is what absorbs that imprecision, rather than the tool trying to model a fee schedule precisely.
- **It doesn't support multiple currencies.** Every amount is cents in a single, implicit currency throughout.

---

## Project layout

```
internal/model/        POSTransaction, BankEntry — money always in int64 cents
internal/importer/     CSV parsing (column-name-robust, no float rounding traps)
internal/matcher/       exact + subset-sum batch matching, confidence scoring
internal/review/        SQLite-backed approve/reject workflow + audit trail
internal/httpapi/        bookkeeper-facing review dashboard
cmd/reconbook/            CLI: reconcile, review
testdata/                 example POS/bank exports exercising every match kind
test/integration/         full pipeline against the example exports, hand-verified
docs/ARCHITECTURE.md      deeper design rationale and documented tradeoffs
```

Source: [github.com/Kuwar-Vishnudatt/reconbook](https://github.com/Kuwar-Vishnudatt/reconbook) · Licensed under [MIT](LICENSE).
