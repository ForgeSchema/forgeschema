#!/usr/bin/env python3
"""Build the static replay from committed CLI evidence; optionally copy to Hugo."""
import argparse
import json
import shutil
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FILES = {
    'run': 'run.json', 'current': 'current.json', 'desired': 'desired.json',
    'plan': 'reviewed-plan.json', 'before': 'before-rows.json',
    'after': 'after-rows.json', 'apply': 'apply-result.json',
    'match': 'verify-match.json', 'drift': 'verify-drift.json',
}


def build():
    runs = {}
    for version in ('16.10', '17.6'):
        source = ROOT / 'examples' / 'walkthrough' / ('postgres-' + version)
        run = {key: json.loads((source / name).read_text()) for key, name in FILES.items()}
        run['replay'] = (source / 'replay-error.txt').read_text()
        assert run['run']['status'] == 'passed'
        assert len(run['plan']['operations']) == run['apply']['operations'] == 6
        assert run['before'] == run['after'] and len(run['before']) == 2
        assert run['match']['status'] == 'match' and run['drift']['status'] == 'drift'
        assert run['drift']['differences'][0]['column'] == 'external_note'
        assert (source / 'before-drift-check.json').read_bytes() == (source / 'after-drift-check.json').read_bytes()
        runs[version] = run
    return {'format': 1, 'evidence_run': 'https://github.com/ForgeSchema/forgeschema/actions/runs/38702626841', 'runs': runs}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--site', type=Path, help='Existing Hugo checkout; copy demo to static/browser-demo')
    parser.add_argument('--check', action='store_true', help='Fail if committed data is stale; do not write')
    args = parser.parse_args()
    destination = ROOT / 'web' / 'demo'
    content = json.dumps(build(), indent=2, ensure_ascii=False) + '\n'
    if args.check:
        assert (destination / 'data.json').read_text() == content, 'Regenerate web/demo/data.json'
    else:
        (destination / 'data.json').write_text(content)
    if args.site:
        if not (args.site / 'config.toml').is_file():
            parser.error('--site must identify the existing Hugo checkout')
        target = args.site / 'static' / 'browser-demo'
        target.mkdir(parents=True, exist_ok=True)
        for name in ('index.html', 'demo.js', 'demo.css', 'data.json'):
            shutil.copyfile(destination / name, target / name)
        for version in ('16.10', '17.6'):
            source = ROOT / 'examples' / 'walkthrough' / ('postgres-' + version)
            shutil.copytree(source, target / 'artifacts' / source.name, dirs_exist_ok=True)
    print('Browser replay matches both recorded PostgreSQL runs.')


if __name__ == '__main__':
    main()
