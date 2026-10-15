#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
go test -race ./...
go vet ./...
go build -o bin/forgeschema ./cmd/forgeschema
./bin/forgeschema validate examples/customers.yaml
./bin/forgeschema validate examples/empty.yaml
