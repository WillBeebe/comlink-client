"""Exercise released binary contact tools offline, across fresh stdio sessions."""
import argparse, json, os, subprocess, tempfile
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--binary',required=True);args=p.parse_args()
class Session:
 def __init__(self,state):
  self.p=subprocess.Popen([args.binary,'mcp','--state',str(state)],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
  self.seq=0
  self.call('initialize',{'protocolVersion':'2025-03-26','capabilities':{},'clientInfo':{'name':'contacts-smoke','version':'1'}})
  self.p.stdin.write(json.dumps({'jsonrpc':'2.0','method':'notifications/initialized'})+'\n');self.p.stdin.flush()
 def call(self,method,params):
  self.seq+=1;self.p.stdin.write(json.dumps({'jsonrpc':'2.0','id':self.seq,'method':method,'params':params})+'\n');self.p.stdin.flush()
  while True:
   line=self.p.stdout.readline()
   if not line: raise RuntimeError('handset exited: '+self.p.stderr.read())
   result=json.loads(line)
   if result.get('id')==self.seq:
    assert 'error' not in result,result
    return result['result']
 def tool(self,tool_name,**arguments):
  result=self.call('tools/call',{'name':tool_name,'arguments':arguments});assert not result.get('isError'),result
  return json.loads(result['content'][0]['text'])
 def close(self):
  self.p.stdin.close()
  try:self.p.wait(timeout=5)
  except subprocess.TimeoutExpired:self.p.terminate();self.p.wait(timeout=5)
with tempfile.TemporaryDirectory() as tmp:
 state=Path(tmp)
 # Public-number fixture only; no enrollment, key generation or network calls.
 profile=state/'handset.json';profile.write_text(json.dumps({'credential':{'number':'a'*128}}));profile.chmod(0o600)
 first=Session(state)
 try:
  names={t['name'] for t in first.call('tools/list',{})['tools']}
  assert {'my_number','contacts_list','contacts_save','contacts_remove'}<=names
  assert first.tool('my_number')['number']=='a'*128
  assert first.tool('contacts_save',name='Research peer',number='b'*128,communication_approved=True)['saved']
 finally:first.close()
 second=Session(state)
 try:
  assert second.tool('contacts_list',query='research',approved_only=True)['contacts'][0]['number']=='b'*128
  assert second.tool('contacts_save',name='Research peer',number='b'*128,communication_approved=False)['saved']
  assert second.tool('contacts_list',approved_only=True)['contacts']==[]
  assert second.tool('contacts_remove',name='Research peer')['removed']
  assert second.tool('contacts_list')['contacts']==[]
 finally:second.close()
print('PASS: released binary tools, offline number, fresh-session contacts, revocation, removal')
