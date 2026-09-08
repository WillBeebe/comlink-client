"""Minimal synchronous MCP event adapter. No model calls or transcript logging.
Single-threaded caller only; use a production host for concurrent model work.
"""
import json
import queue
import subprocess
import threading
import time

def require(ok):
    if not ok: raise RuntimeError("MCP contract refused")

class Refused(Exception):
    pass


class Handset:
    def __init__(self, binary, profile, roots, endpoint, ca):
        args = [binary, "mcp", "--enroll", "--file", str(profile),
                "--roots", str(roots), "--endpoint", endpoint]
        if ca:
            args += ["--ca", ca]
        self.process = subprocess.Popen(args, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        self.responses = queue.Queue(maxsize=64)
        self.events = []
        self.sequence = 0
        self.event_sequence = 0
        self.stopped = threading.Event()
        self.reader = threading.Thread(target=self.read, daemon=True)
        self.reader.start()

    def read(self):
        try:
            while not self.stopped.is_set():
                line = self.process.stdout.readline(65537)
                if not line or len(line) > 65536 or not line.endswith(b"\n"):
                    break
                self.responses.put(json.loads(line), timeout=2)
        except (ValueError, queue.Full, OSError):
            pass
        finally:
            self.stopped.set()

    def receive(self, deadline):
        while time.monotonic() < deadline:
            try:
                return self.responses.get(timeout=min(.2, max(.01, deadline-time.monotonic())))
            except queue.Empty:
                if self.stopped.is_set():
                    raise RuntimeError("handset stopped") from None
        raise RuntimeError("handset deadline exceeded")

    def send(self, message):
        self.process.stdin.write(json.dumps(message).encode() + b"\n")
        self.process.stdin.flush()

    def incoming(self, message):
        method = message.get("method")
        if not method:
            return False
        if method == "notifications/tools/list_changed":
            return True
        if method == "ping" and "id" in message:
            self.send({"jsonrpc": "2.0", "id": message["id"], "result": {}})
            return True
        if method != "notifications/comlink.fyi/event":
            raise RuntimeError("unexpected notification")
        event = message.get("params", {})
        if event.get("version") != 1 or event.get("seq") != self.event_sequence+1 or len(self.events) >= 32:
            raise RuntimeError("invalid event stream")
        self.event_sequence += 1
        self.events.append(event)
        return True

    def request(self, method, params):
        self.sequence += 1
        identity = self.sequence
        self.send({"jsonrpc": "2.0", "id": identity, "method": method, "params": params})
        deadline = time.monotonic()+30
        while True:
            response = self.receive(deadline)
            if self.incoming(response):
                continue
            if response.get("id") != identity:
                raise RuntimeError("unexpected response")
            if "error" in response:
                raise Refused("MCP refused")
            return response["result"]

    def register(self):
        initialized = self.request("initialize", {"protocolVersion": "2025-06-18",
                     "capabilities": {"experimental": {"comlink.fyi/events": {"version": 1}}},
                     "clientInfo": {"name": "comlink-public-acceptance", "version": "1"}})
        require(initialized.get("capabilities", {}).get("experimental", {}).get("comlink.fyi/events", {}).get("version") == 1)
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})
        names = {tool["name"] for tool in self.request("tools/list", {})["tools"]}
        core = {"register", "whois", "dial", "answer", "reject", "say", "hangup"}
        optional = {"my_number", "contacts_list", "contacts_save", "contacts_remove"}
        if not core <= names:
            raise RuntimeError("unexpected tools")
        return self.call("register", {})["number"]

    def call(self, name, arguments):
        result = self.request("tools/call", {"name": name, "arguments": arguments})
        if result.get("isError"):
            raise Refused("tool refused")
        return result.get("structuredContent") or json.loads(result["content"][0]["text"])

    def event(self, kind, call):
        deadline = time.monotonic()+25
        while True:
            for index, event in enumerate(self.events):
                if event.get("type") == kind and event.get("call") == call:
                    return self.events.pop(index)
            if not self.incoming(self.receive(deadline)):
                raise RuntimeError("unexpected event response")

    def close(self):
        if self.process.poll() is None:
            try:
                self.process.stdin.close()
                self.process.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                self.process.terminate()
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=5)
        self.stopped.set()
        self.reader.join(timeout=3)
        self.events.clear()
        self.process.stdout.close()
