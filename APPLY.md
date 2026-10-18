# Execute a reviewed migration plan

ForgeSchema applies an exact supported plan, without recalculating a different migration at execution time. This is a development prototype supporting PostgreSQL 16 and 17, not a production-readiness claim. The operator must pause external DDL in the managed schema and keep database-wide hooks/dependencies outside the supported contract under control. Existing-table locks do not protect an entire schema namespace against unrelated CREATEs; ForgeSchema cannot certify external DDL quiescence.

```sh
# DATABASE_URL is already set securely in the environment.
./bin/forgeschema inspect public > current.json
./bin/forgeschema plan current.json desired.yaml changes.yaml > reviewed-plan.json
# Read the artifact and its explained operations before executing that file.
./bin/forgeschema apply reviewed-plan.json
```

The apply command reads a JSON plan file and uses DATABASE_URL from the environment. It prints a JSON result with status, plan_id, migration_id, operations and target_fingerprint after a confirmed commit. A no-op returns no_changes with no migration_id and does not create history. Errors produce no success result. Neither replanning nor partial execution is offered.

| Exit | Meaning |
| --- | --- |
| 0 | Applied, or no changes. |
| 1 | Invalid input, or a lock, SQL or history failure; the transaction was not committed (for an online plan, the message says when its first phase was, and the plan can be resumed). The message names the stage and includes PostgreSQL's message and SQLSTATE, for example a lock timeout (55P03), NULLs found by SET NOT NULL (23502) or duplicates found by a unique constraint (23505). PostgreSQL's detail line is left out because it can quote row values, such as the duplicated key. |
| 2 | Usage or configuration error, including missing DATABASE_URL or an invalid timeout. |
| 3 | The artifact is a blocked plan. |
| 5 | Drift: the live source no longer matches the reviewed plan (inspect again and build a new plan), or the result didn't match the reviewed target, usually because of concurrent DDL. Nothing was committed. |
| 6 | The commit outcome is unknown; see below. |

Connection failures while starting the transaction, and authentication or missing-database errors, are reported generically, because their messages can echo connection details.

## Artifact acceptance

The bounded JSON loader accepts at most 8 MiB and exactly one UTF-8 JSON object. Exact field names and required fields are enforced; duplicate/unknown keys, nulls, scalar coercions, noninteger versions, extra documents and inconsistent plans are rejected. Whitespace, key ordering and equivalent JSON escape spelling do not change the artifact identity.

Plan.Validate reconstructs the artifact from its embedded schemas and rename intent. Changing only SQL, operations, explanations, status or fingerprints fails validation. This is consistency checking, not a signature or authorization system. The API freezes a private validated copy before acquiring database locks. It executes canonical supported operations only; arbitrary supplied SQL is not an extension point. Existing format version 1 plans remain compatible.

## Transaction and coordination

Apply opens a READ COMMITTED transaction with standard_conforming_strings on, whatever the role or connection sets, and takes a database-wide transaction advisory lock shared by all ForgeSchema apply runs. It then locks only the existing tables the plan changes, in ACCESS EXCLUSIVE mode and name order, plus SHARE ROW EXCLUSIVE on existing tables referenced by new foreign keys, which is the mode PostgreSQL itself needs to add one. Locks are held until commit. ACCESS EXCLUSIVE blocks application reads and writes on the changed tables; SHARE ROW EXCLUSIVE blocks writes but not reads. A column type change rebuilds the foreign keys the column takes part in, so the table at the other end of each such key is also locked ACCESS EXCLUSIVE, up front with the others. Other tables are not locked, and inspection takes no table locks, so apply doesn't wait behind traffic on them. `forgeschema plan --explain` lists the lock set.

Only after all relation locks have been acquired does Apply read catalog metadata and compare the live source fingerprint with the reviewed source. READ COMMITTED is deliberate: an inspection snapshot taken before waiting for a relation lock would miss a schema change committed while waiting. The source check uses fresh catalog reads after that wait.

The engine validates isolated bookkeeping storage, executes the frozen operations, reinspects the target, compares its fingerprint, writes migration history and commits. Every operation of a plain plan is transactional DDL. Failure before commit rolls back the transaction's application DDL, metadata creation and history insert together. Online plans, below, are the one exception.

Default lock timeout is five seconds, statement timeout thirty seconds and overall Apply deadline sixty seconds (or the caller's earlier context deadline). The CLI flags --lock-timeout, --statement-timeout and --timeout take Go durations such as 500ms, 2m or 1h, for example `forgeschema apply --statement-timeout 30m --timeout 45m reviewed-plan.json` for an index build on a large table. The Go API postgres.Apply(ctx, db, plan, ApplyOptions{LockTimeout, StatementTimeout, Timeout}) accepts the same values; each must be at least one millisecond, and zero selects the default. PostgreSQL permissions and resource/physical limits can still cause a supported migration to fail.

The advisory lock coordinates ForgeSchema clients only. Relation locks stabilize the changed tables and newly created relations, but do not prevent DDL on unchanged tables, outside-schema dependencies, event triggers or unrelated schema-level CREATEs. The source check under locks and the final target verification catch structural changes made before them; neither can eliminate an external DDL race after the final check. Run this prototype with external DDL paused. Ownership, grants, comments, row contents and other metadata excluded by INSPECTION.md are not fingerprint drift checks.

## Online plans

A plan built with `plan --online` (PLANNING.md) runs in three phases on one connection, which holds a session-level form of the same advisory lock from start to finish, so no other ForgeSchema apply runs in between.

1. A transaction locks the tables its operations change, checks the live source, runs the operations before the builds, checks the result against the plan's intermediate schema and commits.
2. Each CREATE INDEX CONCURRENTLY runs on its own. It takes no lock that blocks reads or writes, but it waits for transactions already running on the table, and it reads the table twice, so a large table takes a while: raise --statement-timeout and --timeout to match.
3. A transaction locks the tables it changes, checks the schema is exactly what the first two phases produced, attaches unique constraints, adds foreign keys, checks the full target, writes history and commits.

If phase 2 or 3 fails, phase 1 stays committed and no history is written. A failed build, for example over duplicate values for a unique index (23505), leaves an invalid index behind, as PostgreSQL always does. The CLI exits 1 and says the first phase is committed. Fix the cause and run the same apply again: it recognises the committed first phase and any indexes already built, drops the invalid index with DROP INDEX CONCURRENTLY, builds it again and finishes. Any other change to the schema in between is drift (exit 5). A finished plan is never applied twice: when the schema looks resumable but history already holds the plan, apply reports drift.

Inspect and verify reject a schema with an invalid index, so an unfinished online apply shows up there too until it is resumed.

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

A connection error during commit may leave the outcome unknown. The API returns CommitError with the plan and migration IDs; the CLI exits 6 and reports those IDs and an explicit uncertainty message. It does not label that situation a confirmed rollback. Inspect the schema and history before deciding whether to retry. Crash recovery/fault-injection coverage beyond the tested transactional failure cases remains later work.

## Verified cases

Live PostgreSQL 16.10 and 17.6 tests cover successful apply/history and stale replay rejection, no-ops, malformed/tampered/blocked plans, competing runs, fresh source checks after a lock wait, lock timeouts and lock release, rollback after a real SQL failure, rollback on a target mismatch, CLI application, altered history rejection and a private frozen artifact unaffected by caller-side mutation during a lock wait. Test database users are privileged and disposable; those credentials are not production connection settings. Standalone verify is available; see VERIFY.md. Further tests cover the full CLI workflow, cancellation, statement timeout, metadata bootstrap failure, history-recording failure, applying the shop example to an empty schema, column and constraint changes on populated tables, rollback with PostgreSQL's reason on NULLs and duplicates, apply proceeding while an unchanged table is exclusively locked by another session, the timeout flags and exit codes 5 and 2.
