# Execute a reviewed migration plan

ForgeSchema applies an exact supported plan, without recalculating a different migration at execution time. This is a development prototype supporting PostgreSQL 16 and 17, not a production-readiness claim. The operator must pause external DDL in the managed schema and keep database-wide hooks/dependencies outside the supported contract under control. Existing-table locks do not protect an entire schema namespace against unrelated CREATEs; ForgeSchema cannot certify external DDL quiescence.

```sh
# DATABASE_URL is already set securely in the environment.
./bin/forgeschema inspect public > current.json
./bin/forgeschema plan current.json desired.yaml renames.yaml > reviewed-plan.json
# Read the artifact and its explained operations before executing that file.
./bin/forgeschema apply reviewed-plan.json
```

The apply command reads a JSON plan file and uses DATABASE_URL from the environment. It prints a JSON result with status, plan_id, migration_id, operations and target_fingerprint after a confirmed commit. A no-op returns no_changes with no migration_id and does not create history. A blocked artifact exits 3. Invalid input, drift, lock/SQL/verification failures and unconfirmed commits exit 1; usage/missing configuration exits 2. Errors produce no success result. Neither replanning nor partial execution is offered.

## Artifact acceptance

The bounded JSON loader accepts at most 8 MiB and exactly one UTF-8 JSON object. Exact field names and required fields are enforced; duplicate/unknown keys, nulls, scalar coercions, noninteger versions, extra documents and inconsistent plans are rejected. Whitespace, key ordering and equivalent JSON escape spelling do not change the artifact identity.

Plan.Validate reconstructs the artifact from its embedded schemas and rename intent. Changing only SQL, operations, explanations, status or fingerprints fails validation. This is consistency checking, not a signature or authorization system. The API freezes a private validated copy before acquiring database locks. It executes canonical supported operations only; arbitrary supplied SQL is not an extension point. Existing format version 1 plans remain compatible.

## Transaction and coordination

Apply opens a READ COMMITTED transaction and takes a database-wide transaction advisory lock shared by all ForgeSchema apply runs. It then takes ACCESS EXCLUSIVE locks on every source table in normalized order, retaining them until commit. These locks can block application reads and writes, including on source tables whose structure is not changing.

Only after all relation locks have been acquired does Apply read catalog metadata and compare the live source fingerprint with the reviewed source. READ COMMITTED is deliberate: an inspection snapshot taken before waiting for a relation lock would miss a schema change committed while waiting. The source check uses fresh catalog reads after that wait.

The engine validates isolated bookkeeping storage, executes the frozen operations, reinspects the target, compares its fingerprint, writes migration history and commits. Supported operations are transactional table creation, column renames, nullable additions and ordinary index creation. CONCURRENTLY and other nontransactional operations are not accepted by the canonical planner. Failure before commit rolls back the transaction's application DDL, metadata creation and history insert together.

Default lock timeout is five seconds, statement timeout thirty seconds and overall Apply deadline sixty seconds (or the caller's earlier context deadline). The Go API postgres.Apply(ctx, db, plan, ApplyOptions) can override lock and statement timeouts with positive durations of at least one millisecond. The CLI uses the defaults. PostgreSQL permissions and resource/physical limits can still cause a supported migration to fail.

The advisory lock coordinates ForgeSchema clients only. Relation locks stabilize existing source tables and newly created relations, but do not prohibit every outside-schema dependency, event trigger or unrelated schema-level CREATE. Final target verification catches observed unexpected structures; it cannot eliminate an external DDL race after that check. Run this prototype with external DDL paused. Ownership, grants, comments, row contents and other metadata excluded by INSPECTION.md are not fingerprint drift checks.

## History storage and role

History is isolated in forgeschema_meta.migrations, never silently excluded from a managed schema. That reserved namespace is not a valid managed schema. Its creation and rows are part of the migration transaction. The migration role needs CREATE privilege on the database for initial setup and the normal PostgreSQL ownership/schema privileges for the planned DDL.

The metadata schema and table must be owned exclusively by the applying role, with no other ACL grantees. Unexpected objects, columns, constraints, indexes, defaults, triggers, policies or definitions are rejected rather than adopted. Existing metadata is locked while it is validated and used. Use a consistent migration role; this prototype does not manage shared operator-role permissions or silently change existing grants.

Each history row contains a random migration ID, SHA-256 plan ID, managed schema name, source/target fingerprints, operation count, database timestamp and the exact artifact as JSONB. Plan IDs identify canonical artifacts independently of whitespace. An already applied plan normally fails the source check on replay; it is not treated as an automatic success. If the original source was deliberately restored, a later valid execution can record another migration ID for the same plan ID.

```sql
SELECT migration_id, plan_id, schema_name, operation_count, applied_at
FROM forgeschema_meta.migrations
ORDER BY applied_at;
```

## Unconfirmed commits

A connection error during commit may leave the outcome unknown. The API returns CommitError with the plan and migration IDs; the CLI reports those IDs and an explicit uncertainty message. It does not label that situation a confirmed rollback. Inspect the schema and history before deciding whether to retry. Crash recovery/fault-injection coverage beyond the tested transactional failure cases remains later work.

## Verified cases

Live PostgreSQL 16.10 and 17.6 tests cover successful apply/history and stale replay rejection, no-ops, malformed/tampered/blocked plans, competing runs, fresh source checks after a lock wait, lock timeouts and lock release, rollback after a real SQL failure, rollback on a target mismatch, CLI application, altered history rejection and a private frozen artifact unaffected by caller-side mutation during a lock wait. Test database users are privileged and disposable; those credentials are not production connection settings. Standalone verify is available; see VERIFY.md. Session 5 extends tests through the full CLI workflow, cancellation, statement timeout, metadata bootstrap failure and history-recording failure.
