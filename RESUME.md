# ForgeSchema resume checkpoint

Updated: 2026-10-15, end of day 1 session 2.

Status: PAUSED. Sessions 1 and 2 are complete. Await an explicit go-ahead for session 3 only. Do not begin migration planning or execution during this checkpoint.

## Published checkpoints
Engine session 2 implementation: 31713887e9c39a8bd8ceeaab5b3cdebad89aea0e on main in ForgeSchema/forgeschema.
Initial live-tested inspection commit: fcd6eebfb5b188076f4bdcf40014318f7455e23b.
Website session 2 content: 0d5da4ec46981eafdab6fd08079b3f672bab8826 on main in photozeitgeist/forgeschema.com.
This resume file is updated in a separate documentation commit after the implementation.

## What exists
The Go CLI version is 0.0.2-dev, minimum Go 1.24. Commands: help, version, validate and inspect. The strict version 1 desired-schema validator, canonical JSON and SHA-256 fingerprints from session 1 are retained.
New postgres.Inspect(ctx, db, schemaName) reads one existing PostgreSQL 16/17 schema in a READ ONLY, REPEATABLE READ transaction, with catalog-only search_path. It reads no application rows and creates no objects. All catalog queries use parameters for object IDs/names. Missing schemas and unsupported server versions fail.
The inspector captures supported tables/columns, primary keys and ordinary indexes. Primary-key backing indexes are not duplicated. Ordered key/index columns are preserved; tables/columns/indexes normalize by name. Unsupported objects and unrepresentable properties fail with an UnsupportedError, and no partial document is returned. Read SCHEMA.md and INSPECTION.md for the precise scope and ignored metadata.
CLI inspection uses DATABASE_URL in the environment, a 30-second deadline, canonical JSON stdout and sanitized database errors on stderr. Usage errors exit 2, inspection failures exit 1. The Go API returns underlying database errors for diagnosis; application callers should avoid exposing credentials.
No comparison engine, migration SQL generation, apply command or standalone verify command exists yet. No public binary release or license grant.

## Verified results
Live integration CI against PostgreSQL 16.10 AND 17.6 passed:
https://github.com/ForgeSchema/forgeschema/actions/runs/38694420957
Each job executed the populated customer SQL fixture in a uniquely named schema, compared three inspections with the YAML contract, exercised successful CLI inspection, checked all supported types and ordered composite keys/indexes, rejected unsupported structures, tested empty/missing schemas, out-of-scope objects and dropped column slots, and inspected under a role without application-table privileges. Cancellation was also tested.
Selected verbatim job logs are committed in test/results/session-2-postgres-16.10.txt and session-2-postgres-17.6.txt. They identify the tested commit and run. They are selected test lines, not full container logs.
Final local scripts/check.sh passed (race tests, vet, build, both schema examples). Additional CLI checks cover missing DATABASE_URL, invalid scope and credential-safe malformed connection errors. Local database integration cases explicitly SKIPPED because no PostgreSQL server is running here; actual database evidence is the hosted CI. Local transcript: test/results/session-2-local.txt.
Final implementation CI: both PostgreSQL jobs passed for 31713887e9c39a8bd8ceeaab5b3cdebad89aea0e: https://github.com/ForgeSchema/forgeschema/actions/runs/38694756777
Hugo 0.158.0 built the updated website (25 generated pages). Generated internal links/assets resolved. All 18 theme blobs remain byte-for-byte unchanged. Website content, status, roadmap, build/schema/workflow pages and development note now reflect inspection. No migration features are advertised as available. Domain connection remains the owner's task; deployment is not verified.

## Environment and publishing
Native PostgreSQL cannot run normally in this container: its UID/GID maps contain only root, so switching to an unprivileged database user fails. Docker is absent. Do not hack PostgreSQL's root protections. The integration workflow now provides disposable official PostgreSQL 16.10/17.6 service containers, with local-test-only credentials and an explicit test database environment variable.
The test suite creates/drops schemas and roles and requires a disposable database and a privileged test user. This is separate from production inspection, which uses catalog visibility and no application-table SELECT permission.
Go and Hugo are currently in /tmp/go/bin and /tmp/forgeschema-hugo. These are transient; do not assume future sessions retain them.
Shell clone/fetch works for the public engine, but shell pushes/private clones lack credentials. Publish with GitHub connector create_tree/create_commit/update_ref and expected head SHA. Read fresh branch heads and relevant files before editing. Preserve themes, layouts, CSS, site structure and domain configuration ownership. Code remains all rights reserved.

## Next session, only after go-ahead
Read PLAN.md, SCHEMA.md and INSPECTION.md. Implement deterministic comparison and a versioned, reviewable migration plan artifact, NOT execution. Compare two normalized documents. Explicitly validate rename metadata instead of guessing renames. Initial operations: create tables (including basic primary keys), add nullable columns, explicit column renames and ordinary index additions. Block destructive changes, type conversions, required-column additions and unsupported constraint changes.
Include canonical source/target fingerprints, explained ordered operations, quoted SQL and blocked reasons. Check relation-name collisions and dependencies, including indexes affected by renames. Decide and document the plan and rename input formats before coding. Standalone Inspect does not lock against later drift; execution/precondition coordination is session 4.
At the end of session 3, run appropriate tests, publish code, update this resume file and website with verified behavior, report commit IDs and pause for the next go-ahead.
