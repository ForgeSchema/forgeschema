# Reproduce the prototype

Use a fresh checkout, Go 1.24 or later and an explicitly supplied disposable PostgreSQL 16 or 17 database. The role needs database CREATE permission and exclusive ownership of ForgeSchema metadata. Pause external DDL. This automated demonstration prints its review and executes without an interactive approval pause.

For the included local fixture, install Docker with Compose:

```sh
git clone https://github.com/ForgeSchema/forgeschema.git
cd forgeschema
docker compose up -d --wait
export FORGESCHEMA_DEMO_DATABASE_URL='postgres://forgeschema:local_test_only@localhost:55432/forgeschema_test?sslmode=disable'
sh scripts/demo.sh --output demo-output
```

Alternatively supply your own disposable database URL in that explicit environment variable. The harness never falls back to DATABASE_URL. The script builds the real CLI and runs it as a child process. No psql installation is needed. Output must be a new directory; existing files are never overwritten.

The harness creates a randomly named isolated schema with two synthetic customers. It inspects the schema, generates a desired model and explicit renames, saves JSON and readable plans, applies six operations, and independently verifies a match. The operations create an events table with its primary key/index, rename two customer columns, add a nullable notes column and create its index. It compares both customer rows before and after, including a NULL email.

It then demonstrates stale-plan rejection (CLI exit 1) and injects one extra column to demonstrate structural drift (verify exit 4). Inspection before and after verification must agree: verification performs no repair. The harness removes its injected fixture column and verifies the target again. It checks exactly one matching history record with six operations.

By default cleanup drops only the newly created demo schema; migration history remains in forgeschema_meta. It never resets the public fixture or an existing application schema. Direct fixture creation, drift injection and cleanup SQL belong to the harness, not the supported migration operation set. Use --keep-schema to retain the successful final target for manual inspection; on failure consult run.json and transcript.txt for cleanup status. Cleanup can fail after connection loss.

```sh
sh scripts/demo.sh --output another-new-directory --keep-schema
# After finishing with the disposable Compose database:
docker compose down
```

Compose has no persistent database volume, so down discards its database and retained history. For another database, remove retained demo schemas/history only through your own reviewed cleanup procedure. Never run privileged integration tests or this fixture demonstration against production.

## Artifacts

run.json records server/CLI versions, schema, plan and migration IDs, six operations, two preserved rows, expected negative exits and cleanup. reviewed-plan.json and review.txt are the saved review; current.json, desired.json and renames.yaml are its inputs. apply-result.json and verify-*.json are real CLI results. before-rows.json and after-rows.json demonstrate preservation. before-drift-check.json and after-drift-check.json demonstrate that verify did not repair drift. replay-error.txt and transcript.txt record the negative case and commands.

GitHub Actions runs this script from fresh checkouts on PostgreSQL 16.10 and 17.6 and uploads these text/JSON artifacts. Committed examples/walkthrough evidence captures actual runs; random schema names, IDs and fingerprints vary. Those captured plans describe their original database state and are evidence, not plans to replay against your database. Generate a fresh plan for each run.

Read COMMANDS.md, FEATURES.md and APPLY.md before adapting this example. A successful walkthrough demonstrates the tested contract; it does not establish production readiness, general crash recovery or continued absence of future drift.
