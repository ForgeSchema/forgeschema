# ForgeSchema

ForgeSchema is a PostgreSQL schema evolution engine being built in Go. The intended workflow is inspect, plan, apply and verify, with an explained migration artifact between comparison and execution.

Session 2 adds read-only PostgreSQL 16/17 catalog inspection to the strict desired-schema validator. Live integration tests passed on PostgreSQL 16.10 and 17.6. Migration planning and execution are not implemented, and this is not a production release.

## Try the foundation

Install Go 1.24 or later, then run:

```sh
go test ./...
go build -o bin/forgeschema ./cmd/forgeschema
./bin/forgeschema validate examples/customers.yaml
./bin/forgeschema version
# Set DATABASE_URL securely in your environment, then inspect one existing schema:
./bin/forgeschema inspect public
```

Validation prints canonical JSON on success and errors to stderr on failure. Exit codes are 0 for success, 1 for invalid input or file errors, and 2 for command usage errors. See SCHEMA.md for the exact supported contract. The version is 0.0.2-dev; no release binaries are published.

## PostgreSQL fixture

With Docker and Compose available:

```sh
docker compose up -d --wait
docker compose exec -T postgres psql -U forgeschema -d forgeschema_test -c 'TABLE public.customers'
docker compose down
```

The fixture is initialized on container creation with a populated customers table matching examples/customers.yaml. The database is disposable; no persistent volume is configured. Port 55432 binds to localhost only, and the credentials are for local testing. The inspector reads catalog metadata only, using a READ ONLY, REPEATABLE READ transaction and a 30-second CLI deadline. It reads no application rows and writes no database objects. Connection errors are sanitized; keep DATABASE_URL out of shell arguments and logs. DATABASE_URL for subsequent integration work: postgres://forgeschema:local_test_only@localhost:55432/forgeschema_test?sslmode=disable.

## Live integration tests

Use a disposable database: the tests create and drop uniquely named schemas and roles. With the Compose service running, set FORGESCHEMA_TEST_DATABASE_URL to its local connection URL and run:

```sh
go test -race -count=1 -v ./postgres
```

Without that variable, database tests explicitly skip. The GitHub Actions workflow runs them against PostgreSQL 16.10 and 17.6 services. See INSPECTION.md for managed scope, supported objects, ignored metadata and rejection behavior.

## Development checkpoints

Read PLAN.md for the six-session schedule, RESUME.md for current results and continuation instructions, and AGENTS.md for session rules. Every session ends with code and website updates, followed by a pause for the owner's go-ahead. Website content is maintained in photozeitgeist/forgeschema.com. Domain connection is left to the owner.

Copyright 2026 ForgeSchema. All rights reserved. No open-source license has been granted. Third-party dependency notices are in THIRD_PARTY_NOTICES.md.
