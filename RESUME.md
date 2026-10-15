# ForgeSchema resume checkpoint

Updated: 2026-10-15, end of day 2 session 4.

Status: PAUSED. Sessions 1–4 are complete. Await explicit go-ahead for session 5 only. Do not start standalone verification or further hardening during this checkpoint.

## Published checkpoints
Engine implementation/evidence: df6c1e1a5ffea1e519fc511cb15b1e219c237d3a.
Final functional implementation (artifact freezing): 4304185920af976a1caf653730b02ee62b03d942.
Website session 4 content: fc2d35b7e093b6d1cf094eb272fc1388bc488674.
Contact email was added separately in website commit a7fd39fcb372fb6fa71803d1ba8009f4ce211d1f.
This resume update and package documentation correction are a separate checkpoint commit.

## Current functionality
CLI 0.0.4-dev, Go 1.24 minimum. Commands: validate, inspect, plan, apply, version and help. No independent verify command yet; apply verifies its own target.
Existing format version 1 plans remain compatible. Canonical operation generation and artifact fields were NOT changed. Planning stays offline; its summary text now accurately says that planning itself does not execute SQL.
plan.Load strictly reads at most 8 MiB of one UTF-8 JSON object, using exact field names and requiring nonoptional fields. Duplicate/unknown keys, nulls, wrong scalar types, noninteger versions, trailing documents and inconsistent/tampered artifacts are rejected. Whitespace/key order do not matter. Plan.ID hashes the validated compact artifact, not the input file formatting.
postgres.Apply(ctx, db, plan, ApplyOptions) validates and freezes a private artifact copy before touching the database or waiting on locks. Blocked plans are rejected. It never replans and does not accept arbitrary SQL extensions.
Apply opens READ COMMITTED, sets local catalog search_path, application_name and timeouts, takes a fixed database-wide transaction advisory lock, and locks every source table in normalized order using ACCESS EXCLUSIVE. It inspects the live source AFTER acquiring locks, so schema changes committed during a lock wait are visible. A source fingerprint mismatch aborts.
It validates isolated history storage, executes supported frozen operations, reinspects and verifies the target, records history and commits. Application DDL, metadata creation and the history insert share one transaction. No-op plans verify source and return no_changes with no history write.
History lives in reserved forgeschema_meta.migrations, outside every allowed managed schema. Schema/table ownership and ACLs must belong exclusively to the applying role; table shape, scope, constraints/indexes, defaults and unsupported features are checked. The history table is locked while being validated/used. No silent managed-table exclusions were added to inspection.
History records random migration ID, canonical plan ID, source/target fingerprints, managed schema, operation count, database timestamp and full artifact JSONB. Replaying an already applied non-no-op normally fails source drift; it is not silently treated as success.
Apply defaults: lock timeout 5s, statement timeout 30s, overall deadline 60s or the caller's earlier deadline. API supports positive lock/statement overrides of at least 1ms. CLI uses defaults and DATABASE_URL.
CLI apply result is JSON only after confirmed commit. Input/SQL/drift/verification failures exit 1, usage/missing configuration 2, blocked artifacts 3. Connection errors are sanitized. CommitError reports uncertainty and plan/migration IDs rather than claiming rollback; inspect schema/history before retrying.

## Proven results
Both PostgreSQL 16.10 and 17.6 passed the final functional suite:
https://github.com/ForgeSchema/forgeschema/actions/runs/38698348149
Tests cover successful apply and exact history, stale replay rejection, no-ops, malformed/tampered/blocked plans, competing applies (one succeeds, the second sees source drift), fresh source checks after lock waits, lock timeouts/release, real SQL-error rollback, target-mismatch rollback, CLI apply, modified history rejection, invalid options and a caller mutation that cannot affect the already frozen artifact while Apply waits.
All earlier schema, planner, generated-SQL and inspection tests also passed. Selected verbatim logs: test/results/session-4-postgres-16.10.txt and session-4-postgres-17.6.txt.
Final implementation/evidence commit CI also passed on both versions:
https://github.com/ForgeSchema/forgeschema/actions/runs/38698747629
Local scripts/check.sh passed (race, vet, build and examples), and a short new JSON loader fuzz smoke run passed after 146 executions using a valid artifact seed. The stage 3 JSON example remains byte-identical to regeneration. Transcript: test/results/session-4-local.txt. Database tests skip locally; real PostgreSQL evidence is hosted CI.
Hugo 0.158.0 built the website (27 generated pages), internal links/assets resolved, all 18 theme blobs remained unchanged. Site content/status/roadmap/build/schema/workflow and session 4 note now reflect apply, with operational limits.

## Website/contact
The owner connected ForgeSchema.com. HTTPS serves the project; the live homepage now shows Transactional migration execution. The contact page contains info@forgeschema.com and a mail link. Cloudflare email protection rewrites it in delivered HTML; decoding data-cfemail confirmed the requested address. Do not alter the template or DNS/domain configuration.

## Material limitations
This is an unreleased prototype, all rights reserved. No public binary release or license change.
External DDL must be paused. Advisory locks coordinate ForgeSchema clients only; relation locks stabilize existing tables but cannot prohibit every namespace CREATE or outside-schema/database-wide hook. Target verification catches observed unexpected structures, not every race after the check. External dependencies, permissions, comments/grants, data contents and physical dropped-column limits remain outside structural fingerprints.
ACCESS EXCLUSIVE on all source tables can block application reads and writes. Use a consistent migration role: metadata storage requires exclusive role ownership/ACLs and initial database CREATE permission, plus normal DDL ownership/privileges. Metadata grants are not silently changed.
Commit connection failures may have unknown outcomes. SQL and target-mismatch rollback were tested; general crash recovery and commit-network-failure fault injection have NOT been established.

## Environment/publishing
Tools currently /tmp/go/bin and /tmp/forgeschema-hugo, transient. Docker is absent; root-only UID/GID mapping prevents normal native PostgreSQL startup. Do not bypass PostgreSQL root protections. CI provides disposable official PostgreSQL services with test-only credentials.
Public shell clone/fetch works; private clones/pushes lack credentials. Use GitHub connector create_tree/create_commit/update_ref with expected head SHA and fresh branch reads. Preserve theme/templates/layout/CSS/structure. Website contact address must persist.

## Next session, only after go-ahead
Session 5: add read-only standalone verify with useful results/exit codes, then appropriate remaining failure cases and end-to-end checks. Reuse canonical schema/inspection functions without writes. Extend CLI documentation and examples accurately.
Consider coverage of a full multi-operation plan through the actual Apply API, context cancellation, statement timeout, metadata bootstrap failures and transactional history failure. Prioritize meaningful behavior, not mirror tests. Preserve format version 1 compatibility or explicitly version any generator changes. Do not claim general crash safety/commit recovery without evidence.
Read PLAN.md, APPLY.md, PLANNING.md, SCHEMA.md and INSPECTION.md first. Finish by committing code, updating this file and website with verified results, reporting and pausing for session 6 authorization.
