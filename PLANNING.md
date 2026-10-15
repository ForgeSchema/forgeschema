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

Column identities are remapped in a comparison copy, including their primary key and ordinary index references. PostgreSQL retains the column's data and updates these references during RENAME COLUMN. A rename combined with a type/nullability change is still blocked. Index names and primary key constraint names do not change automatically in the plan; changing their definitions or names remains unsupported.

## Artifact contract

The JSON object has format_version (1), canonical source and target schema objects, source_fingerprint and target_fingerprint (SHA-256 of canonical schema JSON), sorted renames, status, operations and blocked. All three lists are present, including when empty. Each operation contains kind, table, optional subject, sql and explanation. Each blocker contains object and reason.

The status is ready when the supported structural comparison contains no blockers. This means ready for review, not approved or guaranteed executable against a live database. A blocked plan contains ordered reasons and an EMPTY operations list, including no SQL for changes that would otherwise be supported. There is no partial-apply interpretation. Equivalent schemas yield ready with no operations.

The Go API is plan.Build(source, target, renames). Plan.Validate reconstructs the entire canonical artifact from its embedded schemas and rename mappings and rejects changes to operations, SQL, explanations, status, format or fingerprints. Plan.JSON and Plan.Summary perform that consistency check before output. The check is not a signature, authorization or evidence that a database still matches. There is no external plan-file loader or executor yet; strict parsing and execution preconditions belong to session 4.

## Supported operation order

Plans first create new tables, including supported columns and a deterministically named primary key. They then rename existing columns, add nullable columns without defaults to existing tables, and finally create ordinary btree indexes. Within each phase, normalized table/name order or sorted explicit rename order makes output deterministic. Index creation follows the columns it references. No foreign key dependency graph is needed because foreign keys are outside the schema contract.

Every schema, table, column, constraint and index identifier is quoted. Types come only from the normalized schema whitelist, not arbitrary SQL input. Ordinary CREATE INDEX is not concurrent and can block writes. ALTER TABLE operations require table locks. Planning does not estimate lock duration or inspect row counts.

Dropping or implicitly renaming tables/columns, changing types/nullability, adding required columns to existing tables, altering existing primary keys, and dropping/renaming/rebuilding indexes are blocked. Required columns are allowed on a newly created empty table. Target normalization rejects relation-name collisions before a plan is generated; since ready plans remove no relations, existing names cannot be silently recycled.

## Validation boundary

Unit tests cover no-op equivalence, normalized aliases and ordering, quoted identifiers, rename identity propagation, blocked mixed proposals, malformed mappings, artifact round trips and tampered plans. The live PostgreSQL test harness executes generated SQL for combined table creation, primary-key/index column renames, nullable additions and index creation, then reinspects and checks original data. This test-only execution code does not introduce an apply command.

The plan describes supported structural changes. It does not inspect external dependencies, permissions, dropped-column physical limits, disk capacity, lock contention or a changed database. Those may still cause execution failure. Drift checks, coordination locks, transaction handling and migration history are the next checkpoint, not guarantees of this planner.
