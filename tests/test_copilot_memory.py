import importlib.util
import tempfile
import unittest
from pathlib import Path

@unittest.skipUnless(importlib.util.find_spec('copilot'), 'optional Copilot SDK is not installed')
class MemoryTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        from comlink_adapter.copilot_memory import BoundedMemoryFS
        self.fs=BoundedMemoryFS()
    async def test_no_host_writes_and_close(self):
        with tempfile.TemporaryDirectory() as d:
            p=str(Path(d)/'speech.txt')
            await self.fs.write_file(p,'synthetic private speech')
            self.assertFalse(Path(p).exists())
            self.assertEqual(await self.fs.read_file(p),'synthetic private speech')
            self.fs.clear()
            self.assertFalse(self.fs.files)
            with self.assertRaises(ValueError):await self.fs.read_file(p)
    async def test_limits_are_atomic(self):
        self.fs.MAX_BYTES=4
        await self.fs.write_file('/a','1234')
        with self.assertRaises(ValueError):await self.fs.append_file('/a','5')
        self.assertEqual(await self.fs.read_file('/a'),'1234')
        with self.assertRaises(ValueError):await self.fs.write_file('/new/dir/a','12345')
        self.assertFalse(await self.fs.exists('/new'))
        self.fs.MAX_ENTRIES=len(self.fs.files)+len(self.fs.dirs)
        with self.assertRaises(ValueError):await self.fs.mkdir('/overflow')
        self.assertFalse(await self.fs.exists('/overflow'))
    async def test_entries_unicode_rename_and_remove(self):
        await self.fs.mkdir('/a/b',recursive=True)
        await self.fs.write_file('/a/b/one','🛰')
        self.assertEqual((await self.fs.stat('/a/b/one')).size,4)
        self.assertEqual([e.name for e in await self.fs.readdir_with_types('/a/b')],['one'])
        await self.fs.rename('/a/b/one','/a/b/one')
        await self.fs.rename('/a/b/one','/a/two')
        self.assertEqual(await self.fs.read_file('/a/two'),'🛰')
        with self.assertRaises(ValueError):await self.fs.rm('/a')
        await self.fs.rm('/a',recursive=True)
        self.assertFalse(await self.fs.exists('/a/two'))
    async def test_invalid_paths_and_file_parent(self):
        for p in ('relative','/bad\x00name','/'+'a'*4096):
            with self.assertRaises(ValueError):await self.fs.write_file(p,'')
        await self.fs.write_file('/file','data')
        with self.assertRaises(ValueError):await self.fs.write_file('/file/child','data')
        with self.assertRaises(ValueError):await self.fs.rm('/',recursive=True)

if __name__=='__main__':unittest.main()
