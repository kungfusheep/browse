#!/bin/bash
# Regenerate the known-good sites map from crawler database.
# Run from project root: ./scripts/generate-sites.sh

set -e

DB="${1:-crawler.db}"
MIN_SCORE="${2:-80}"
OUTPUT="sites/known.go"

if [ ! -f "$DB" ]; then
    echo "Database not found: $DB"
    echo "Usage: $0 [database.db] [min-score]"
    exit 1
fi

echo "Generating sites/known.go from $DB (min score: $MIN_SCORE)..."
./crawl -db "$DB" -export-go "$OUTPUT" -min-score "$MIN_SCORE"

echo "Verifying build..."
go build ./sites

echo "Done! Run 'go build' to include in main binary."
