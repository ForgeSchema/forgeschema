# ForgeSchema v0.1.0-alpha.1

First public alpha of the PostgreSQL schema evolution CLI, licensed under Apache 2.0. This is an early prototype release, not a production-readiness claim.

Inspect supported PostgreSQL 16/17 schemas, review deterministic format 1 migration artifacts, apply supported changes transactionally and verify supported structural drift independently. Supported evolution includes new tables/primary keys, explicit column renames, nullable additions and ordinary indexes. The browser demo replays actual PostgreSQL 16.10/17.6 results.

Six archives provide Linux, macOS and Windows binaries for amd64 (x86-64) and arm64. Each archive includes the Apache license, attribution, dependency notices and key guides. SHA256SUMS covers every archive. Binaries are built without CGO; macOS and Windows binaries are not code-signed or notarized. Linux amd64 is smoke-tested; other targets are cross-compiled and have not been run on their native platforms.

The release workflow gates packaging on race tests, vet, the full PostgreSQL 16.10/17.6 suites, disposable CLI walkthroughs and browser replay checks. It smoke-tests the packaged Linux CLI's version, validation and byte-identical example plan before publishing.

Pause external DDL before apply. Apply uses exclusive source-table locks and can block application traffic. The narrow supported contract rejects unsupported constraints/features and blocks destructive conversions. A commit connection error can leave outcome uncertain. Read APPLY.md, FEATURES.md and VERIFY.md before using a real database. See WALKTHROUGH.md for reproduction against an explicitly disposable database.
