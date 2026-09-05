#!/usr/bin/env python3
"""Exercise an existing public exchange with two fresh, local MCP handsets.

Programmed synthetic clients, not a model proof. Creates two subscriber records,
sets their owner policies, and revokes both on completion. Private profiles and
speech remain in a temporary private directory/RAM. Incomplete cleanup preserves
the private profiles for recovery. Evidence contains hashes, counts, pass/fail
and any recovery directory path. Does not configure a server or call a provider.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import queue
import secrets
import shutil
import subprocess
import tempfile
import threading
import time
import traceback
from urllib.parse import urlsplit


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
        if names != {"register", "whois", "dial", "answer", "reject", "say", "hangup"}:
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


def require(value):
    if not value:
        raise RuntimeError("public acceptance condition failed")


def refused(function):
    try:
        function()
    except Refused:
        return
    raise RuntimeError("expected refusal missing")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--roots", type=Path, required=True)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--ca", default="")
    parser.add_argument("--evidence", type=Path, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    endpoint = urlsplit(args.endpoint)
    require(endpoint.path == "/mcp" and not endpoint.query and not endpoint.fragment
            and endpoint.username is None and endpoint.hostname
            and (endpoint.scheme == "https" or
                 endpoint.scheme == "http" and endpoint.hostname in ("127.0.0.1", "::1")))
    binary, roots = str(args.binary.resolve()), args.roots.resolve()
    require(Path(binary).is_file() and os.access(binary, os.X_OK) and roots.is_file())
    # Reserve evidence before enrolling; never overwrite another run's outcome.
    evidence_file = args.evidence.open("x")
    evidence = {"passed": False, "model_backed": False, "provider_requests": 0,
                "started_at": datetime.now(timezone.utc).isoformat(),
                "endpoint": args.endpoint, "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                "roots_sha256": hashlib.sha256(roots.read_bytes()).hexdigest(),
                "steps": [], "subscribers_created": 0, "subscribers_revoked": 0}
    agents = []
    profiles = []
    revoked = set()
    private = None
    next_enrollment = 0.0
    def pace_enrollment():
        # All fixture owners share one source IP. Respect the public bootstrap
        # refill rate instead of treating an intentional flood refusal as a bug.
        nonlocal next_enrollment
        delay = next_enrollment-time.monotonic()
        if delay > 0:
            time.sleep(delay)
        next_enrollment = time.monotonic()+1.1
    def persist():
        evidence_file.seek(0)
        json.dump(evidence, evidence_file, indent=2)
        evidence_file.write("\n")
        evidence_file.truncate()
        evidence_file.flush()
        os.fsync(evidence_file.fileno())
    def step(name):
        evidence["steps"].append(name)
        persist()
        print(name, flush=True)
    def owner(command, profile, extra=()):
        pace_enrollment()
        argv = [binary, command, "--file", str(profile), "--roots", str(roots),
                "--endpoint", args.endpoint, *extra]
        if args.ca:
            argv += ["--ca", args.ca]
        result = subprocess.run(argv, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
        require(result.returncode == 0)
    def close_agents():
        for agent in reversed(agents):
            try:
                agent.close()
            except Exception:
                evidence["handset_cleanup_failed"] = True
        agents.clear()
    try:
        # Keep recovery keys private if an interrupted run cannot prove revocation.
        # The directory is removed only after all locally enrolled owners are retired.
        private = Path(tempfile.mkdtemp(prefix="comlink-public-smoke-"))
        evidence["recovery_directory"] = str(private)
        persist()
        profiles = [private / "a.json", private / "b.json"]
        def connect(profile):
            pace_enrollment()
            agent = Handset(binary, profile, roots, args.endpoint, args.ca)
            agents.append(agent)
            return agent, agent.register()
        try:
            a, number_a = connect(profiles[0])
            evidence["subscribers_created"] += 1
            b, number_b = connect(profiles[1])
            evidence["subscribers_created"] += 1
            require(number_a != number_b and len(number_a) == 128 and len(number_b) == 128)
            step("two_fresh_profiles_registered")
            refused(lambda: a.call("dial", {"to": number_b}))
            refused(lambda: b.call("dial", {"to": number_a}))
            step("incoming_disabled_by_default")
            close_agents()
            for profile, peer in zip(profiles, (number_b, number_a)):
                policy = private / "policy.json"
                policy.write_text(json.dumps({"incoming": True, "allow_unknown": False,
                                             "allowed": [peer], "blocked": []}))
                owner("policy", profile, ("--policy", str(policy)))
            a, current_a = connect(profiles[0])
            b, current_b = connect(profiles[1])
            require((current_a, current_b) == (number_a, number_b))
            step("owner_opt_in_and_stable_numbers")
            for caller, receiver, sender, peer in ((a, b, number_a, number_b), (b, a, number_b, number_a)):
                call = caller.call("dial", {"to": peer})["call"]
                require(receiver.event("ring", call)["from"] == sender)
                refused(lambda: caller.call("say", {"call": call, "text": "before answer"}))
                receiver.call("answer", {"call": call})
                require(caller.event("answer", call)["from"] == peer)
                challenge = secrets.token_hex(16)
                caller.call("say", {"call": call, "text": challenge})
                received = receiver.event("say", call)
                require(received["text"] == challenge and received["from"] == sender)
                response = challenge[::-1]
                receiver.call("say", {"call": call, "text": response})
                received = caller.event("say", call)
                require(received["text"] == response and received["from"] == peer)
                caller.call("hangup", {"call": call})
                receiver.event("closed", call)
                refused(lambda: receiver.call("say", {"call": call, "text": "after hangup"}))
            step("both_call_directions_encrypted_with_consent_and_hangup")
            close_agents()
            for profile in profiles:
                owner("revoke", profile)
                revoked.add(profile)
                evidence["subscribers_revoked"] += 1
                candidate = Handset(binary, profile, roots, args.endpoint, args.ca)
                agents.append(candidate)
                pace_enrollment()
                refused(candidate.register)
                candidate.close()
                agents.remove(candidate)
            step("revoked_profiles_cannot_register")
            evidence["passed"] = True
        finally:
            close_agents()
            # Best-effort owner revocation also handles response-loss enrollment.
            # A failed cleanup is recorded, never masked by claiming a pass.
            if evidence["subscribers_revoked"] != evidence["subscribers_created"] or not evidence["passed"]:
                evidence["cleanup_attempted"] = True
                for profile in profiles:
                    if not profile.is_file() or profile in revoked:
                        continue
                    try:
                        owner("revoke", profile)
                        revoked.add(profile)
                    except Exception:
                        evidence["cleanup_requires_review"] = True
    except Exception as error:
        evidence["passed"] = False
        evidence["error"] = "public acceptance failed; review metadata and retained server authority"
        location = traceback.extract_tb(error.__traceback__)[-1]
        evidence["failure_location"] = {"function": location.name, "line": location.lineno}
        evidence["failure_kind"] = type(error).__name__
    finally:
        if private is not None:
            if all(not profile.exists() or profile in revoked for profile in profiles):
                shutil.rmtree(private)
                evidence.pop("recovery_directory", None)
            else:
                evidence["recovery_directory"] = str(private)
                evidence["cleanup_requires_review"] = True
                evidence["passed"] = False
        evidence["finished_at"] = datetime.now(timezone.utc).isoformat()
        if evidence.get("handset_cleanup_failed"):
            evidence["passed"] = False
        persist()
        evidence_file.close()
    if not evidence["passed"]:
        raise SystemExit("Public acceptance failed; evidence contains no speech or credentials.")
    print("PASS: public programmed MCP acceptance (no model calls)")


if __name__ == "__main__":
    main()
