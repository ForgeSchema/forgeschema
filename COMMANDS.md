# Command reference

Build with Go 1.24+: go build -o bin/forgeschema ./cmd/forgeschema. The CLI version is 0.2.0-alpha.1, an alpha prerelease; downloadable binaries and checksums are on the GitHub release page. All database commands use DATABASE_URL from the environment; keep credentials out of arguments and logs.

| Command | Result |
| --- | --- |
| forgeschema validate SCHEMA.yaml | Strictly validate YAML; print normalized canonical JSON. |
| forgeschema inspect SCHEMA_NAME | Read supported PostgreSQL catalog structures; print canonical JSON. |
| forgeschema plan SOURCE TARGET [RENAMES.yaml] | Offline comparison; print format 1 JSON plan. Inputs accept schema YAML or inspection JSON. |
| forgeschema plan --explain SOURCE TARGET [RENAMES.yaml] | Print readable operations or blockers without executing SQL. |
| forgeschema apply [--lock-timeout D] [--statement-timeout D] [--timeout D] PLAN.json | Strictly validate the saved ready artifact, lock the tables it changes, check the source, apply transactionally, verify the target and record history. D is a duration such as 5s, 30m or 1h. |
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

Inspect and verify use a 30-second CLI deadline. Apply defaults to 5-second lock, 30-second statement and 60-second overall deadlines; raise them with the flags for slow steps such as large index builds. Apply's ACCESS EXCLUSIVE locks on the tables a plan changes (and on foreign key partners of a column whose type changes) block application reads and writes on those tables; plan --explain lists them. An uncertain commit error requires investigation using its IDs; a later structural match alone does not establish history or commit outcome.

| Exit | Meaning |
| --- | --- |
| 0 | Successful command, ready plan or verification match. |
| 1 | Invalid input, database failure, unsupported inspected structure or failed apply. Database errors include PostgreSQL's message and SQLSTATE, except connection and authentication errors, which stay generic. |
| 2 | Usage/configuration error, including missing database URL. |
| 3 | Blocked plan, or blocked target supplied to verify --plan. |
| 4 | Verification found supported structural drift; JSON report is on stdout. |
| 5 | Apply found the database no longer matches the plan (source or target drift); nothing was committed. |
| 6 | Apply could not confirm the commit; investigate with the reported IDs before retrying. |

Errors go to stderr. A blocked plan includes reasons and no executable operations. Verify drift is a report, not an automatic repair. See SCHEMA.md, INSPECTION.md, PLANNING.md, APPLY.md and VERIFY.md for exact APIs, strict formats and limits. scripts/demo.sh is a separate disposable-database harness documented in WALKTHROUGH.md.
