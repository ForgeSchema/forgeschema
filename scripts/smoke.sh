#!/bin/sh
# Smoke test for a built forgeschema binary, run on the machine it was built
# for: sh scripts/smoke.sh PATH/TO/forgeschema VERSION
# Needs no database. Checks the version, strict validation, a byte-identical
# example plan and a full plan of the shop example from an empty schema.
set -eu
bin=$1
want=$2
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
got=$("$bin" version | tr -d '\r')
[ "$got" = "$want" ] || { echo "version: got $got, want $want" >&2; exit 1; }
"$bin" validate examples/customers.yaml > "$tmp/customers.json"
"$bin" validate examples/shop.yaml > "$tmp/shop.json"
"$bin" plan examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml | tr -d '\r' > "$tmp/plan.json"
cmp "$tmp/plan.json" examples/customer-plan.json
"$bin" plan --explain examples/empty.yaml examples/shop.yaml > "$tmp/shop-plan.txt"
grep -q 'ADD CONSTRAINT "orders_customer_fkey" FOREIGN KEY' "$tmp/shop-plan.txt"
if "$bin" validate examples/customer-renames.yaml > /dev/null 2>&1; then
  echo "accepted a rename file as a schema" >&2; exit 1
fi
echo "smoke test passed for forgeschema $got"
