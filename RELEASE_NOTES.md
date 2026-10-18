# ForgeSchema v0.2.0-alpha.1

Second alpha of the PostgreSQL schema evolution CLI, licensed under Apache 2.0. Still an early release, not a production-readiness claim.

## What's new

ForgeSchema now reads and writes far more of what real schemas use. Inspect, plan, apply and verify all understand:

- varchar(n), char(n) and numeric(p,s) types
- column defaults: literals plus now(), current_timestamp, localtimestamp, current_date and gen_random_uuid()
- identity columns (GENERATED ALWAYS / BY DEFAULT AS IDENTITY)
- unique constraints and unique indexes
- foreign keys between tables in the same schema, with ON DELETE / ON UPDATE actions

Plans can now add a required column when it has a default, widen varchar and numeric columns, change nullability and defaults, and add unique constraints, indexes and foreign keys to existing tables. Drops and other type conversions are still blocked. examples/shop.yaml shows the whole contract.

## Apply

- Locks only the tables a plan changes (plus the foreign key partners of a column whose type changes), so unrelated tables keep serving traffic. `plan --explain` lists the locks.
- `--lock-timeout`, `--statement-timeout` and `--timeout` flags for slow steps such as index builds on large tables.
- Failures show PostgreSQL's message and SQLSTATE, without row values or connection details.
- New exit codes: 5 when the database no longer matches the plan, 6 when the commit outcome is unknown.

## Fixes

- A text default containing a backslash could inject SQL on a server running with standard_conforming_strings off. Backslashes are now rejected and apply/inspect force the setting on.
- Inspection no longer waits behind other sessions' locks on tables it only reads.

## Compatibility

Plan format is still 1. Plans saved with 0.1.0-alpha.1 load, validate and apply unchanged. Plans 0.1 blocked for changes that are now supported must be rebuilt.

## Downloads

Six archives for Linux, macOS and Windows on amd64 and arm64, each with the Apache license, attribution, dependency notices and key guides, plus SHA256SUMS. Binaries are built without CGO; macOS and Windows binaries are not code-signed or notarized. Linux amd64 is smoke-tested; other targets are cross-compiled.

The release workflow gates packaging on race tests, vet, the PostgreSQL 16.10 and 17.6 suites, disposable CLI walkthroughs and browser replay checks.

Pause external DDL before apply. Read APPLY.md, FEATURES.md and VERIFY.md before using a real database.
