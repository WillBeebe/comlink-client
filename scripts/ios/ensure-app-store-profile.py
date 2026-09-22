#!/usr/bin/env python3
"""Create/install IOS_APP_STORE profile 'Comlink App Store Local'.

Reuses the same IOS_DISTRIBUTION certificate as Ada (id 855UVGFL9N).
Does not create a new Distribution cert. ASC app record stays web-UI-only.
"""
from __future__ import annotations

import base64
import json
import os
import plistlib
import subprocess
import sys
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path

import jwt

BUNDLE = "io.containerlabs.comlink"
PROFILE_NAME = "Comlink App Store Local"
CERT_ID = "855UVGFL9N"  # Ada / team IOS_DISTRIBUTION
ROOT = Path(__file__).resolve().parents[2]
SIGNING = ROOT / ".local" / "signing"
PROFILE_FILE = SIGNING / "Comlink-App-Store-Local.mobileprovision"
INSTALL_DIRS = [
    Path.home() / "Library/Developer/Xcode/UserData/Provisioning Profiles",
    Path.home() / "Library/MobileDevice/Provisioning Profiles",
]


def load_env() -> None:
    for p in (
        ROOT.parent / "mymesh" / ".local" / "asc.env",
        ROOT / ".local" / "asc.env",
        ROOT / ".local" / "creds.env",
        ROOT.parent.parent / "labs" / "ada-runtime" / ".local" / "asc.env",
    ):
        if not p.is_file():
            continue
        for line in p.read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            if line.startswith("export "):
                line = line[len("export ") :]
            k, _, v = line.partition("=")
            k, v = k.strip(), v.strip().strip("'").strip('"')
            if k and k not in os.environ:
                os.environ[k] = v


def key_path(kid: str) -> Path:
    for p in (
        ROOT / ".local" / f"AuthKey_{kid}.p8",
        ROOT.parent / "mymesh" / ".local" / f"AuthKey_{kid}.p8",
        ROOT.parent.parent / "labs" / "ada-runtime" / ".local" / f"AuthKey_{kid}.p8",
        Path.home() / ".appstoreconnect" / "private_keys" / f"AuthKey_{kid}.p8",
    ):
        if p.is_file():
            return p
    raise SystemExit(f"error: AuthKey_{kid}.p8 not found")


def token(p8: str, kid: str, iss: str) -> str:
    now = datetime.now(timezone.utc)
    return jwt.encode(
        {
            "iss": iss,
            "iat": now - timedelta(seconds=30),
            "exp": now + timedelta(minutes=18),
            "aud": "appstoreconnect-v1",
        },
        p8,
        algorithm="ES256",
        headers={"kid": kid, "typ": "JWT"},
    )


def req(method: str, url: str, bearer: str, body: dict | None = None):
    data = None if body is None else json.dumps(body).encode()
    r = urllib.request.Request(
        url,
        data=data,
        method=method,
        headers={"Authorization": "Bearer " + bearer, "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(r) as resp:
            raw = resp.read()
            return resp.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            parsed = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            parsed = {"raw": raw.decode("utf-8", "replace")}
        return e.code, parsed


def install_provision(blob: bytes) -> None:
    SIGNING.mkdir(parents=True, exist_ok=True)
    PROFILE_FILE.write_bytes(blob)
    decoded = subprocess.check_output(["security", "cms", "-D", "-i", str(PROFILE_FILE)])
    pl = plistlib.loads(decoded)
    uuid = pl.get("UUID")
    name = pl.get("Name")
    appid = (pl.get("Entitlements") or {}).get("application-identifier")
    if name != PROFILE_NAME:
        raise SystemExit(f"error: profile name {name!r} != {PROFILE_NAME}")
    if not str(appid).endswith("." + BUNDLE) and appid != BUNDLE:
        raise SystemExit(f"error: profile app id {appid!r}")
    for d in INSTALL_DIRS:
        d.mkdir(parents=True, exist_ok=True)
        dest = d / f"{uuid}.mobileprovision"
        dest.write_bytes(blob)
        print(f"installed {name} → {dest}")


def main() -> int:
    load_env()
    kid = os.environ.get("ASC_KEY_ID") or ""
    iss = os.environ.get("ASC_ISSUER_ID") or ""
    if not kid or not iss:
        print("error: ASC_KEY_ID / ASC_ISSUER_ID missing", file=sys.stderr)
        return 2
    bearer = token(key_path(kid).read_text(), kid, iss)

    code, payload = req(
        "GET",
        "https://api.appstoreconnect.apple.com/v1/bundleIds?limit=200&filter[platform]=IOS",
        bearer,
    )
    if code != 200:
        print(f"error: list bundleIds HTTP {code}", file=sys.stderr)
        return 1
    bid = None
    for row in payload.get("data") or []:
        if (row.get("attributes") or {}).get("identifier") == BUNDLE:
            bid = row["id"]
            break
    if not bid:
        code, payload = req("POST", "https://api.appstoreconnect.apple.com/v1/bundleIds", bearer,
            {"data": {"type": "bundleIds", "attributes": {"identifier": BUNDLE, "name": "Comlink", "platform": "IOS"}}})
        if code != 201:
            print(f"error: register bundle HTTP {code}: {payload}", file=sys.stderr)
            return 1
        bid = payload["data"]["id"]
    print(f"bundle {BUNDLE} ({bid})")

    code, payload = req(
        "GET",
        "https://api.appstoreconnect.apple.com/v1/profiles?limit=200&filter[profileType]=IOS_APP_STORE",
        bearer,
    )
    if code != 200:
        print(f"error: list profiles HTTP {code}", file=sys.stderr)
        return 1
    profile_id = None
    for row in payload.get("data") or []:
        attrs = row.get("attributes") or {}
        if attrs.get("name") == PROFILE_NAME:
            profile_id = row["id"]
            print(f"profile already exists: {PROFILE_NAME} ({profile_id})")
            content = attrs.get("profileContent")
            if content:
                install_provision(base64.b64decode(content))
                return 0
            break

    if profile_id is None:
        code, payload = req(
            "POST",
            "https://api.appstoreconnect.apple.com/v1/profiles",
            bearer,
            {
                "data": {
                    "type": "profiles",
                    "attributes": {
                        "name": PROFILE_NAME,
                        "profileType": "IOS_APP_STORE",
                    },
                    "relationships": {
                        "bundleId": {"data": {"type": "bundleIds", "id": bid}},
                        "certificates": {
                            "data": [{"type": "certificates", "id": CERT_ID}]
                        },
                    },
                }
            },
        )
        if code not in (200, 201):
            print(f"error: create profile HTTP {code}: {payload}", file=sys.stderr)
            return 1
        profile_id = payload["data"]["id"]
        content = (payload.get("data") or {}).get("attributes", {}).get("profileContent")
        print(f"created {PROFILE_NAME} ({profile_id})")
        if not content:
            print("error: create response missing profileContent", file=sys.stderr)
            return 1
        install_provision(base64.b64decode(content))
        return 0

    code, payload = req(
        "GET",
        f"https://api.appstoreconnect.apple.com/v1/profiles/{profile_id}",
        bearer,
    )
    if code != 200:
        print(f"error: get profile HTTP {code}", file=sys.stderr)
        return 1
    content = (payload.get("data") or {}).get("attributes", {}).get("profileContent")
    if not content:
        print("error: profile missing profileContent", file=sys.stderr)
        return 1
    install_provision(base64.b64decode(content))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
