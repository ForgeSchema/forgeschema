# Migration planning, format version 1

The planner is offline. It compares two supported schema documents and generates SQL for review; it never connects to PostgreSQL or executes that SQL. Source and target must name the same existing schema. An inspection snapshot is canonical JSON, which the restricted schema YAML reader also accepts.

```sh
# DATABASE_URL is already set securely in the environment.
./bin/forgeschema inspect public > current.json
./bin/forgeschema plan current.json examples/customers-next.yaml examples/customer-renames.yaml > proposed-plan.json
./bin/forgeschema plan --explain current.json examples/customers-next.yaml examples/customer-renames.yaml
```

A connection-free example uses examples/customers.yaml instead of current.json. examples/customer-plan.json and examples/customer-plan.txt show its actual output. The default output is pretty-printed JSON; --explain prints a readable review instead. Output is deterministic, with no timestamps or random identifiers. Exit codes: 0 for a ready plan (including no changes), 1 for invalid inputs, 2 for command usage, and 3 for a blocked plan. Exit 3 still writes the complete blocked artifact to stdout; ordinary input errors write diagnostics to stderr and no artifact.

## Explicit change input

The optional third argument is a strict YAML change file. It says what the planner must never guess: column renames, drops and type conversions. A 0.1 rename file is a valid change file.

```yaml
version: 1
columns:                  # renames
- table: order_lines
  from: quantity_new
  to: quantity
drops:                    # objects the plan may delete
- table: order_lines
  column: quantity
- table: order_lines
  check: order_lines_quantity_check
- table: audit_log        # a table alone drops the whole table
conversions:              # columns whose stored values may be converted
- table: orders
  column: code
```

columns may be omitted when drops or conversions are given. Unknown fields, duplicates and entries that do not match the two schemas are input errors (exit 1).

### Renames

The columns sequence holds mappings with table, from and to. An empty sequence is valid. The table must exist in both schemas; from must be a source column absent from the target, and to must be a target column absent from the source. Mapping sources and targets must be one-to-one. Identical names, missing columns, reuse of the old name, chains and swaps are rejected. No column or table rename is inferred from similarity. Malformed mappings are input errors, not partially accepted instructions.

Column identities are remapped in a comparison copy, including their primary key, unique constraint, index and foreign key references, and foreign keys in other tables that reference the renamed column. PostgreSQL retains the column's data and updates these references during RENAME COLUMN. A rename can be combined with supported column changes such as a new default. Renaming an identity column is blocked, because PostgreSQL keeps the old TABLE_COLUMN_seq sequence name. Index and constraint names do not change automatically in the plan; changing their definitions or names remains unsupported. A rename may take the name of a column the same plan drops; the drop runs first.

### Drops

Removing an object from the target blocks the plan unless the change file lists it under drops. Each drop names a table and, optionally, one column, index, unique, foreign_key or check on it. The object must exist in the source and be absent from the target. A table drop takes its own columns, indexes and constraints with it; list only the table. Everything else that would go must be listed: when a column is dropped, the indexes, unique constraints, foreign keys and checks that use it must be listed too, and so must foreign keys in other tables that reference a dropped table or column. Otherwise the plan is blocked and says which entry is missing. A primary key column cannot be dropped, since changing a primary key is unsupported.

Drops run before everything else, in this order: drop_foreign_key, drop_check, drop_unique, drop_index, drop_column, drop_table. A dropped table's foreign keys to other tables are removed first (drop_foreign_key), so the keys and columns they reference can be dropped and the tables can go in any order. PostgreSQL ties each foreign key to one unique index of the referenced columns, and the schema does not say which, so dropping a unique constraint or unique index is blocked while a kept foreign key references the same columns. drop_column and drop_table delete data permanently; their explanations say so, and --explain starts with a line listing every column and table the plan deletes. PostgreSQL's DROP fails, and the migration rolls back, if an object outside the plan still depends on the dropped one. Dropping a foreign key, or a table that has one, also locks the referenced table.

### Conversions

A type change that is not one of the widenings below blocks the plan unless the column is listed under conversions, by its target name. A listed conversion becomes convert_column_type: ALTER COLUMN ... TYPE with USING column::type, so PostgreSQL converts each stored value. It rewrites the table and its indexes under an exclusive lock, and the migration rolls back if any value does not convert (text 'x' to integer fails with 22P02). Only conversions whose result depends on the stored value alone are planned:

| From | To |
| --- | --- |
| integer types | integer types, numeric, text, varchar without a length |
| numeric | integer types, numeric, text, varchar without a length |
| real, double precision | integer types, numeric; real to double precision |
| text, varchar, char | each other, integer types, numeric, boolean, uuid |
| boolean | integer, text, varchar without a length |
| uuid, jsonb | text, varchar without a length |
| date | timestamp without time zone |

Everything else is blocked with a reason: conversions that depend on session settings (dates and timestamps to or from text, timestamps with and without time zone, bytea, real and double precision to text), that lose information without failing (to real or double precision, except real to double precision), or that reinterpret values (integer to boolean, text to jsonb). The explanation notes rounding: numeric, real or double precision to an integer rounds fractions, and a numeric with a smaller scale rounds to that scale. Between text types there is no USING clause, so a value longer than a new varchar(n) or char(n) length makes the conversion fail (22001) instead of being cut short; converting another type family to a length-limited text type is blocked for the same reason. A default is dropped before the conversion and the target's default set after it. Identity columns, columns in a check constraint that stays, and columns in a foreign key are not converted; the plan is blocked and says why.

## Staged changes

Some changes are safer in steps, each its own reviewed plan, with application changes or a backfill in between. ForgeSchema plans and applies each step; it never writes application rows, so backfills are yours to run.

A type change without a long exclusive lock, here integer to bigint on order_lines.quantity:

1. Expand: add quantity_new bigint, nullable. Apply. Deploy code that writes both columns.
2. Backfill: `UPDATE order_lines SET quantity_new = quantity`, in batches on a large table.
3. Contract: target has quantity bigint NOT NULL with the old default and check, and no quantity_new. The change file renames quantity_new to quantity and drops the old column and its check (the example above). Apply. The plan drops the check and column, renames, sets NOT NULL (a table scan) and the default, and adds the check again (another scan).

A required column on an existing table: add it nullable or with a default, backfill, then make it NOT NULL in a later plan. A rename that old code still reads: add the new column, write both, backfill, then drop the old one. The live tests run the type change above end to end through the CLI.

## Artifact contract

The JSON object has format_version (1), canonical source and target schema objects, source_fingerprint and target_fingerprint (SHA-256 of canonical schema JSON), sorted renames, status, operations and blocked. All three lists are present, including when empty. Plans with drops or conversions, or built with --online, also carry drops, conversions and online; these are omitted otherwise, so older plans keep their exact JSON and plan ID. Each operation contains kind, table, optional subject, sql and explanation. Each blocker contains object and reason.

The status is ready when the supported structural comparison contains no blockers. This means ready for review, not approved or guaranteed executable against a live database. A blocked plan contains ordered reasons and an EMPTY operations list, including no SQL for changes that would otherwise be supported. There is no partial-apply interpretation. Equivalent schemas yield ready with no operations.

The Go API is plan.Build(source, target, renames), or plan.BuildWith with plan.Options{Online, Drops, Conversions}; plan.LoadChanges reads a change file. Plan.Validate reconstructs the entire canonical artifact from its embedded schemas and change intent and rejects changes to operations, SQL, explanations, status, format or fingerprints. Plan.JSON and Plan.Summary perform that consistency check before output. The check is not a signature, authorization or evidence that a database still matches. The plan.Load JSON loader and postgres.Apply executor are now available; read APPLY.md for strict artifact acceptance, drift checks, transaction behavior and operational limits.

## Supported operations and order

Plans run in seven phases after any drops (see Drops above). Within each phase, normalized table/name order or sorted explicit rename order makes output deterministic.

1. create_table: new tables with their columns (types, defaults, identity, nullability), primary key, unique constraints and check constraints.
2. rename_column: explicit renames.
3. add_column: new columns on existing tables. A column needs to be nullable or have a default. A constant or now()-style default is stored once, so existing rows get it without a rewrite; now(), current_timestamp and similar give every existing row the time of the migration. gen_random_uuid() is evaluated per row, so PostgreSQL rewrites the table; the explanation says so.
4. Column changes on existing tables: alter_column_type for widening only (longer or unbounded varchar, varchar to text, more numeric precision at the same scale, numeric(p,s) to numeric); set_not_null, which scans the table and fails if any row is NULL; drop_not_null; set_default and drop_default, which affect future inserts only.
5. add_check: new check constraints on existing tables, after the column changes they may depend on. PostgreSQL checks every existing row under an exclusive lock and fails if any row violates the constraint (SQLSTATE 23514).
6. add_unique and create_index: unique constraints and plain or unique indexes on new and existing tables. Building over existing rows fails if they contain duplicates.
7. add_foreign_key: every new foreign key, after all tables, columns and referenced unique keys exist. On an existing table PostgreSQL checks every row and fails if any has no match.

Any step that fails against real data (duplicates, NULLs, orphan rows) makes the whole transaction roll back; apply reports PostgreSQL's reason and SQLSTATE.

Every schema, table, column, constraint and index identifier is quoted. Types and defaults come only from the normalized schema whitelist, not arbitrary SQL input. A plain plan builds indexes with ordinary CREATE INDEX, which blocks writes to the table while it runs; see Online plans below for building them concurrently. ALTER TABLE operations require table locks; --explain lists the tables apply will lock. Planning does not estimate lock duration or inspect row counts.

A column rename carries the check constraints that use it along, as PostgreSQL does; the target lists them with the new name.

Still blocked: dropping tables, columns, indexes, unique constraints, foreign keys or check constraints that the change file does not list, and any implicit rename; type conversions other than the widening above that the change file does not list; adding a required column without a default; adding an identity column to an existing table, or adding, removing or changing identity on an existing column; renaming an identity column; altering a primary key; and changing an index, unique constraint, foreign key or check constraint definition. Required columns are allowed on a newly created empty table. Target normalization rejects relation-name collisions before a plan is generated; since ready plans remove no relations, existing names cannot be silently recycled.

## Online plans

`forgeschema plan --online` builds indexes on existing tables with CREATE INDEX CONCURRENTLY, so reads and writes keep going while a large table is indexed. A new unique constraint on an existing table is built the same way, as a unique index, and then attached with ADD CONSTRAINT ... UNIQUE USING INDEX, which takes a brief lock. Indexes on tables the plan creates are built with the table, as before. Plans without such indexes get the same operations with or without the flag, but the artifact records online: true, so its JSON and plan ID differ.

An online plan runs in three phases, and --explain says so:

1. one transaction for everything before the builds (new tables, renames, new columns, column changes), checked against the expected intermediate schema and committed;
2. each concurrent build on its own, outside any transaction (operation kind create_index_concurrently);
3. one transaction that attaches the unique constraints (attach_unique), adds the foreign keys, checks the full target and records history.

The online flag is part of the artifact: validation rebuilds the plan with the same flag, so changing it makes the plan fail validation. APPLY.md explains what happens when a phase fails.

## Compatibility with 0.1 plans

Plan format stays 1. Operations that 0.1 could produce keep their exact SQL and explanation text, so plans saved with 0.1 still load, validate and apply. Some changes that 0.1 blocked (nullability and default changes, widening) are now planned; a blocked 0.1 artifact for such a change no longer matches its rebuild and is rejected, so build a new plan. Blocked plans could never be applied in either version.

## Validation boundary

Unit tests cover no-op equivalence, normalized aliases and ordering, quoted identifiers, rename identity propagation into constraints and foreign keys, every new operation kind and its phase order, every remaining blocker, lock lists, blocked mixed proposals, malformed mappings, artifact round trips, tampered plans and loading the saved 0.1 example and walkthrough plans. The live PostgreSQL tests execute generated SQL for combined table creation, primary-key/index column renames, nullable additions and index creation, create the whole shop example from an empty schema, apply column, constraint and foreign key changes to populated tables, check that failing changes (NULLs, duplicates, check violations, values that do not convert) roll back with PostgreSQL's reason, run a staged integer-to-bigint change through the CLI, drop tables linked by foreign keys, and build indexes online, including resuming after a failed build. The planner remains offline; the separate apply command handles reviewed-artifact execution.

The plan describes supported structural changes. It does not inspect external dependencies, permissions, dropped-column physical limits, disk capacity, lock contention or a changed database. Those may still cause execution failure. The separate executor supplies drift checks, coordination locks, transaction handling and migration history. They are not guarantees made by offline planning.
