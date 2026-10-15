# ForgeSchema resume checkpoint

Updated: 2026-10-15, end of day 2 session 3.

Status: PAUSED. Sessions 1–3 are complete. Await an explicit go-ahead for session 4 only. Do not begin execution work during this checkpoint.

## Published checkpoints
Engine session 3 implementation/evidence: 117c49d10f4119f5e0fbb9489d6fb4e8e6e15fce.
Initial live-tested planner commit: 32a79169e88ef5ed95090e2dff9dc10e64c3b5a9.
Website session 3 content: 9640ecb00a5f4b41fbb430b137aff9e7685430ac.
This resume update is a separate documentation commit following implementation.

## Current functionality
CLI 0.0.3-dev, Go 1.24 minimum. Commands: validate, inspect, plan, version and help. Inspect remains READ ONLY, REPEATABLE READ on PostgreSQL 16/17, reading metadata only.
New public plan package builds deterministic version 1 JSON artifacts containing canonical source/target schemas, fingerprints, sorted explicit rename mappings, status, explained operations/SQL and blocked reasons. Plan.Validate reconstructs the entire canonical artifact and rejects altered SQL, kind, explanation, status, format or fingerprints. This is consistency checking, not signing or live drift verification.
Supported operations: create tables with supported columns and basic primary keys, explicitly rename columns, add nullable columns without defaults, add ordinary indexes. Phases: create tables, rename columns, add columns, create indexes. Names are quoted and SQL types come from the normalized whitelist. Renamed column identities propagate into existing key/index references.
A blocked proposal has an EMPTY operation list: no partial SQL. Drops, implicit renames, conversions, nullability changes, required additions to existing tables, primary-key changes and index drops/rebuilds/renames are blocked. Invalid rename intent is an input error.
Rename mappings are in a separate strict YAML file: version: 1 and columns: [{table, from, to}]. Source/target names must exist as specified; occupied destinations, source-name reuse, duplicate mappings, chains and swaps are rejected. Schema files retain their original contract.
CLI: forgeschema plan [--explain] CURRENT.yaml DESIRED.yaml [RENAMES.yaml]. Inspection JSON is accepted as a source schema through the YAML subset. Default output is a JSON artifact; --explain is a readable review. Exit codes 0 ready/no-op, 1 invalid input, 2 usage, 3 blocked (with full artifact on stdout).
The existing strict YAML decoding logic was extracted into internal/strictyaml for shared schema/rename loading; all schema regression tests pass. Read PLANNING.md, SCHEMA.md and INSPECTION.md.
Examples: customers-next.yaml, customer-renames.yaml, actual generated customer-plan.json and customer-plan.txt. scripts/check.sh exercises the planner. No product executor, apply command, external plan loader or standalone verify yet. Code remains all rights reserved.

## Evidence
Live PostgreSQL 16.10 and 17.6 suites passed:
https://github.com/ForgeSchema/forgeschema/actions/runs/38695802665
The new test harness executed six generated operations combining a new table with a primary key/index, two column renames (including a primary-key column and an existing composite-index reference), a nullable addition and index creation. Reinspection exactly matched the target, and both original customer rows retained their IDs/names with NULL in the added column. Execution exists only in the test harness.
Unit tests cover no-op normalization, deterministic output, quoting, identity propagation, blocked mixed proposals with no SQL, invalid rename intent, JSON round trips, tampering and CLI success/blocked/error behavior. All earlier PostgreSQL inspector tests also passed.
Selected verbatim logs: test/results/session-3-postgres-16.10.txt and session-3-postgres-17.6.txt. Final local race tests, vet, build, CLI examples and a 16,104-execution schema-reader fuzz run passed. The committed example artifact matches regenerated output byte-for-byte. Local transcript: test/results/session-3-local.txt. Database cases skip locally because this container cannot run PostgreSQL normally; real database evidence is hosted CI.
Final implementation CI also passed on both PostgreSQL versions: https://github.com/ForgeSchema/forgeschema/actions/runs/38696157729 (commit 117c49d10f4119f5e0fbb9489d6fb4e8e6e15fce).
Website: Hugo 0.158.0 build passed (26 generated pages), internal links and assets resolved, all 18 theme blobs unchanged. Content/status/roadmap/build/schema/workflow pages and a session 3 development note reflect planning, not execution.

## Domain
The owner connected ForgeSchema.com during this session. HTTPS https://forgeschema.com/ returned 200 and served ForgeSchema content from this environment. After the website update, the live homepage also served the new 'Reviewable migration plans' badge, confirming the session 3 content is deployed here.
Do not change DNS/domain configuration. Propagation can still differ by resolver.

## Environment and publishing
Local tools currently /tmp/go/bin and /tmp/forgeschema-hugo; they are transient. Docker is absent, and the container UID/GID maps permit only root, so native PostgreSQL cannot start through its normal unprivileged workflow. Do not bypass its root protections. GitHub Actions supplies disposable official PostgreSQL 16.10/17.6 services.
Public shell clone/fetch works; shell push/private clone has no credentials. Use connector create_tree/create_commit/update_ref with expected head SHA. Read fresh heads before editing. Preserve website theme/templates/layout/CSS/structure and the owner's domain configuration.

## Next session, only after go-ahead
Implement execution of an EXACT reviewed plan, not replanning during apply. First add a strict bounded JSON artifact loader that rejects duplicate/unknown fields, null/coerced fields, extra documents and noncanonical/inconsistent artifacts, then invoke Plan.Validate. Ready status alone is insufficient; do not trust embedded SQL. Preserve format version 1 canonical generation unless deliberately versioning changes.
Require live source fingerprint equality, coordinate competing ForgeSchema runs, acquire suitable relation locks and reinspect with a fresh appropriate snapshot after obtaining locks. Consider DDL races carefully: repeatable-read snapshot acquisition before waiting for locks is not a drift check. Validate resulting schema before commit; record history atomically, configure timeouts and rollback on failure. Refuse unsupported concurrent/nontransactional operations.
History must not contaminate the managed schema: the inspector rejects reserved forgeschema_ relation names. Design isolated bookkeeping storage with explicit inspection boundaries, not a silent special-case omission.
Test stale/tampered/blocked plans, two competing runs, SQL failure rollback, target verification and history. The standalone inspector does not enumerate external dependencies; document the coordination limits with external writers and outside-schema objects honestly.
Finish by publishing code, updating this resume file and website with actual results, reporting commit IDs and pausing for session 5 authorization. No automatic release or license change.
