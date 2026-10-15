# Command reference

Build with Go 1.24+: go build -o bin/forgeschema ./cmd/forgeschema. CLI version 0.0.6-dev is unreleased. All database commands use DATABASE_URL from the environment; keep credentials out of arguments and logs.

| Command | Result |
| --- | --- |
| forgeschema validate SCHEMA.yaml | Strictly validate YAML; print normalized canonical JSON. |
| forgeschema inspect SCHEMA_NAME | Read supported PostgreSQL catalog structures; print canonical JSON. |
| forgeschema plan SOURCE TARGET [RENAMES.yaml] | Offline comparison; print format 1 JSON plan. Inputs accept schema YAML or inspection JSON. |
| forgeschema plan --explain SOURCE TARGET [RENAMES.yaml] | Print readable operations or blockers without executing SQL. |
| forgeschema apply PLAN.json | Strictly validate the saved ready artifact, check source under locks, apply transactionally, verify target and record history. |
| forgeschema verify EXPECTED | Read-only comparison with a complete desired schema YAML/inspection JSON. |
| forgeschema verify --plan PLAN.json | Strictly validate a ready plan and compare its target with the database. |
| forgeschema version | Print development version. |
| forgeschema help | Print usage. |

```sh
./bin/forgeschema inspect public > current.json
./bin/forgeschema plan current.json desired.yaml renames.yaml > reviewed-plan.json
./bin/forgeschema plan --explain current.json desired.yaml renames.yaml
# Review the artifact and coordinate external DDL before execution.
./bin/forgeschema apply reviewed-plan.json
./bin/forgeschema verify --plan reviewed-plan.json
```

Inspect and verify use a 30-second CLI deadline. Apply defaults to 5-second lock, 30-second statement and 60-second overall deadlines. ACCESS EXCLUSIVE relation locks can block application reads and writes. An uncertain commit error requires investigation using its IDs; a later structural match alone does not establish history or commit outcome.

| Exit | Meaning |
| --- | --- |
| 0 | Successful command, ready plan or verification match. |
| 1 | Invalid input, database failure, unsupported inspected structure or failed apply. |
| 2 | Usage/configuration error, including missing database URL. |
| 3 | Blocked plan, or blocked target supplied to verify --plan. |
| 4 | Verification found supported structural drift; JSON report is on stdout. |

Errors go to stderr. A blocked plan includes reasons and no executable operations. Verify drift is a report, not an automatic repair. See SCHEMA.md, INSPECTION.md, PLANNING.md, APPLY.md and VERIFY.md for exact APIs, strict formats and limits. scripts/demo.sh is a separate disposable-database harness documented in WALKTHROUGH.md.
