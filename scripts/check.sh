#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
go test -race ./...
go vet ./...
go build -o bin/forgeschema ./cmd/forgeschema
./bin/forgeschema validate examples/customers.yaml
./bin/forgeschema validate examples/empty.yaml
./bin/forgeschema plan examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml > bin/example-plan.json
./bin/forgeschema plan --explain examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml
