#!/usr/bin/env python3
"""Install the pinned client, or explicitly select the newest non-draft beta release."""
import argparse, hashlib, json, os, platform, re, shutil, stat, subprocess, tempfile
from pathlib import Path
REPO='WillBeebe/comlink-client'
ASSETS={'comlink-linux-amd64','comlink-linux-arm64','comlink-darwin-amd64','comlink-darwin-arm64'}
def digest(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def run(*args): return subprocess.check_output(args, text=True).strip()
def verify(directory, pin, asset):
    sums=directory/'SHA256SUMS'
    if sums.stat().st_size>16384 or digest(sums)!=pin: raise ValueError('release manifest checksum mismatch')
    entries={}
    for line in sums.read_text().splitlines():
        match=re.fullmatch(r'([0-9a-f]{64})  ([a-zA-Z0-9._-]+)',line)
        if not match or match[2] in entries: raise ValueError('invalid release manifest')
        entries[match[2]]=match[1]
    if set(entries)!=ASSETS|{'release.json','THIRD_PARTY_NOTICES.txt'}: raise ValueError('unexpected release files')
    for name in (asset,'release.json','THIRD_PARTY_NOTICES.txt'):
        p=directory/name
        if not p.is_file() or p.is_symlink() or p.stat().st_size>100_000_000 or digest(p)!=entries[name]: raise ValueError('artifact checksum mismatch')
    metadata=json.loads((directory/'release.json').read_text())
    if metadata.get('event_version')!=1 or metadata.get('client_only_commands') is not True: raise ValueError('incompatible release')

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--latest',action='store_true',help='explicitly select newest non-draft release instead of repository pin')
    a=p.parse_args()
    if os.geteuid()==0: raise ValueError('install as your ordinary user, without sudo')
    system={'Linux':'linux','Darwin':'darwin'}.get(platform.system())
    arch={'x86_64':'amd64','AMD64':'amd64','arm64':'arm64','aarch64':'arm64'}.get(platform.machine())
    asset=f'comlink-{system}-{arch}'
    if asset not in ASSETS: raise ValueError('supported: Linux/macOS, Intel/ARM64')
    if not shutil.which('gh'): raise ValueError('GitHub CLI required; authenticate with gh auth login for this private beta')
    lock=json.loads((Path(__file__).resolve().parents[1]/'release-lock.json').read_text())
    if a.latest:
        releases=json.loads(run('gh','api',f'repos/{REPO}/releases?per_page=20'))
        release=next((r for r in releases if not r['draft']),None)
        if not release: raise ValueError('no release available')
        pins=re.findall(r'^Manifest-SHA256: ([0-9a-f]{64})$',release.get('body') or '',re.M)
        if len(pins)!=1: raise ValueError('release lacks one authenticated manifest pin')
        lock={'tag':release['tag_name'],'manifest_sha256':pins[0]}
    if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.]+)?',lock['tag']) or not re.fullmatch(r'[0-9a-f]{64}',lock['manifest_sha256']): raise ValueError('invalid release pin')
    home=Path.home(); directory=home/'.local/bin'
    for parent in (home,home/'.local',directory):
        if parent.is_symlink(): raise ValueError('install parent must not be a symlink')
        if parent.exists():
            s=parent.stat()
            if not stat.S_ISDIR(s.st_mode) or s.st_uid!=os.geteuid() or s.st_mode&0o022: raise ValueError('install parent has unsafe owner or permissions')
    directory.mkdir(parents=True,exist_ok=True,mode=0o700)
    target=directory/'comlink'
    if target.exists() or target.is_symlink(): raise ValueError('client already installed; stop it and move the old executable aside before upgrading; keep your profile')
    with tempfile.TemporaryDirectory(prefix='.comlink-install-',dir=directory) as temp:
        stage=Path(temp)
        subprocess.run(['gh','release','download',lock['tag'],'--repo',REPO,'--dir',str(stage),'--pattern','SHA256SUMS','--pattern',asset,'--pattern','release.json','--pattern','THIRD_PARTY_NOTICES.txt'],check=True)
        verify(stage,lock['manifest_sha256'],asset)
        (stage/asset).chmod(0o755)
        # Exclusive creation: never overwrite an existing client, even in a race.
        os.link(stage/asset,target)
        notices=directory/'comlink-THIRD_PARTY_NOTICES.txt'
        if not notices.exists():
            try: os.link(stage/'THIRD_PARTY_NOTICES.txt',notices)
            except FileExistsError: pass
    print(f'Installed {lock["tag"]}: {target}\nNext: {target} setup\nThen: {target} doctor\nNo enrollment, provider calls, or background services were started.')
if __name__=='__main__':
    try: main()
    except (ValueError,OSError,subprocess.CalledProcessError) as e:
        raise SystemExit(f'Comlink installation refused: {e}') from None
