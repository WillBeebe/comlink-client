#!/usr/bin/env python3
"""Verify standalone examples outside the client module and private checkout."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile

examples = Path(__file__).resolve().parent
names = ('work-bounty', 'compute-lease', 'delegation', 'knowledge-exchange', 'collective-fund')
with tempfile.TemporaryDirectory(prefix='collaboration-check-') as temp:
    root = Path(temp)
    env = dict(os.environ, GO111MODULE='off', GOWORK='off')
    for name in names:
        source = examples / name / 'main.go'
        dest = root / name
        dest.mkdir()
        shutil.copyfile(source, dest / 'main.go')
        subprocess.run(['go', 'vet', 'main.go'], cwd=dest, env=env, check=True)
        result = subprocess.run(['go', 'run', '-race', 'main.go'], cwd=dest,
                                env=env, check=True, capture_output=True, text=True)
        if 'PASS:' not in result.stdout or 'REFUSED:' not in result.stdout:
            raise RuntimeError(f'{name}: missing acceptance evidence')
        print(name + ': ' + result.stdout.strip().splitlines()[-1])
print('All five examples passed outside the client module, with the race detector.')
