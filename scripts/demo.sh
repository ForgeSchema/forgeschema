#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ -z "${FORGESCHEMA_DEMO_DATABASE_URL:-}" ]; then
  echo 'Set FORGESCHEMA_DEMO_DATABASE_URL to a disposable PostgreSQL 16/17 database; no default database is selected.' >&2
  exit 2
fi
go build -o bin/forgeschema ./cmd/forgeschema
go run ./cmd/forgeschema-demo --binary ./bin/forgeschema "$@"
