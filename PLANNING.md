# Migration planning, format version 1

The planner is offline. It compares two supported schema documents and generates SQL for review; it never connects to PostgreSQL or executes that SQL. Source and target must name the same existing schema. An inspection snapshot is canonical JSON, which the restricted schema YAML reader also accepts.

```sh
# DATABASE_URL is already set securely in the environment.
./bin/forgeschema inspect public > current.json
./bin/forgeschema plan current.json examples/customers-next.yaml examples/customer-renames.yaml > proposed-plan.json
./bin/forgeschema plan --explain current.json examples/customers-next.yaml examples/customer-renames.yaml
```

A connection-free example uses examples/customers.yaml instead of current.json. examples/customer-plan.json and examples/customer-plan.txt show its actual output. The default output is pretty-printed JSON; --explain prints a readable review instead. Output is deterministic, with no timestamps or random identifiers. Exit codes: 0 for a ready plan (including no changes), 1 for invalid inputs, 2 for command usage, and 3 for a blocked plan. Exit 3 still writes the complete blocked artifact to stdout; ordinary input errors write diagnostics to stderr and no artifact.

## Explicit rename input

An optional, separate strict YAML file contains version: 1 and a columns sequence of mappings with table, from and to. An empty sequence is valid. The table must exist in both schemas; from must be a source column absent from the target, and to must be a target column absent from the source. Mapping sources and targets must be one-to-one. Identical names, missing columns, reuse of the old name, chains and swaps are rejected. No column or table rename is inferred from similarity. Malformed mappings are input errors, not partially accepted instructions.

Column identities are remapped in a comparison copy, including their primary key, unique constraint, index and foreign key references, and foreign keys in other tables that reference the renamed column. PostgreSQL retains the column's data and updates these references during RENAME COLUMN. A rename can be combined with supported column changes such as a new default. Renaming an identity column is blocked, because PostgreSQL keeps the old TABLE_COLUMN_seq sequence name. Index and constraint names do not change automatically in the plan; changing their definitions or names remains unsupported.

## Artifact contract

The JSON object has format_version (1), canonical source and target schema objects, source_fingerprint and target_fingerprint (SHA-256 of canonical schema JSON), sorted renames, status, operations and blocked. All three lists are present, including when empty. Each operation contains kind, table, optional subject, sql and explanation. Each blocker contains object and reason.

The status is ready when the supported structural comparison contains no blockers. This means ready for review, not approved or guaranteed executable against a live database. A blocked plan contains ordered reasons and an EMPTY operations list, including no SQL for changes that would otherwise be supported. There is no partial-apply interpretation. Equivalent schemas yield ready with no operations.

The Go API is plan.Build(source, target, renames). Plan.Validate reconstructs the entire canonical artifact from its embedded schemas and rename mappings and rejects changes to operations, SQL, explanations, status, format or fingerprints. Plan.JSON and Plan.Summary perform that consistency check before output. The check is not a signature, authorization or evidence that a database still matches. The plan.Load JSON loader and postgres.Apply executor are now available; read APPLY.md for strict artifact acceptance, drift checks, transaction behavior and operational limits.

## Supported operations and order

Plans run in six phases. Within each phase, normalized table/name order or sorted explicit rename order makes output deterministic.

1. create_table: new tables with their columns (types, defaults, identity, nullability), primary key and unique constraints.
2. rename_column: explicit renames.
3. add_column: new columns on existing tables. A column needs to be nullable or have a default. A constant or now()-style default is stored once, so existing rows get it without a rewrite; now(), current_timestamp and similar give every existing row the time of the migration. gen_random_uuid() is evaluated per row, so PostgreSQL rewrites the table; the explanation says so.
4. Column changes on existing tables: alter_column_type for widening only (longer or unbounded varchar, varchar to text, more numeric precision at the same scale, numeric(p,s) to numeric); set_not_null, which scans the table and fails if any row is NULL; drop_not_null; set_default and drop_default, which affect future inserts only.
5. add_unique and create_index: unique constraints and plain or unique indexes on new and existing tables. Building over existing rows fails if they contain duplicates.
6. add_foreign_key: every new foreign key, after all tables, columns and referenced unique keys exist. On an existing table PostgreSQL checks every row and fails if any has no match.

Any step that fails against real data (duplicates, NULLs, orphan rows) makes the whole transaction roll back; apply reports PostgreSQL's reason and SQLSTATE.

Every schema, table, column, constraint and index identifier is quoted. Types and defaults come only from the normalized schema whitelist, not arbitrary SQL input. A plain plan builds indexes with ordinary CREATE INDEX, which blocks writes to the table while it runs; see Online plans below for building them concurrently. ALTER TABLE operations require table locks; --explain lists the tables apply will lock. Planning does not estimate lock duration or inspect row counts.

Still blocked: dropping or implicitly renaming tables, columns, indexes, unique constraints or foreign keys; type conversions other than the widening above; adding a required column without a default; adding an identity column to an existing table, or adding, removing or changing identity on an existing column; renaming an identity column; altering a primary key; and changing an index, unique constraint or foreign key definition. Required columns are allowed on a newly created empty table. Target normalization rejects relation-name collisions before a plan is generated; since ready plans remove no relations, existing names cannot be silently recycled.

## Online plans

`forgeschema plan --online` builds indexes on existing tables with CREATE INDEX CONCURRENTLY, so reads and writes keep going while a large table is indexed. A new unique constraint on an existing table is built the same way, as a unique index, and then attached with ADD CONSTRAINT ... UNIQUE USING INDEX, which takes a brief lock. Indexes on tables the plan creates are built with the table, as before. Plans without such indexes come out the same with or without the flag.

An online plan runs in three phases, and --explain says so:

1. one transaction for everything before the builds (new tables, renames, new columns, column changes), checked against the expected intermediate schema and committed;
2. each concurrent build on its own, outside any transaction (operation kind create_index_concurrently);
3. one transaction that attaches the unique constraints (attach_unique), adds the foreign keys, checks the full target and records history.

The online flag is part of the artifact: validation rebuilds the plan with the same flag, so changing it makes the plan fail validation. APPLY.md explains what happens when a phase fails.

## Compatibility with 0.1 plans

Plan format stays 1. Operations that 0.1 could produce keep their exact SQL and explanation text, so plans saved with 0.1 still load, validate and apply. Some changes that 0.1 blocked (nullability and default changes, widening) are now planned; a blocked 0.1 artifact for such a change no longer matches its rebuild and is rejected, so build a new plan. Blocked plans could never be applied in either version.

## Validation boundary

Unit tests cover no-op equivalence, normalized aliases and ordering, quoted identifiers, rename identity propagation into constraints and foreign keys, every new operation kind and its phase order, every remaining blocker, lock lists, blocked mixed proposals, malformed mappings, artifact round trips, tampered plans and loading the saved 0.1 example and walkthrough plans. The live PostgreSQL tests execute generated SQL for combined table creation, primary-key/index column renames, nullable additions and index creation, create the whole shop example from an empty schema, apply column, constraint and foreign key changes to populated tables, and check that failing changes (NULLs, duplicates) roll back with PostgreSQL's reason. The planner remains offline; the separate apply command handles reviewed-artifact execution.

The plan describes supported structural changes. It does not inspect external dependencies, permissions, dropped-column physical limits, disk capacity, lock contention or a changed database. Those may still cause execution failure. The separate executor supplies drift checks, coordination locks, transaction handling and migration history. They are not guarantees made by offline planning.
