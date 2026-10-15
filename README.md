# ForgeSchema

ForgeSchema is a PostgreSQL schema evolution engine being built in Go. The intended workflow is inspect, plan, apply and verify, with an explained migration artifact between comparison and execution.

Session 1 implements strict desired-schema validation and deterministic normalization only. There is no database inspection or migration execution yet, and this is not a production release.

## Try the foundation

Install Go 1.24 or later, then run:

```sh
go test ./...
go build -o bin/forgeschema ./cmd/forgeschema
./bin/forgeschema validate examples/customers.yaml
./bin/forgeschema version
```

Validation prints canonical JSON on success and errors to stderr on failure. Exit codes are 0 for success, 1 for invalid input or file errors, and 2 for command usage errors. See SCHEMA.md for the exact supported contract. The version is 0.0.1-dev; no release binaries are published.

## PostgreSQL fixture for subsequent sessions

With Docker and Compose available:

```sh
docker compose up -d --wait
docker compose exec -T postgres psql -U forgeschema -d forgeschema_test -c 'TABLE public.customers'
docker compose down
```

The fixture is initialized on container creation with a populated customers table matching examples/customers.yaml. The database is disposable; no persistent volume is configured. Port 55432 binds to localhost only, and the credentials are for local testing. Session 1 does not connect to it. DATABASE_URL for subsequent integration work: postgres://forgeschema:local_test_only@localhost:55432/forgeschema_test?sslmode=disable.

## Development checkpoints

Read PLAN.md for the six-session schedule, RESUME.md for current results and continuation instructions, and AGENTS.md for session rules. Every session ends with code and website updates, followed by a pause for the owner's go-ahead. Website content is maintained in photozeitgeist/forgeschema.com. Domain connection is left to the owner.

Copyright 2026 ForgeSchema. All rights reserved. No open-source license has been granted. Third-party dependency notices are in THIRD_PARTY_NOTICES.md.
