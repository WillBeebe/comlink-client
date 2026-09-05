import hashlib,importlib.util,json,tempfile,unittest
from pathlib import Path
spec=importlib.util.spec_from_file_location('installer',Path(__file__).resolve().parents[1]/'scripts/install.py')
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class VerifyTests(unittest.TestCase):
 def test_valid_release_and_tamper(self):
  with tempfile.TemporaryDirectory() as d:
   root=Path(d)
   for name in m.ASSETS: (root/name).write_bytes(b'fixture executable')
   (root/'THIRD_PARTY_NOTICES.txt').write_text('fixture notice')
   (root/'release.json').write_text(json.dumps({'event_version':1,'client_only_commands':True}))
   text=''.join(m.digest(p)+'  '+p.name+'\n' for p in sorted(root.iterdir()))
   (root/'SHA256SUMS').write_text(text);pin=m.digest(root/'SHA256SUMS')
   m.verify(root,pin,'comlink-linux-amd64')
   (root/'comlink-linux-amd64').write_bytes(b'tampered')
   with self.assertRaisesRegex(ValueError,'checksum'):m.verify(root,pin,'comlink-linux-amd64')
   with self.assertRaisesRegex(ValueError,'manifest'):m.verify(root,'0'*64,'comlink-linux-amd64')
 def test_bad_manifest_names(self):
  with tempfile.TemporaryDirectory() as d:
   root=Path(d);s=root/'SHA256SUMS';s.write_text('0'*64+'  ../escape\n')
   with self.assertRaises(ValueError):m.verify(root,m.digest(s),'comlink-linux-amd64')
if __name__=='__main__': unittest.main()
