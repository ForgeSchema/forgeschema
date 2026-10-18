#!/bin/sh
# Runs every test against a disposable PostgreSQL, the race detector and the
# walkthrough, and writes the output to test/results/ with a note of the
# machine. Numbers on forgeschema.com come from these files.
#
#   FORGESCHEMA_TEST_DATABASE_URL=postgres://... sh scripts/record-tests.sh
#
# The database must be disposable: the tests create and drop schemas and roles.
set -eu
cd "$(dirname "$0")/.."
: "${FORGESCHEMA_TEST_DATABASE_URL:?set FORGESCHEMA_TEST_DATABASE_URL to a disposable PostgreSQL 16 or 17}"
out=test/results
mkdir -p "$out"
server=$(psql -X -At "$FORGESCHEMA_TEST_DATABASE_URL" -c 'SHOW server_version' 2>/dev/null || echo unknown)

header() {
	echo "# $1"
	echo "# recorded $(date -u '+%Y-%m-%d %H:%M UTC') with $(go version | cut -d' ' -f3-), PostgreSQL $server"
	echo "# $(uname -sr), $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ //'), $(nproc 2>/dev/null || echo '?') CPUs"
	echo "# command: $2"
	echo
}

go vet ./...

cmd="go test -count=1 -v ./..."
{ header "Every test, verbose, including the database tests" "$cmd"; $cmd 2>&1; } > "$out/tests.txt"

cmd="go test -race -count=1 ./..."
{ header "Every test with the race detector" "$cmd"; $cmd 2>&1; } > "$out/race.txt"

cmd="python3 scripts/release-plan-test.py"
{ header "The release plan's cases" "$cmd"; $cmd 2>&1; } > "$out/release-plan.txt"

rm -rf "$out/walkthrough"
cmd="sh scripts/demo.sh --output $out/walkthrough"
{ header "The customer walkthrough, run with the real CLI" "$cmd"; FORGESCHEMA_DEMO_DATABASE_URL=$FORGESCHEMA_TEST_DATABASE_URL $cmd 2>&1; } > "$out/walkthrough.txt"

grep -hE '^(ok|FAIL|---)' "$out/tests.txt" "$out/race.txt" | grep -v -- '--- PASS' || true
tail -1 "$out/release-plan.txt"
grep -h 'PASS:' "$out/walkthrough.txt" || true
echo "results written to $out/"
