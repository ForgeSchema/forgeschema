# GitHub Action

`ForgeSchema/forgeschema@v0.4.0` puts the reviewed plan where the review happens. On a pull request it inspects a database, plans from what is there to the schema file in the branch, and posts `forgeschema plan --explain` as a comment. Later runs update the same comment. A blocked plan fails the step. With `mode: verify` it checks a database against the file instead, for a job after a deploy. The action reads catalog metadata only and never applies anything; apply stays a step you run on purpose.

```yaml
name: schema
on: pull_request
permissions:
  contents: read
  pull-requests: write
jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ForgeSchema/forgeschema@v0.4.0
        with:
          database-url: ${{ secrets.STAGING_DATABASE_URL }}
          schema: db/schema.yaml
          changes: db/changes.yaml   # optional: renames, drops, conversions
```

The action downloads the release binary for the runner (Linux, macOS or Windows, x86-64 or ARM64), checks it against the release's SHA256SUMS and runs it. The release it uses is the one the action's tag names; `version: latest` or `version: vX.Y.Z` picks another.

## Inputs

| Input | Default | Meaning |
| --- | --- | --- |
| database-url | required | The database, as DATABASE_URL takes it. Pass it from a secret; the action masks it in the log and keeps it out of the comment. |
| schema | required | The schema file. Its schema field names the PostgreSQL schema to inspect. |
| changes | | A change file for the plan (renames, drops, conversions; see PLANNING.md). |
| mode | plan | plan, or verify to compare the database with the file. |
| online | false | Plan with --online. |
| comment | true | Post or update the pull request comment. |
| fail | true | Fail the step on a blocked plan or on drift. An error always fails. |
| version | the action's own | A vX.Y.Z tag or latest. |
| pull-request | the event's | The pull request to comment on. |
| comment-author | github-actions[bot] | Whose comment is updated: only a comment by this account, carrying the action's hidden marker, counts, so nobody else can take it over. |
| github-token | github.token | The token that comments. |

Only true and false count for the switches; any other value is an error, so a typo can't let a blocked plan through.

## Outputs

result is ready, no-changes, blocked, match, drift or error. exit-code is the command's exit code (see COMMANDS.md). report is the Markdown file that was posted, plan the plan JSON (plan mode), ready to hand to `forgeschema apply` in a later step. version is the release tag used (also when you asked for latest); comment-url and comment-id are the comment.

## Notes

The database should be one the branch is about to change, such as staging or a restored copy; the plan is only as current as the inspection. The comment shows the SQL, so treat a pull request comment as public as the repository. From a fork, the default token can't comment; the step warns and the result is still in the job summary. Each schema file and mode gets its own comment.

The repository's action-test workflow runs the action on every change to it against a PostgreSQL service, and again on the published binaries after each release.
