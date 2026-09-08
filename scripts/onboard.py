#!/usr/bin/env python3
"""One command: verified handset + private adapter environment + managed onboarding."""
import argparse
import json
import os
import shlex
from pathlib import Path
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host', choices=['opencode','codex','generic'], required=True)
    parser.add_argument('--state', type=Path)
    parser.add_argument('--receiver-config', type=Path)
    parser.add_argument('--test-number')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    if os.geteuid() == 0: raise SystemExit('Run onboarding as your ordinary user, without sudo.')
    binary = Path.home()/'.local/bin/comlink'
    # Keep executable paths stable across agent sessions. Never install into the repo.
    state = args.state or Path.home()/'.local/share/comlink'
    legacy = Path.home()/('.local/share/comlink-' + args.host)
    if args.state is None and (legacy/'handset.json').exists():
        if (state/'handset.json').exists(): raise SystemExit('Two existing identities found; select --state explicitly. No new number was created.')
        state = legacy
    if args.receiver_config is None and (state/'config.json').is_file():
        candidate = json.loads((state/'config.json').read_text())
        if {'runtime','runtime_binary','api_key_file','max_requests'} <= candidate.keys():
            args.receiver_config = state/'config.json'
    # Legacy handsets cannot participate in the new identity lease. Never displace one.
    processes=subprocess.check_output(['ps','-axo','pid=,args='],text=True)
    for line in processes.splitlines():
        fields=line.strip().split(None,1)
        if len(fields)!=2 or fields[0]==str(os.getpid()):continue
        commandline=fields[1]
        words=commandline.split()
        executable_names={Path(w).name for w in words[:2]}
        is_handset=bool(executable_names & {'comlink','comlink-coding','comlink-sdk','comlink-hermes'})
        is_bridge='-m comlink_adapter.coding' in commandline or '-m comlink_adapter.hermes' in commandline
        matches_state=str(state/'handset.json') in commandline or (args.receiver_config and str(args.receiver_config) in commandline)
        if (is_handset or is_bridge) and matches_state and not (state/'control.sock').exists():
            raise SystemExit('An existing handset is using this identity. Finish its live call and stop that legacy receiver before migration; no call was interrupted.')
    subprocess.run([sys.executable,str(root/'scripts/install.py'),'--reuse','--upgrade'],check=True)
    if state.is_symlink(): raise SystemExit('Private state must not be a symlink.')
    state.mkdir(mode=0o700,parents=True,exist_ok=True)
    if state.stat().st_mode & 0o077: raise SystemExit('Private state requires mode 0700.')
    venv = state/'adapter'
    if venv.is_symlink(): raise SystemExit('Adapter environment must not be a symlink.')
    if not (venv/'bin/python').exists(): subprocess.run([sys.executable,'-m','venv',str(venv)],check=True)
    python = str(venv/'bin/python')
    # Do not replace adapter code under an active receiver. Reuse an identical installed version.
    version = __import__('tomllib').loads((root/'pyproject.toml').read_text())['project']['version']
    installed = subprocess.run([python,'-c','from importlib.metadata import version; print(version("comlink-adapter"))'],capture_output=True,text=True)
    if installed.returncode or installed.stdout.strip()!=version:
        if (state/'control.sock').exists(): raise SystemExit('Receiver is running; finish its live call and stop it before upgrading the adapter.')
        subprocess.run([python,'-m','pip','install','--disable-pip-version-check',str(root)],check=True)
    launcher = Path.home()/'.local/bin/comlink-connect'
    launcher_text = '#!/bin/sh\nexec ' + shlex.quote(python) + ' -m comlink_adapter.onboarding --state ' + shlex.quote(str(state.absolute())) + ' "$@"\n'
    if launcher.is_symlink(): raise SystemExit('Existing comlink-connect launcher is a symlink; no file was replaced.')
    if launcher.exists():
        if launcher.read_text()!=launcher_text: raise SystemExit('Existing comlink-connect belongs to another setup; select its state explicitly.')
    else:
        fd=os.open(launcher,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o700)
        with os.fdopen(fd,'w') as f:f.write(launcher_text)
    command=[python,'-m','comlink_adapter.onboarding','onboard','--host',args.host,'--state',str(state.absolute()),'--binary',str(binary)]
    if args.receiver_config:command+=['--receiver-config',str(args.receiver_config.absolute())]
    if args.test_number:command+=['--test-number',args.test_number]
    subprocess.run(command,check=True)

if __name__=='__main__':main()
