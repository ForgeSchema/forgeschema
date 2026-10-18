# ForgeSchema

ForgeSchema is a PostgreSQL schema evolution engine written in Go. The workflow is inspect, plan, apply and verify, with an explained migration artifact between comparison and execution.

You describe the schema you want in YAML. ForgeSchema reads the live schema, works out the SQL to get there, and writes it down as a plan you can review. Apply runs that exact plan in one transaction, after checking the database still matches what you reviewed. Verify tells you, read-only, whether a database matches a schema file.

It understands ordinary tables, columns with literal or common function defaults, identity columns, primary keys, unique constraints, unique and plain btree indexes, and foreign keys within one schema. Types cover integers, text, varchar(n), char(n), numeric(p,s), boolean, uuid, dates and timestamps, jsonb, bytea, real and double precision. Anything it can't represent exactly is reported as unsupported rather than silently left out.

Explore the [interactive browser demo](https://forgeschema.com/demo/) to compare recorded schemas, review SQL, inspect preserved rows and switch between match/drift reports. It replays actual CLI evidence; it does not execute database changes. See [BROWSER_DEMO.md](BROWSER_DEMO.md) for its source and publishing instructions.

Start with [WALKTHROUGH.md](WALKTHROUGH.md). See [COMMANDS.md](COMMANDS.md) for commands and exits, and [FEATURES.md](FEATURES.md) for the supported/unsupported matrix. This is alpha software, not a production-readiness claim.

## Try it

Install Go 1.24 or later, then run:

```sh
go test ./...
go build -o bin/forgeschema ./cmd/forgeschema
./bin/forgeschema validate examples/shop.yaml
./bin/forgeschema version
# Set DATABASE_URL securely in your environment, then inspect one existing schema:
./bin/forgeschema inspect public
```

[examples/shop.yaml](examples/shop.yaml) shows the full contract: identity keys, defaults, a unique constraint, a unique index and foreign keys with referential actions. [examples/customers.yaml](examples/customers.yaml) is the smaller schema used by the walkthrough.

Validation prints canonical JSON on success and errors to stderr on failure. Validation uses exit 0 for success, 1 for invalid input/files, and 2 for usage errors. Blocked plan targets use exit 3; standalone verification uses exit 4 for structural drift; apply uses 5 when the database no longer matches the plan and 6 when a commit outcome is unknown. See SCHEMA.md for the exact supported contract. The current version is 0.2.0-alpha.1. Download packaged binaries and SHA256SUMS from the [alpha release](https://github.com/ForgeSchema/forgeschema/releases/tag/v0.2.0-alpha.1). Linux, macOS and Windows archives cover amd64 and arm64; see [RELEASING.md](RELEASING.md) for validation and platform limits.

## Review a migration plan

```sh
./bin/forgeschema plan examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml
./bin/forgeschema plan --explain examples/customers.yaml examples/customers-next.yaml examples/customer-renames.yaml
```

The default output is a versioned JSON artifact; --explain prints a human review, including which tables apply will lock. Inspection JSON can also be used as the source schema. A blocked plan exits 3, includes reasons and contains no operations. The plan commands never execute SQL.

To execute a saved reviewed artifact, use `forgeschema apply PLAN.json` with DATABASE_URL set and external DDL paused. Apply locks only the tables the plan changes, plus the foreign key partners of a column whose type changes. Slow steps such as building an index on a large table can get more time with `--lock-timeout`, `--statement-timeout` and `--timeout`. When a step fails, the message includes PostgreSQL's own message and SQLSTATE, without row values. Read APPLY.md for transaction, locking, role and commit-outcome limits. See PLANNING.md for the artifact format, supported operations and limits.

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

## Not supported yet

- check constraints, views, sequences other than identity, partitions, triggers and functions
- changes that need more than one step: required columns without a default, type conversions other than widening, and drops
- timestamp precision, arrays and enums

[CLAUDE.md](CLAUDE.md) says where development stands; [docs/dev](docs/dev) keeps the history of the first sessions. Test output is recorded in [test/results](test/results). The website content is maintained in photozeitgeist/forgeschema.com.

Copyright 2026 ForgeSchema. Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for attribution and third-party licenses.
