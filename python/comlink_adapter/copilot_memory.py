"""Bounded, per-worker virtual session filesystem. No host filesystem operations."""
import errno
import posixpath
from datetime import datetime,timezone
from copilot.session_fs_provider import SessionFsProvider,SessionFsFileInfo
from copilot.rpc import SessionFSReaddirWithTypesEntry,SessionFSReaddirWithTypesEntryType

class BoundedMemoryFS(SessionFsProvider):
    MAX_BYTES=8*1024*1024
    MAX_ENTRIES=256
    def __init__(self):
        self.files={};self.dirs={'/','/workspace','/session-state'};self.closed=False
    def clear(self):
        self.files.clear();self.dirs.clear();self.closed=True
    def path(self,path):
        if self.closed or not isinstance(path,str) or not path.startswith('/') or len(path)>4096 or '\x00' in path:
            raise ValueError('invalid virtual path')
        return posixpath.normpath(path)
    def missing(self):raise FileNotFoundError(errno.ENOENT,'virtual entry absent')
    def limits(self,files,dirs):
        if len(files)+len(dirs)>self.MAX_ENTRIES or sum(len(v.encode()) for v in files.values())>self.MAX_BYTES:
            raise ValueError('virtual session capacity exceeded')
    async def read_file(self,path):
        path=self.path(path)
        if path not in self.files:self.missing()
        return self.files[path]
    async def write_file(self,path,content,mode=None):
        path=self.path(path)
        if path in self.dirs or not isinstance(content,str):raise ValueError('invalid virtual file')
        dirs=set(self.dirs);parent=posixpath.dirname(path)
        while parent not in dirs:
            if parent in self.files:raise ValueError('file is not directory')
            dirs.add(parent);parent=posixpath.dirname(parent)
        files=dict(self.files);files[path]=content;self.limits(files,dirs)
        self.files,self.dirs=files,dirs
    async def append_file(self,path,content,mode=None):
        path=self.path(path)
        await self.write_file(path,self.files.get(path,'')+content,mode)
    async def exists(self,path):
        path=self.path(path);return path in self.files or path in self.dirs
    async def stat(self,path):
        path=self.path(path)
        if not await self.exists(path):self.missing()
        now=datetime.now(timezone.utc)
        return SessionFsFileInfo(path in self.files,path in self.dirs,len(self.files.get(path,'').encode()),now,now)
    async def mkdir(self,path,recursive=False,mode=None):
        path=self.path(path)
        if path in self.files:raise ValueError('file is not directory')
        dirs=set(self.dirs)
        while path not in dirs:
            if path in self.files:raise ValueError('file is not directory')
            dirs.add(path);path=posixpath.dirname(path)
            if not recursive and path not in dirs:self.missing()
        self.limits(self.files,dirs);self.dirs=dirs
    async def readdir(self,path):
        path=self.path(path)
        if path not in self.dirs:self.missing()
        return sorted(posixpath.basename(p) for p in self.dirs|self.files.keys() if p!=path and posixpath.dirname(p)==path)
    async def readdir_with_types(self,path):
        path=self.path(path)
        return [SessionFSReaddirWithTypesEntry(name=name,type=SessionFSReaddirWithTypesEntryType('directory' if posixpath.join(path,name) in self.dirs else 'file')) for name in await self.readdir(path)]
    async def rm(self,path,recursive=False,force=False):
        path=self.path(path)
        if path=='/':raise ValueError('virtual root removal refused')
        if not await self.exists(path):
            if force:return
            self.missing()
        prefix=path+'/'
        if not recursive and any(p.startswith(prefix) for p in self.dirs|self.files.keys()):raise ValueError('directory not empty')
        self.files={p:v for p,v in self.files.items() if p!=path and not p.startswith(prefix)}
        self.dirs={p for p in self.dirs if p!=path and not p.startswith(prefix)}
    async def rename(self,src,dest):
        src=self.path(src);dest=self.path(dest)
        if src==dest:
            if not await self.exists(src):self.missing()
            return
        if src not in self.files or dest in self.dirs:raise ValueError('only virtual file rename supported')
        content=self.files[src];await self.write_file(dest,content);self.files.pop(src,None)
