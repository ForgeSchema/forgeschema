# Read-only standalone verification

The verify command compares the live managed PostgreSQL schema with a supported expected schema. It performs no DDL, repairs, application-row reads or migration-history writes. It reuses the inspector's READ ONLY, REPEATABLE READ snapshot and the same canonical schema contract as planning.

```sh
# DATABASE_URL is already supplied securely in the environment.
./bin/forgeschema verify desired.yaml
./bin/forgeschema verify --plan reviewed-plan.json
```

The first form accepts a schema YAML file or canonical inspection JSON. The second strictly loads a ready plan and checks its target; the command does not guess the input format. A blocked plan is rejected. The Go API is postgres.Verify(ctx, db, expectedSchema); callers supply a context deadline, while the CLI uses thirty seconds.

## Results and exit codes

A completed comparison prints one JSON object with status (match or drift), schema, expected_fingerprint, actual_fingerprint and differences. The differences array is always present, including [] for a match. Exit 0 means the complete supported structural snapshot matches. Exit 4 means observed supported structural drift, with the report still written to stdout. Exit 1 means input, connection, unsupported schema/server/object or inspection failure, without a partial report. Exit 2 means usage/missing DATABASE_URL. Exit 3 rejects a blocked plan target.

Each difference contains kind, table, optional column/index/constraint and string expected/actual values. Presence uses present/absent. Types use normalized PostgreSQL names; nullability and index uniqueness use true/false strings; defaults use the canonical forms in SCHEMA.md and identity uses always/by_default, with none for absence. Ordered key, constraint and index columns use compact JSON-array strings. Foreign keys are described as columns, referenced table and columns, and any non-default actions, for example ["customer_id"] references customers["id"] on_delete restrict. Kinds are table_missing, table_unexpected, column_missing, column_unexpected, column_type, column_nullability, column_default, column_identity, primary_key_columns, unique_missing, unique_unexpected, unique_columns, index_missing, index_unexpected, index_columns, index_unique, foreign_key_missing, foreign_key_unexpected and foreign_key_definition. Missing/extra whole tables produce one table-level difference rather than redundant reports for every child object. Reports are sorted deterministically by table, column, index, constraint and kind.

Verification describes differences rather than proposing changes. It does not infer renames or apply migration policy: an old name and a new name appear as unexpected/missing columns, not an assumed data-preserving rename. It generates no SQL and does not repair anything. A structure the inspector cannot represent is an error, not a partially complete drift report.

## Scope

The expected file describes the entire supported managed schema, not a subset. Extra supported tables, columns, constraints and indexes are drift. Type aliases and nonsemantic table/column/index ordering normalize; ordered key/index columns remain significant. Unknown schemas and unsupported objects fail explicitly. See SCHEMA.md and INSPECTION.md for the representation and excluded metadata.

A match is a structural snapshot result, not proof of application data correctness, permissions, historical execution of a particular artifact or continued consistency after the snapshot. Verify --plan checks the target regardless of whether that plan was applied by ForgeSchema or achieved another way. It does not consult history. After an unconfirmed commit, use both structural inspection/verification and the identifiers in migration history as described in APPLY.md; the command does not automatically resolve an unknown commit outcome.

## Validation

Unit tests cover every report kind, deterministic output, aliases, input ordering and ordered key/index differences. Live tests inspect under a role with no application-table privileges and default read-only transactions, return errors for unsupported structures/cancellation, and exercise the complete inspect → plan → apply → verify CLI workflow with six supported operations and preserved customer rows. Later DDL yields a drift report without repair or history changes.

Execution failure coverage also includes cancellation while waiting for a lock, statement timeout rollback, metadata bootstrap rollback and an INSERT privilege failure when recording history after application DDL and target verification. Those tests use dedicated disposable privileged test databases and temporarily isolate their metadata namespace; they must not be run against production data. General crash recovery and commit-network-failure fault injection are not established by these cases.
