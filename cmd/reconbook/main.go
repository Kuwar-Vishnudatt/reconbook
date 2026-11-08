// Command reconbook is the CLI: `reconcile` imports a POS export and a
// bank export, runs matching, and saves the proposals for review;
// `review` serves the bookkeeper's review dashboard.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Kuwar-Vishnudatt/reconbook/internal/httpapi"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/importer"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/matcher"
	"github.com/Kuwar-Vishnudatt/reconbook/internal/review"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "reconcile":
		runReconcile(os.Args[2:])
	case "review":
		runReview(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: reconbook <reconcile|review> [flags]")
}

func runReconcile(args []string) {
	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	posPath := fs.String("pos", "", "path to the POS export CSV (required)")
	bankPath := fs.String("bank", "", "path to the bank export CSV (required)")
	reviewDB := fs.String("review-db", "reconbook-review.db", "path to the review database")
	dateWindow := fs.Int("date-window-days", 3, "how many days before a deposit a sale may fall and still be considered")
	feeRate := fs.Float64("fee-rate", 0.029, "estimated card-processing percentage fee")
	fixedFee := fs.Int64("fixed-fee-cents", 30, "estimated fixed per-transaction processing fee, in cents")
	tolerance := fs.Int64("tolerance-cents", 50, "slack beyond the fee model, in cents")
	fs.Parse(args)

	if *posPath == "" || *bankPath == "" {
		fmt.Fprintln(os.Stderr, "reconcile: -pos and -bank are required")
		os.Exit(2)
	}

	posFile, err := os.Open(*posPath)
	must(err)
	defer posFile.Close()
	pos, err := importer.ParsePOS(posFile)
	must(err)

	bankFile, err := os.Open(*bankPath)
	must(err)
	defer bankFile.Close()
	bank, err := importer.ParseBank(bankFile)
	must(err)

	cfg := matcher.Config{DateWindowDays: *dateWindow, FeeRate: *feeRate, FixedFeeCents: *fixedFee, ToleranceCents: *tolerance}
	result := matcher.Reconcile(pos, bank, cfg)

	r, err := review.Open(*reviewDB)
	must(err)
	defer r.Close()
	must(r.SaveResult(result, time.Now()))

	fmt.Printf("reconciled %d POS transactions against %d bank entries:\n", len(pos), len(bank))
	fmt.Printf("  %d proposed matches (%d exact, %d batch)\n", len(result.Matches), countKind(result.Matches, matcher.MatchExact), countKind(result.Matches, matcher.MatchBatch))
	fmt.Printf("  %d unmatched bank entries, %d unmatched POS sales\n", len(result.UnmatchedBank), len(result.UnmatchedPOS))
	fmt.Printf("saved to %s for review — run `reconbook review -review-db %s` to open the dashboard\n", *reviewDB, *reviewDB)
}

func countKind(matches []matcher.Match, kind matcher.MatchKind) int {
	n := 0
	for _, m := range matches {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

func runReview(args []string) {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	reviewDB := fs.String("review-db", "reconbook-review.db", "path to the review database")
	addr := fs.String("addr", ":8090", "HTTP listen address")
	fs.Parse(args)

	r, err := review.Open(*reviewDB)
	must(err)
	defer r.Close()

	srv := httpapi.New(r)
	log.Printf("reconbook review dashboard on http://localhost%s/", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
