import hashlib,json,os,subprocess
from pathlib import Path
root=Path(__file__).resolve().parents[1]; out=root/'dist';out.mkdir(exist_ok=True)
expected_nex='73814649998b202d2b9964a28814367d2e285e52'
actual=subprocess.check_output(['git','-C',str(root.parent/'nex'),'rev-parse','HEAD'],text=True).strip()
if actual!=expected_nex: raise RuntimeError('exact approved private Nex revision required')
if subprocess.check_output(['git','-C',str(root.parent/'nex'),'status','--porcelain'],text=True).strip(): raise RuntimeError('clean private Nex checkout required')
env=dict(os.environ,CGO_ENABLED='0',GOWORK='off',GOFLAGS='-p=2')
def run(args,**kw):return subprocess.check_output(args,cwd=root,env=env,text=True,**kw)
modules=run(['go','list','-deps','-f','{{if .Module}}{{.Module.Path}}|{{.Module.Dir}}{{end}}','./cmd/comlink'])
notices=[]
for line in sorted(set(modules.splitlines())):
 if not line or line.startswith(('comlink|','nexum|')):continue
 name,path=line.split('|',1);folder=Path(path)
 files=[p for p in folder.iterdir() if p.is_file() and p.name.upper() in ('LICENSE','LICENSE.TXT','LICENSE.MD','COPYING','NOTICE','NOTICE.TXT','AUTHORS')]
 if not files:raise RuntimeError('missing notices for '+name)
 notices.append('\n===== '+name+' =====\n'+''.join('\n'+p.name+'\n'+p.read_text() for p in sorted(files)))
(out/'THIRD_PARTY_NOTICES.txt').write_text('Third-party dependencies of the private Comlink client beta.\nNex protocol dependency remains proprietary/unpublished pending owner license choice.\n'+''.join(notices))
artifacts={}
for system in ['linux','darwin']:
 for arch in ['amd64','arm64']:
  name=f'comlink-{system}-{arch}'
  subprocess.run(['go','build','-mod=readonly','-trimpath','-buildvcs=false','-ldflags=-s -w','-o',str(out/name),'./cmd/comlink'],cwd=root,env=dict(env,GOOS=system,GOARCH=arch),check=True)
  artifacts[name]=hashlib.sha256((out/name).read_bytes()).hexdigest()
metadata={'version':'0.3.0-beta.3','event_version':1,'client_only_commands':True,'nex_revision':'73814649998b202d2b9964a28814367d2e285e52','go_version':run(['go','version']).strip(),'artifacts':artifacts,'private_beta':True,'source_build_requires_private_nex':True}
(out/'release.json').write_text(json.dumps(metadata,indent=2,sort_keys=True)+'\n')
files=sorted(p for p in out.iterdir() if p.name!='SHA256SUMS')
(out/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in files))
pin=hashlib.sha256((out/'SHA256SUMS').read_bytes()).hexdigest()
(root/'release-lock.json').write_text(json.dumps({'tag':'v0.3.0-beta.3','manifest_sha256':pin},indent=2)+'\n')
print(json.dumps({'manifest_sha256':pin,'artifacts':artifacts}))
