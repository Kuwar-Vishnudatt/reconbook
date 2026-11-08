#!/usr/bin/env bash
# Reconciles the bundled example POS/bank exports and opens the review
# dashboard so you can see a batch match, two exact matches, an unmatched
# mystery deposit, and an unmatched not-yet-deposited sale, then approve
# or reject one yourself.
set -euo pipefail
cd "$(dirname "$0")/.."

DB=$(mktemp -t reconbook-demo.XXXXXX.db)
rm -f "$DB"
trap 'rm -f "$DB"' EXIT

echo "--- reconciling the bundled example exports ---"
./bin/reconbook reconcile -pos testdata/pos_export.csv -bank testdata/bank_export.csv -review-db "$DB"

echo
echo "--- opening the review dashboard at http://localhost:8090/ (Ctrl+C to stop) ---"
./bin/reconbook review -addr :8090 -review-db "$DB"
