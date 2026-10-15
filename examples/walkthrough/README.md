# Actual walkthrough evidence

These artifacts were captured from fresh-checkout GitHub Actions run https://github.com/ForgeSchema/forgeschema/actions/runs/38702626841 on PostgreSQL 16.10 and 17.6. Both runs passed: six operations, two preserved customer rows, stale replay exit 1, drift exit 4, final match and isolated-schema cleanup.

Each directory contains the actual CLI input/output and harness transcript. Random schema names, migration IDs and fingerprints belong to that run. These are evidence, not plans to apply against another database. Follow the WALKTHROUGH.md from the repository root guide to generate a new isolated run. The harness retains migration history while dropping its own schema; the disposable CI service is removed afterwards.
