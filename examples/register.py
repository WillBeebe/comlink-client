"""Register one stable local identity; print only its public number."""
import argparse
from pathlib import Path
from handset import Handset
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--state', type=Path, default=Path.home()/'.local/share/comlink')
p.add_argument('--binary', default=str(Path.home()/'.local/bin/comlink'))
p.add_argument('--register', action='store_true', help='explicitly create/reuse one lifetime identity')
a=p.parse_args()
if not a.register: p.error('pass --register to create/reuse a number')
h=Handset(a.binary,a.state/'handset.json',str(a.state/'roots.json'),'https://api.comlink.fyi/mcp','')
try: print(h.register())
finally: h.close()
