# Interactive browser replay

The public demo is https://forgeschema.com/demo/, with a full-width view at https://forgeschema.com/browser-demo/. It presents the actual committed PostgreSQL 16.10 and 17.6 walkthrough artifacts. It is a static interactive replay: selecting a step or drift scenario changes the displayed evidence without contacting a database or executing SQL. No credentials, uploads, database endpoints or backend service are involved.

Visitors can compare inspected and desired tables/columns/keys/indexes, open six explained SQL operations, compare two preserved customer rows, inspect stale replay rejection and switch between matching/drift reports. Every artifact link downloads the captured original text/JSON. The notes column display comes from the harness's separate NULL assertion; before/after row files deliberately contain the original logical ID/name/email fields for equality checking.

## Source and synchronization

web/demo/index.html, demo.css and demo.js implement the replay. web/demo/data.json is generated from examples/walkthrough/postgres-16.10 and postgres-17.6. Regenerate and validate with Python 3 and Node.js 22+:

```sh
python3 scripts/export-browser-demo.py
python3 scripts/export-browser-demo.py --check
node --test web/demo/demo.test.cjs
node --check web/demo/demo.js
```

To copy into the existing Hugo website checkout:

```sh
python3 scripts/export-browser-demo.py --site /path/to/forgeschema.com
hugo --source /path/to/forgeschema.com
```

This copies four static files and both sixteen-file artifact sets into static/browser-demo/. The existing /demo/ content page embeds the replay and links to its full-width view. Theme/template/layout/CSS files are untouched. Demo styling is isolated inside its own document. Website configuration changes only its existing button/menu copy and destination.

The exporter asserts the recorded results and exact before/after inspection equality before generating data. Node tests execute the actual renderer with a DOM contract harness, covering both recordings, all four views, scenario controls, accessible selection announcements, artifact paths and the load-failure fallback. They are not a graphical browser test. A browser-control plugin was unavailable during this checkpoint, so graphical/real-browser QA was not performed. Hugo build, generated-link/asset checks and deployed HTTP asset/content checks are separate verification.

Publish source to ForgeSchema/forgeschema and copied assets/content to photozeitgeist/forgeschema.com. The existing Hugo deployment serves them. Keep run provenance and synthetic data synchronized; captured plans belong to their original random schema names and are not reusable plans for another database. ForgeSchema source and original website work are licensed under Apache License 2.0; third-party notices remain applicable. The separately authorized alpha binary release is documented in RELEASING.md; the browser replay retains its original evidence provenance.
