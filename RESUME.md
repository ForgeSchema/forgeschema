# ForgeSchema resume checkpoint

Updated: 2026-10-15, end of day 3 session 5.

Status: PAUSED. Sessions 1–5 are complete. Await explicit go-ahead for session 6 only. Do not begin the walkthrough/handoff during this checkpoint.

## Published checkpoints
Engine implementation/evidence: fed9c3bb2e1055a5b30a418ebf6eeddc2fa5dd82.
Functional verification/failure suite commit: 45f5402bb45c1a267e60ecc8f9d46d28aab0ed21.
Website session 5 content: b38d713bf78e1652ff33ff41643e830e03433a9a.
This resume update is a separate checkpoint commit.

## Current functionality
CLI 0.0.5-dev, Go 1.24 minimum. Commands: validate, inspect, plan, apply, verify, version and help. Plan artifact format remains version 1; canonical generation and sample JSON are unchanged.
New schema.Compare(expected, actual) returns deterministic descriptive differences without generating SQL, inferring renames or applying migration policy. It normalizes copies of inputs. Different schema names or invalid documents fail.
New postgres.Verify(ctx, db, expected) uses the inspector's READ ONLY, REPEATABLE READ snapshot. It reads no application rows/history and performs no DDL, repairs or history writes. Callers supply a deadline; the CLI uses 30 seconds.
VerifyResult includes status match/drift, schema, expected/actual fingerprints and a differences array (always present). Difference fields are kind, table, optional column/index, expected and actual strings. Kinds cover missing/unexpected tables/columns/indexes, types, nullability and ordered primary-key/index columns. Whole missing/extra tables produce one table-level difference. Reports sort by table/column/index/kind. Ordered lists are JSON-array strings.
CLI verify EXPECTED.yaml accepts schema YAML or inspection JSON; verify --plan PLAN.json strictly loads a ready plan and checks its target. No input-format guessing. Exit 0 match, 4 supported structural drift (report on stdout), 1 input/inspection/unsupported-object failure (no partial report), 2 usage/missing DATABASE_URL, 3 blocked plan target. Database errors are sanitized.
Read VERIFY.md, APPLY.md, PLANNING.md, SCHEMA.md and INSPECTION.md. Existing apply semantics remain: private validated artifact, transaction advisory lock, ACCESS EXCLUSIVE on every source table, fresh source check after locks under READ COMMITTED, supported DDL, target check, exact isolated history and commit. Defaults remain 5s lock/30s statement/60s overall Apply deadline. Commit uncertainty remains explicit.

## Verified results
PostgreSQL 16.10 and 17.6 passed the full functional suite:
https://github.com/ForgeSchema/forgeschema/actions/runs/38699937303
New tests cover read-only verification under a role without application-table privileges and default read-only transactions, unsupported structures/cancellation, blocked plan rejection and every supported descriptive difference kind.
The full CLI test performs inspect → plan → apply → verify with six operations: new table with primary key/index, two column renames (including key/index references), a nullable addition and another index. Both original rows retain IDs, names and nullable values. Verification matches afterwards; later extra DDL yields a column_unexpected drift report without repairs or history changes.
New apply failure cases passed: context cancellation while waiting for a relation lock (including released locks and a later successful apply), statement timeout during real DDL, metadata bootstrap rollback preserving pre-existing metadata and history INSERT permission failure after successful application DDL/target verification. That last failure rolls back the application DDL and records no history.
Metadata collision/privilege tests temporarily isolate global test history and create a test role; the suite must run only in a privileged dedicated disposable database, never against production. Tests intentionally do not run in parallel; the existing competing-client test controls its own goroutines.
Selected verbatim logs: test/results/session-5-postgres-16.10.txt and session-5-postgres-17.6.txt. All earlier schema, planner, loader, inspector, concurrency and rollback tests also passed.
Final implementation/evidence commit also passed both PostgreSQL jobs: https://github.com/ForgeSchema/forgeschema/actions/runs/38700425576 (commit fed9c3bb2e1055a5b30a418ebf6eeddc2fa5dd82).
Local scripts/check.sh passed (race tests, vet, build and offline examples). The committed format 1 example still matches regenerated JSON byte-for-byte. Transcript: test/results/session-5-local.txt. Database cases skip locally; actual database proof is hosted CI. No new parser changes required another fuzz run.
Hugo 0.158.0 build passed (28 generated pages); all generated internal links/assets resolved. All 18 theme blobs are unchanged. Website content/status/roadmap/build/workflow/schema and session 5 note now reflect the complete command workflow and its limits.

## Website/contact
The owner connected ForgeSchema.com. The live HTTPS homepage serves Independent schema verification. Contact remains info@forgeschema.com; Cloudflare protects the delivered mail link, and decoding its data-cfemail confirms the correct address. Preserve contact, theme/templates/layout/CSS/structure and DNS/domain configuration.

## Limits that remain
This is an unreleased prototype, all rights reserved. No binary release, tag, automatic release workflow or license change is authorized by completion of these sessions.
External DDL must be paused for Apply. The advisory lock coordinates ForgeSchema only, and relation locks do not eliminate every external namespace CREATE, outside-schema dependency or database-wide hook. ACCESS EXCLUSIVE can block application reads/writes. The metadata schema/table require the consistent applying role's exclusive ownership/ACLs and initial database CREATE permission.
A verification match is a supported structural snapshot, not proof about application data, grants/comments, excluded metadata, migration history or continued future state. Verify --plan checks the target even if another tool achieved it. It does not automatically resolve an unconfirmed commit.
Unsupported/destructive changes, type/nullability conversions, required additions to existing tables, index rebuilds, general data backfills, other databases and online UI remain out of scope. General crash recovery/commit-network-failure fault injection is not established.

## Environment/publishing
Transient tools currently /tmp/go/bin and /tmp/forgeschema-hugo. Docker absent, root-only UID/GID map prevents normal native PostgreSQL. Do not bypass root protections. CI provides disposable official PostgreSQL 16.10/17.6 service containers with test-only credentials.
Public shell clone/fetch works, private clone/push lacks credentials. Publish using GitHub connector create_tree/create_commit/update_ref with expected head SHA and fresh branch reads. Preserve the private website repository photozeitgeist/forgeschema.com; do not replace its theme.

## Next session, only after go-ahead
Session 6 is the final prototype walkthrough and handoff. Read PLAN.md and the guides. Create a reproducible populated customer-database walkthrough/script using a fresh checkout and documented PostgreSQL prerequisites; do not silently target or reset a production/default database. Prefer an explicit disposable database variable and isolated demo schema, with clear cleanup behavior.
Provide actual example artifacts/reports, complete command documentation and a supported/unsupported matrix. Run the full final build/unit/integration and walkthrough checks against the final state; fix real failures or document precise blockers. CI already has 16.10/17.6 services and privileged disposable test credentials; local native PostgreSQL remains unavailable.
Update the website to describe demonstrated prototype behavior, including accurate limitations and contact. Keep it unreleased/all rights reserved unless the owner separately changes that instruction. Do not infer release authorization from HyperCrux's pattern.
Finish with code, resume and website commits, actual validation evidence and a clear completed-prototype handoff. Stop after session 6 and await the owner's next direction.
