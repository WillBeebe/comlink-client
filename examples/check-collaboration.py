#!/usr/bin/env python3
"""Check the five real Nexum examples as an isolated downstream Go module."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

EXAMPLES = Path(__file__).resolve().parent
NAMES = ('work-bounty', 'compute-lease', 'delegation', 'knowledge-exchange', 'collective-fund')
MODULE = 'github.com/WillBeebe/nexum'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--nexum-source', type=Path, help='approved clean Nexum candidate; only overrides the temporary test module')
    args = parser.parse_args()
    if not args.nexum_source and 'v0.0.0' in (EXAMPLES / 'go.mod').read_text():
        parser.error('Nexum release is not pinned yet. Supply --nexum-source for preparation or pin a published version per COLLABORATION.md.')
    env = dict(os.environ, GOWORK='off', GO111MODULE='on')
    with tempfile.TemporaryDirectory(prefix='collaboration-check-') as temp:
        root = Path(temp)
        for name in (*NAMES, 'internal/lab'):
            target = root / name
            target.mkdir(parents=True)
            for source in (EXAMPLES / name).glob('*.go'):
                shutil.copyfile(source, target / source.name)
        for name in ('go.mod', 'go.sum'):
            shutil.copyfile(EXAMPLES / name, root / name)
        if args.nexum_source:
            candidate = args.nexum_source.resolve(strict=True)
            metadata = json.loads(subprocess.check_output(['go', 'mod', 'edit', '-json'], cwd=candidate, env=env, text=True))
            if metadata['Module']['Path'] != MODULE or metadata.get('Replace'):
                parser.error('--nexum-source must be a clean public module with no replacements, not the private Nex checkout')
            subprocess.run(['go', 'mod', 'edit', '-replace', f'{MODULE}={candidate}'], cwd=root, env=env, check=True)
            with (root / 'go.sum').open('a') as out:
                out.write((candidate / 'go.sum').read_text())
        subprocess.run(['go', 'mod', 'tidy'], cwd=root, env=env, check=True)
        subprocess.run(['go', 'test', '-race', './...'], cwd=root, env=env, check=True)
        for name in NAMES:
            result = subprocess.run(['go', 'run', './' + name], cwd=root, env=env, check=True, capture_output=True, text=True)
            report, _ = json.JSONDecoder().raw_decode(result.stdout.lstrip())
            if report.get('example') != name:
                raise RuntimeError(name + ': wrong result identity')
            if name in ('work-bounty', 'compute-lease') and report.get('status') != 'accepted':
                raise RuntimeError(name + ': agreement was not accepted')
            print(name + ': PASS (real Nexum dependency)')
    print('All five examples and their race tests passed as a downstream module.')


if __name__ == '__main__':
    main()
