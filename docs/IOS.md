# Comlink for iOS

Minimal native person-to-person messaging using **Comlink numbers** (128 lowercase
letters/digits), not carrier/SMS numbers. This app implements the owner's
2026-09-16 request for a human handset. It uses the existing Comlink MCP exchange,
signed admission and encrypted circuits; it does not send messages through Ada,
choose an inference provider, or require access to the family tailnet.

## Build and run

Use authorized sibling `comlink-client` and `nex` checkouts, Go, Xcode with an iOS
SDK, and XcodeGen. Dependencies match the existing Mothers Go-mobile bridge.

```sh
./scripts/build-ios.sh
open ios/Comlink.xcodeproj
```

Select the Comlink scheme and an iPhone simulator, or choose your Apple development
team in Signing & Capabilities and run on an iPhone. The script produces ignored
`.local/ComlinkNative.xcframework` and `ios/Comlink.xcodeproj`; source and project
configuration are tracked. It builds both device and simulator slices. No signing
identity, provisioning profile, developer-team ID or private service credential is
bundled. Device installation/TestFlight distribution requires Apple signing.

## Talk to someone

1. Both people open the app and tap **Connect**. Each installation creates a
   separate stable Comlink identity and enables incoming conversation requests.
2. Share your number. In **Add a number**, enter their name and full number,
   verify it with them, and tap **Trust and add contact**.
3. Tap **Message**. Their connected app shows the authenticated caller number.
   They check the number, choose **Accept and trust**, and name the contact.
4. Exchange text. Both sides must remain connected with the app open. Every
   incoming conversation requires acceptance, even for an existing contact.
5. End the conversation to clear text. Swipe a contact to **Block** or **Trust**.
   Blocking is persisted locally, immediately disconnects an active conversation
   with that number, and rejects later rings without showing a trust prompt.

There is no offline delivery, push notification, transcript, multi-device sync,
or automatic retry of a potentially delivered message. Access expiry/network
loss requires **Connect** again; the saved number and contact trust survive.
Backgrounding or losing active app status disconnects and clears conversation
text. The app shows at most 100 messages and accepts text up to 4096 UTF-8 bytes.
The remote peer may use another compatible Comlink client; its owner must consent
and answer using that client's policy/flow.

## API and security boundary

`mobile.Client` is a small gomobile wrapper over `internal/handset.Native`:
`Open`, `Contacts`, `SaveContact`, `Dial`, `Act`, `NextEvent`, `Close`.
The native API reuses `handset.Phone`, public enrollment, profile leasing,
address-book validation and the Nex circuit implementation. It neither shells out
to a CLI nor reimplements signatures, X25519 or AES-GCM in Swift. It can also be
bound for macOS. The matching native API lives in the private service source tree
for real OTP integration tests, following the existing handset source layout.

Admission allows unknown **rings**, so a new contact can request trust without
out-of-band reciprocal allowlist setup. A ring does not authorize speech. The
native API refuses outgoing calls to untrusted contacts, refuses `answer` until
local trust is saved, and refuses `say` until the circuit is accepted. Both the
Nex state machine and native API enforce this. Existing default-deny agent
profiles and service defaults are unchanged. Use the app's own profile; importing
an existing agent's profile into this API would change its incoming-ring policy.
Blocks are enforced by the native endpoint, not published to the server policy.

The exchange still sees routing metadata, timing and sizes. Message encryption
terminates on the devices. There is no claim that trust proves who a human is,
that a peer cannot record text, or that communication approval authorizes work.
The app stores private identity files and contacts under Application Support in
a directory configured for complete iOS file protection and excluded from backup.
It does not persist message text, add analytics or log message contents. The
public issuer roots are pinned in `ios/Comlink/comlink-roots.json`, matching the
existing Mothers handset. There is no TLS bypass or user-editable issuer trust.

## Verification

The change was built for iOS/device and simulator, and tested on the iPhone 16
simulator. XCTest checks conversation cleanup, call isolation and a fast reply
arriving during answer completion. Native tests check contact identity replacement,
pre-consent speech rejection, revoked/stale event suppression and safe UI event
projection. The service integration test uses **two real native clients**, signed
admission and a local OTP exchange: trust request, explicit acceptance, encrypted
text both ways, active revocation, stable reconnect identity and persistent block.

```sh
# Client regression tests
go test ./...
go test -race ./internal/handset

# From the private comlink checkout, with its compiled codec and OTP switch:
COMLINK_TEST_CODEC="$PWD/bin/comlink" \
COMLINK_TEST_SWITCH="$PWD/exchange/comlink_switch" \
go test -race ./internal/exchange -run TestNativeTrustAndMessaging -v

# Choose an installed simulator name/ID on this Mac:
xcodebuild -project ios/Comlink.xcodeproj -scheme Comlink \
  -destination 'platform=iOS Simulator,name=iPhone 16' \
  -derivedDataPath .local/DerivedData CODE_SIGNING_ALLOWED=NO test
```

No production exchange deployment or real-device installation was performed.

## Internal TestFlight (2026-09-18)

The app uses the existing Apple team `LJCC46HF4H` and existing iOS Distribution
certificate used by Mothers/Ada. Its separate bundle is `io.containerlabs.comlink`;
its provisioning profile is `Comlink App Store Local`. Signing keys remain in the
existing private keychain, and the release script restores the prior keychain
search list after signing.

```sh
# Archive + signed IPA, without upload:
COMLINK_TF_PREPARE_ONLY=1 ./scripts/upload-ios-testflight.sh

# Upload a prepared archive (choose an unused/unconfirmed build deliberately):
COMLINK_TF_UPLOAD_ONLY=1 COMLINK_TF_BUILD=3 ./scripts/upload-ios-testflight.sh

# Future builds: archive + export + upload, with a fresh build number:
COMLINK_TF_BUILD=3 ./scripts/upload-ios-testflight.sh
```

Release 0.1.0 (2) archived, exported and uploaded successfully on 2026-09-18.
Apple returned `UPLOAD SUCCEEDED with no errors`, delivery UUID
`9f76658b-04e4-485b-bbb2-dcab424d0c69`. Apple processing is `VALID`; internal testing is `MISSING_EXPORT_COMPLIANCE`.
The owner must complete Manage Compliance for build 2 in App Store Connect before
TestFlight can distribute it. The internal group contains William Beebe and has
access to all builds. No public App Store review was submitted. Code signing
verified the existing iPhone Distribution identity and team. The signed IPA is
at `.local/testflight/0.1.0-2/export/Comlink.ipa` (ignored, not source-controlled).
Apple's API refuses creation of app records; the owner created **Comlink App**,
Apple ID `6813559001`, with bundle `io.containerlabs.comlink` and SKU `comlink-ios`.
The existing internal group is configured for all builds and contains the owner.
Do not submit for public App Store review.

Build 1 was rejected for a missing approved encryption compliance code. Build 2
omits the optional encryption declaration key so Apple can present its encryption
questionnaire. The app uses Go's standard cryptographic implementations outside
Apple's operating-system APIs. Do not copy a TLS-only app's no-encryption/exemption
declaration to bypass Apple's questions. An upload does not mean TestFlight is
ready: verify Apple's processing and compliance states before claiming availability.
The upload script checks Apple's success text as well as its exit status because
this altool version returned exit code zero after a validation rejection.

The opt-in `TestNativeDeploymentSmoke` passed against `https://api.comlink.fyi/mcp`
with the bundled issuer roots on 2026-09-18. It created two isolated identities,
verified trust gating and encrypted bidirectional text, then revoked both.
It contacted no existing subscriber or inference provider.
