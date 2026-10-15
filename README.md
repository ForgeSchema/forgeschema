# ForgeSchema

ForgeSchema is a PostgreSQL schema evolution engine written in Go. The workflow is inspect, plan, apply and verify, with an explained migration artifact between comparison and execution.

The six-session prototype is complete. Inspect, plan, apply and independently verify supported PostgreSQL schemas. The reproducible walkthrough runs the compiled CLI against two populated synthetic customers, applies six operations and demonstrates stale-plan rejection and read-only drift detection.

Explore the [interactive browser demo](https://forgeschema.com/demo/) to compare recorded schemas, review SQL, inspect preserved rows and switch between match/drift reports. It replays actual CLI evidence; it does not execute database changes. See [BROWSER_DEMO.md](BROWSER_DEMO.md) for its source and publishing instructions.

Start with [WALKTHROUGH.md](WALKTHROUGH.md). See [COMMANDS.md](COMMANDS.md) for commands and exits, and [FEATURES.md](FEATURES.md) for the supported/unsupported matrix. This remains an unreleased prototype, not a production-readiness claim.

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

Validation prints canonical JSON on success and errors to stderr on failure. Validation uses exit 0 for success, 1 for invalid input/files, and 2 for usage errors. Blocked plan targets use exit 3; standalone verification uses exit 4 for structural drift. See SCHEMA.md for the exact supported contract. The version is 0.0.6-dev; no release binaries are published.

## Review a migration plan

```sh
./bin/forgeschema plan examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml
./bin/forgeschema plan --explain examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml
```

The default output is a versioned JSON artifact; --explain prints a human review. Inspection JSON can also be used as the source schema. A blocked plan exits 3, includes reasons and contains no operations. The plan commands never execute SQL. To execute a saved reviewed artifact, use `forgeschema apply PLAN.json` with DATABASE_URL set and external DDL paused. Read APPLY.md for transaction, locking, role and commit-outcome limits. See PLANNING.md for the artifact format, supported operations and limits.

## Verify independently

```sh
./bin/forgeschema verify desired.yaml
./bin/forgeschema verify --plan reviewed-plan.json
```

With DATABASE_URL set, verify prints a match/drift report without changing the database. A schema file describes the complete managed schema; the plan form checks a strictly validated ready plan target. See VERIFY.md for difference kinds, exit codes and snapshot limitations.

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

Read PLAN.md for the six-session schedule, RESUME.md for current results and continuation instructions, and AGENTS.md for session rules. Every session ends with code and website updates, followed by a pause for the owner's go-ahead. Website content is maintained in photozeitgeist/forgeschema.com. The owner connected https://forgeschema.com/. All six sessions are complete; further work awaits owner direction.

Copyright 2026 ForgeSchema. Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for attribution and third-party licenses.
