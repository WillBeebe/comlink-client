package agreement

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nexum/adapter"
	"nexum/nex"
)

func TestLocalProtocolWalkFailureReturnIsNotSuccess(t *testing.T) {
	a, b := openPair(t)
	id, open := openBoth(t, a, b)
	deliver(t, b, a.self, open)

	admitA := mustApply(t, a, agreementInput(id, "a-admit", "admit", "peer"))
	deliver(t, b, a.self, admitA)
	admitB := mustApply(t, b, agreementInput(id, "b-admit", "admit", "peer"))
	deliver(t, a, b.self, admitB)
	commit := mustApply(t, a, agreementInput(id, "a-commit", "commit", "peer"))
	deliver(t, b, a.self, commit)

	reportIn := agreementInput(id, "b-report", "report", "peer")
	reportIn.Outcome = string(nex.WorkFailedSafely)
	reportIn.Stage = "reproduction"
	reportIn.Reason = "the requested input was missing"
	report := mustApply(t, b, reportIn)
	deliver(t, a, b.self, report)

	resolveIn := agreementInput(id, "a-resolve", "resolve", "peer")
	resolveIn.Resolution = "reject"
	resolveIn.Reason = "no verified answer"
	resolve := mustApply(t, a, resolveIn)
	deliver(t, b, a.self, resolve)

	returnIn := agreementInput(id, "b-return", "return", "peer")
	returnIn.Uncertainty = "the missing input may exist elsewhere"
	returnIn.Assumption = "no answer was verified"
	returnIn.Disposition = "retry"
	ret := mustApply(t, b, returnIn)
	deliver(t, a, b.self, ret)

	view, work, err := a.View(id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Phase != "complete" || work || view.Resolution == nil || view.Resolution.Outcome != "reject" {
		t.Fatalf("phase %s work %v resolution %+v", view.Phase, work, view.Resolution)
	}
	peer, peerWork, err := b.View(id)
	if err != nil || peer.Head != view.Head || peerWork {
		t.Fatalf("peers diverged head %s/%s work %v err %v", view.Head, peer.Head, peerWork, err)
	}
}

func TestLostPacketReconcileResendsSameBytes(t *testing.T) {
	dir := t.TempDir()
	a := openAt(t, dir, "a")
	b := openAt(t, t.TempDir(), "b")
	id, open := openBoth(t, a, b)
	if err := b.Incoming(open, a.self); err != nil {
		t.Fatal(err)
	}
	first, firstEnv, send, err := a.Apply(agreementInput(id, "a-admit", "admit", "peer"))
	if err != nil || !send || first.Kind != adapter.ResultAgreement {
		t.Fatalf("apply %+v send %v err %v", first, send, err)
	}
	// Local commit stands. The packet outcome is unknown, so nothing is delivered.
	reopened := openAt(t, dir, "a")
	again, againEnv, err := reopened.Reconcile("a-admit", true)
	if err != nil {
		t.Fatal(err)
	}
	if again.ReceiptHead != first.ReceiptHead || !bytes.Equal(firstEnv, againEnv) {
		t.Fatal("reconcile prepared a new command or changed the receipt")
	}
	if err = b.Incoming(againEnv, a.self); err != nil {
		t.Fatal(err)
	}
	before, err := b.store.Export(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Incoming(againEnv, a.self); err != nil {
		t.Fatal(err)
	}
	after, err := b.store.Export(id)
	if err != nil || len(after.Events) != len(before.Events) || len(after.Events) != 1 {
		t.Fatalf("duplicate protocol transition: %d then %d err %v", len(before.Events), len(after.Events), err)
	}
}

func TestCrashBeforeRecordReopensOntoSameReceipt(t *testing.T) {
	dir := t.TempDir()
	a := openAt(t, dir, "a")
	b := openAt(t, t.TempDir(), "b")
	id, open := openBoth(t, a, b)
	if err := b.Incoming(open, a.self); err != nil {
		t.Fatal(err)
	}
	staged, err := a.applyWithoutRecord(agreementInput(id, "a-admit", "admit", "peer"))
	if err != nil || staged.Kind != adapter.ResultAgreement {
		t.Fatalf("unrecorded apply %+v err %v", staged, err)
	}
	rec, err := a.box.Get(a.tenant, "a-admit")
	if err != nil || rec.Status == adapter.StatusResolved {
		t.Fatalf("result was recorded before the crash: %+v err %v", rec.Status, err)
	}
	reopened := openAt(t, dir, "a")
	again, raw, err := reopened.Reconcile("a-admit", true)
	if err != nil {
		t.Fatal(err)
	}
	if again.ReceiptHead != staged.ReceiptHead {
		t.Fatalf("reopen changed receipt %s -> %s", staged.ReceiptHead, again.ReceiptHead)
	}
	bundle, err := reopened.store.Export(id)
	if err != nil || len(bundle.Events) != 1 {
		t.Fatalf("events %d err %v", len(bundle.Events), err)
	}
	if err = b.Incoming(raw, a.self); err != nil {
		t.Fatal(err)
	}
	if err = b.Incoming(raw, a.self); err != nil {
		t.Fatal(err)
	}
	peer, err := b.store.Export(id)
	if err != nil || len(peer.Events) != 1 {
		t.Fatalf("peer duplicated the command: %d err %v", len(peer.Events), err)
	}
}

func TestThreePartyWitnessIsRequired(t *testing.T) {
	a, b := openPair(t)
	w := openAt(t, t.TempDir(), "w")
	resp, raw, err := a.OpenWithWitness(b.self, w.self, "return a usable failure", "an answer or a documented defect", "research/reproducibility", "nonce-witness")
	if err != nil {
		t.Fatal(err)
	}
	deliver(t, b, a.self, raw)
	deliver(t, w, a.self, raw)
	deliver(t, b, a.self, mustApply(t, a, agreementInput(resp.AgreementID, "a-admit", "admit", "peer")))
	deliver(t, w, a.self, mustApply(t, a, agreementInput(resp.AgreementID, "a-admit", "admit", "peer")))
	deliver(t, a, b.self, mustApply(t, b, agreementInput(resp.AgreementID, "b-admit", "admit", "peer")))
	deliver(t, w, b.self, mustApply(t, b, agreementInput(resp.AgreementID, "b-admit", "admit", "peer")))
	if _, _, _, err = a.Apply(agreementInput(resp.AgreementID, "a-commit-early", "commit", "peer")); err == nil {
		t.Fatal("commit succeeded before the witness was admitted")
	}
	admitW := mustApply(t, w, agreementInput(resp.AgreementID, "w-admit", "admit", "local"))
	deliver(t, a, w.self, admitW)
	deliver(t, b, w.self, admitW)
	commit := mustApply(t, a, agreementInput(resp.AgreementID, "a-commit", "commit", "peer"))
	deliver(t, b, a.self, commit)
	deliver(t, w, a.self, commit)
	reportIn := agreementInput(resp.AgreementID, "b-report", "report", "peer")
	reportIn.Outcome = string(nex.WorkFailedSafely)
	reportIn.Stage = "reproduction"
	reportIn.Reason = "the requested input was missing"
	report := mustApply(t, b, reportIn)
	deliver(t, a, b.self, report)
	deliver(t, w, b.self, report)
	early := agreementInput(resp.AgreementID, "a-resolve-early", "resolve", "peer")
	early.Resolution = "reject"
	early.Reason = "too soon"
	if _, _, _, err = a.Apply(early); err == nil {
		t.Fatal("resolve skipped the required witness")
	}
	reviewIn := agreementInput(resp.AgreementID, "w-review", "review", "local")
	reviewIn.Assessment = "the missing input blocks any answer"
	reviewIn.Dissent = true
	review := mustApply(t, w, reviewIn)
	deliver(t, a, w.self, review)
	deliver(t, b, w.self, review)
	resolveIn := agreementInput(resp.AgreementID, "a-resolve", "resolve", "peer")
	resolveIn.Resolution = "reject"
	resolveIn.Reason = "no verified answer"
	resolve := mustApply(t, a, resolveIn)
	deliver(t, b, a.self, resolve)
	view, work, err := a.View(resp.AgreementID)
	if err != nil || work || view.Phase != "return_due" || len(view.Reviews) != 1 || !view.Reviews[0].Review.Dissent {
		t.Fatalf("witness review missing: %+v work %v err %v", view, work, err)
	}
}

func TestIncomingSpeechIsNotAnEnvelope(t *testing.T) {
	if IsEnvelope([]byte("hello from the peer")) {
		t.Fatal("speech looked like an agreement envelope")
	}
	a := openAt(t, t.TempDir(), "a")
	if err := a.Incoming([]byte("hello from the peer"), a.self); !errors.Is(err, errNotEnvelope) {
		t.Fatalf("speech entered the agreement store: %v", err)
	}
}

func TestForeignEnvelopeIsRefused(t *testing.T) {
	a, b := openPair(t)
	id, open := openBoth(t, a, b)
	if err := b.Incoming(open, a.self); err != nil {
		t.Fatal(err)
	}
	raw := mustApply(t, a, agreementInput(id, "a-admit", "admit", "peer"))
	stranger, err := nex.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := stranger.Public()
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Incoming(raw, pub); !errors.Is(err, adapter.ErrForeign) {
		t.Fatalf("foreign sender applied: %v", err)
	}
	view, err := b.store.Export(id)
	if err != nil || len(view.Events) != 0 {
		t.Fatal("foreign envelope mutated the protocol")
	}
}

func TestStorageKeyIsNotTheSigningKey(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "handset.json")
	identity, err := nex.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(profile, identity, "tenant"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(profile + ".agreement-key")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("storage key is not private")
	}
	key, err := os.ReadFile(profile + ".agreement-key")
	if err != nil || len(key) != 32 || bytes.Equal(key, identity.Signing) {
		t.Fatal("storage key reused the circuit signing key")
	}
	for _, path := range []string{profile + ".agreements", profile + ".outbox"} {
		info, err = os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("%s is not a private directory", path)
		}
	}
}

func TestSameRequestIDAcrossPeersDoesNotCollide(t *testing.T) {
	a, b := openPair(t)
	id, open := openBoth(t, a, b)
	deliver(t, b, a.self, open)

	admitA := mustApply(t, a, agreementInput(id, "admit", "admit", "peer"))
	deliver(t, b, a.self, admitA)
	admitB := mustApply(t, b, agreementInput(id, "admit", "admit", "peer"))
	deliver(t, a, b.self, admitB)

	viewA, _, err := a.View(id)
	if err != nil || len(viewA.Admitted) != 2 {
		t.Fatalf("a view %+v err %v", viewA, err)
	}
	viewB, _, err := b.View(id)
	if err != nil || viewB.Head != viewA.Head || len(viewB.Admitted) != 2 {
		t.Fatalf("b view %+v err %v", viewB, err)
	}
	dueB, err := b.Due()
	if err != nil || len(dueB) != 0 {
		t.Fatalf("peer command leaked into local outbox due list: %+v err %v", dueB, err)
	}
}

func TestDefinitiveRefusalDoesNotRemainDue(t *testing.T) {
	a, b := openPair(t)
	id, open := openBoth(t, a, b)
	deliver(t, b, a.self, open)

	if err := a.stage(agreementInput(id, "stale-admit", "admit", "peer")); err != nil {
		t.Fatal(err)
	}
	admitA := mustApply(t, a, agreementInput(id, "fresh-admit", "admit", "peer"))
	deliver(t, b, a.self, admitA)

	resp, _, err := a.Reconcile("stale-admit", true)
	if !errors.Is(err, nex.ErrProtocolStale) || resp.Kind != adapter.ResultRefused || !resp.Definitive {
		t.Fatalf("expected definitive stale refusal: %+v err %v", resp, err)
	}
	if _, _, err = a.Reconcile("stale-admit", true); !errors.Is(err, adapter.ErrDefinitive) {
		t.Fatalf("retry of definitive refusal did not return ErrDefinitive: %v", err)
	}
	due, err := a.Due()
	if err != nil || len(due) != 0 {
		t.Fatalf("definitive refusal remained due: %+v err %v", due, err)
	}
}

func TestProtocolV2ReviewedArtifactsAndIncompleteCloseout(t *testing.T) {
	a, b := openPair(t)
	if _, _, err := a.OpenAgreementV2(b.self, "review an output", "verified output", "research/v2", "v2-no-height"); !errors.Is(err, nex.ErrProtocolAuthority) {
		t.Fatalf("v2 opened without authoritative height: %v", err)
	}

	var height uint64
	a.SetHeight(func() uint64 { return height })
	bNoHeight := openAt(t, t.TempDir(), "b-no-height")
	respNoHeight, rawNoHeight, err := a.OpenAgreementV2(bNoHeight.self, "review an output", "verified output", "research/v2", "v2-peer-no-height")
	if err != nil || respNoHeight.AgreementID == "" {
		t.Fatalf("v2 open with height: %+v err %v", respNoHeight, err)
	}
	if err = bNoHeight.Incoming(rawNoHeight, a.self); !errors.Is(err, adapter.ErrForeign) {
		t.Fatalf("peer without height source accepted v2 spec: %v", err)
	}

	b.SetHeight(func() uint64 { return height })
	resp, raw, err := a.OpenAgreementV2(b.self, "review an output", "verified output", "research/v2", "v2-success")
	if err != nil {
		t.Fatal(err)
	}
	deliver(t, b, a.self, raw)

	height = 1
	deliver(t, b, a.self, mustApply(t, a, agreementInput(resp.AgreementID, "admit", "admit", "peer")))
	height = 2
	deliver(t, a, b.self, mustApply(t, b, agreementInput(resp.AgreementID, "admit", "admit", "peer")))
	height = 3
	deliver(t, b, a.self, mustApply(t, a, agreementInput(resp.AgreementID, "commit", "commit", "peer")))

	body := []byte("verified theorem counterexample")
	sum := sha256.Sum256(body)
	ref := nex.ArtifactRef{Digest: hex.EncodeToString(sum[:]), Size: uint64(len(body)), MediaType: "text/plain"}

	height = 4
	missingManifest := agreementInput(resp.AgreementID, "report-no-manifest", "report", "peer")
	missingManifest.Outcome = string(nex.WorkCompleted)
	missingManifest.Stage = "proof"
	missingManifest.Reason = "produced counterexample"
	if _, _, _, err = b.Apply(missingManifest); !errors.Is(err, nex.ErrProtocolEvidence) {
		t.Fatalf("v2 completed report without manifest succeeded: %v", err)
	}

	reportIn := agreementInput(resp.AgreementID, "report", "report", "peer")
	reportIn.Outcome = string(nex.WorkCompleted)
	reportIn.Stage = "proof"
	reportIn.Reason = "produced counterexample"
	reportIn.Method = "symbolic-check-v1"
	reportIn.Claims = []string{"counterexample checked locally"}
	reportIn.Artifacts = []nex.NamedArtifact{{Name: "answer", Ref: ref}}
	deliver(t, a, b.self, mustApply(t, b, reportIn))

	height = 5
	resolveIn := agreementInput(resp.AgreementID, "resolve", "resolve", "peer")
	resolveIn.Resolution = "accept"
	resolveIn.Reason = "verified digest and predicate"
	deliver(t, b, a.self, mustApply(t, a, resolveIn))

	height = 6
	returnIn := agreementInput(resp.AgreementID, "return", "return", "peer")
	returnIn.Uncertainty = "checked only bounded domain"
	returnIn.Assumption = "inputs fit 64-bit integers"
	returnIn.Disposition = "continue"
	deliver(t, a, b.self, mustApply(t, b, returnIn))

	view, work, err := a.View(resp.AgreementID)
	if err != nil || !work || view.Phase != "complete" || len(view.Return.Artifacts) != 1 || view.Return.Artifacts[0].Commitment != ref.Digest {
		t.Fatalf("v2 accepted return mismatch: %+v work %v err %v", view, work, err)
	}

	// Deadline closeout records incomplete obligations without claiming success.
	height = 0
	incResp, incRaw, err := a.OpenAgreementV2(b.self, "deadline closeout", "documented partial state", "research/v2", "v2-incomplete")
	if err != nil {
		t.Fatal(err)
	}
	deliver(t, b, a.self, incRaw)
	height = 1
	deliver(t, b, a.self, mustApply(t, a, agreementInput(incResp.AgreementID, "admit-inc", "admit", "peer")))

	height = 40
	closeIn := agreementInput(incResp.AgreementID, "closeout", "incomplete", "peer")
	closeIn.Reason = "peer did not admit before deadline"
	deliver(t, b, a.self, mustApply(t, a, closeIn))

	incView, incWork, err := b.View(incResp.AgreementID)
	if err != nil || incWork || incView.Phase != "incomplete" || incView.Incomplete == nil {
		t.Fatalf("incomplete closeout view %+v work %v err %v", incView, incWork, err)
	}
	if !slices.Equal(incView.Incomplete.MissingAdmissions, []string{b.self.ID()}) {
		t.Fatalf("missing admissions %+v", incView.Incomplete.MissingAdmissions)
	}
}

func openPair(t *testing.T) (*Session, *Session) {
	t.Helper()
	return openAt(t, t.TempDir(), "a"), openAt(t, t.TempDir(), "b")
}

func openAt(t *testing.T, dir, tenant string) *Session {
	t.Helper()
	identity, err := nex.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if tenant == "a" {
		saved, ok := identities[dir]
		if ok {
			identity = saved
		} else {
			if identities == nil {
				identities = map[string]nex.Identity{}
			}
			identities[dir] = identity
		}
	}
	sess, err := Open(filepath.Join(dir, "handset.json"), identity, tenant)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

var identities map[string]nex.Identity

func openBoth(t *testing.T, a, b *Session) (string, []byte) {
	t.Helper()
	resp, raw, err := a.OpenAgreement(b.self, "return a usable failure", "an answer or a documented defect", "research/reproducibility", "nonce-1")
	if err != nil || resp.Phase != "admission" || resp.Kind != adapter.ResultAgreement {
		t.Fatalf("open %+v err %v", resp, err)
	}
	again, retry, err := a.OpenAgreement(b.self, "return a usable failure", "an answer or a documented defect", "research/reproducibility", "nonce-1")
	if err != nil || again.AgreementID != resp.AgreementID || !bytes.Equal(raw, retry) {
		t.Fatalf("open retry changed the spec: %v %s", err, again.AgreementID)
	}
	return resp.AgreementID, raw
}

func agreementInput(id, request, action, dest string) ApplyInput {
	return ApplyInput{ProtocolID: id, Request: request, Action: action, Destination: dest}
}

func mustApply(t *testing.T, s *Session, in ApplyInput) []byte {
	t.Helper()
	resp, raw, _, err := s.Apply(in)
	if err != nil || resp.Kind != adapter.ResultAgreement || len(raw) == 0 {
		t.Fatalf("apply %s: %+v err %v", in.Action, resp, err)
	}
	return raw
}

func deliver(t *testing.T, to *Session, from nex.Public, raw []byte) {
	t.Helper()
	if err := to.Incoming(raw, from); err != nil {
		t.Fatal(err)
	}
}

func TestPeerAckRejectsUnknownCommandHashAndIncompatibleHeadOrPhase(t *testing.T) {
	a, b := openPair(t)
	id, openRaw := openBoth(t, a, b)
	deliver(t, b, a.self, openRaw)

	admitA := mustApply(t, a, agreementInput(id, "a-admit", "admit", "peer"))
	deliver(t, b, a.self, admitA)
	admitB := mustApply(t, b, agreementInput(id, "b-admit", "admit", "peer"))
	deliver(t, a, b.self, admitB)
	commitA := mustApply(t, a, agreementInput(id, "a-commit", "commit", "peer"))
	_ = commitA // Not yet delivered/acked by b!

	psBefore, err := a.ensureProtocolSyncLocked(id)
	if err != nil || len(psBefore.Steps) < 4 {
		t.Fatalf("unexpected steps before forged acks: len=%d err=%v", len(psBefore.Steps), err)
	}
	commitCmdHash := psBefore.Steps[len(psBefore.Steps)-1].CommandHash

	sendSignedAck := func(ack AckPayload) error {
		sig := nex.Sign(b.identity, ackSignDomain, ack)
		raw, encErr := encode(Envelope{
			Kind:         kindAck,
			ProtocolID:   id,
			Ack:          &ack,
			AckSignature: sig,
			Sender:       b.self,
			CommandHash:  ack.CommandHash,
		})
		if encErr != nil {
			t.Fatal(encErr)
		}
		return a.Incoming(raw, b.self)
	}

	// 1. Unknown command hash MUST fail closed and MUST NOT mark commitCmdHash converged.
	if err := sendSignedAck(AckPayload{
		ProtocolID:  id,
		CommandHash: nex.Hash("unknown-command"),
		ReceiptHead: id,
		Phase:       "admission",
	}); !errors.Is(err, adapter.ErrForeign) {
		t.Fatalf("expected ErrForeign for unknown command hash, got %v", err)
	}

	// 2. Ack for commitCmdHash claiming older ReceiptHead (open head) MUST fail closed.
	if err := sendSignedAck(AckPayload{
		ProtocolID:  id,
		CommandHash: commitCmdHash,
		ReceiptHead: id,
		Phase:       "admission",
	}); !errors.Is(err, adapter.ErrForeign) {
		t.Fatalf("expected ErrForeign for stale receipt head on commit ack, got %v", err)
	}

	// 3. Ack for commitCmdHash with real commit head but incompatible Phase MUST fail closed.
	viewA, _, err := a.View(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := sendSignedAck(AckPayload{
		ProtocolID:  id,
		CommandHash: commitCmdHash,
		ReceiptHead: viewA.Head,
		Phase:       "complete",
	}); !errors.Is(err, adapter.ErrForeign) {
		t.Fatalf("expected ErrForeign for incompatible phase on commit ack, got %v", err)
	}

	conv, err := a.PeerConvergence(id)
	if err != nil {
		t.Fatal(err)
	}
	if conv[b.self.ID()].Converged || conv[b.self.ID()].PendingSteps == 0 {
		t.Fatalf("forged/incompatible acks falsely marked peer converged: %+v", conv[b.self.ID()])
	}

	// 4. Deliver actual commitA and valid ack -> converges cleanly!
	ackRaw, isAck, err := b.IncomingWithAck(commitA, a.self)
	if err != nil || isAck || len(ackRaw) == 0 {
		t.Fatalf("b.IncomingWithAck failed: isAck=%v err=%v", isAck, err)
	}
	if _, gotAck, err := a.IncomingWithAck(ackRaw, b.self); err != nil || !gotAck {
		t.Fatalf("a.IncomingWithAck on valid ack failed: gotAck=%v err=%v", gotAck, err)
	}
	convAfter, err := a.PeerConvergence(id)
	if err != nil || !convAfter[b.self.ID()].Converged {
		t.Fatalf("expected peer convergence after valid ack, got %+v err=%v", convAfter[b.self.ID()], err)
	}
}

func TestPeerSyncCrashReconstructionAndCorruptionFailsClosed(t *testing.T) {
	dir := t.TempDir()
	a := openAt(t, dir, "a")
	b := openAt(t, t.TempDir(), "b")

	id, _ := openBoth(t, a, b)
	if _, err := a.applyWithoutRecord(agreementInput(id, "a-admit", "admit", "peer")); err != nil {
		t.Fatal(err)
	}

	// Simulate crash where ProtocolStore committed open + a-admit BEFORE peerSync persisted them.
	syncDir := filepath.Join(dir, "handset.json.peersync")
	entries, err := os.ReadDir(syncDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if err := os.Remove(filepath.Join(syncDir, ent.Name())); err != nil {
			t.Fatal(err)
		}
	}

	reopened := openAt(t, dir, "a")
	pending, err := reopened.PendingPeerEnvelopes(b.self.ID(), "")
	if err != nil || len(pending) != 2 {
		t.Fatalf("expected 2 reconstructed pending envelopes ([open, a-admit]), got len=%d err=%v", len(pending), err)
	}
	for _, env := range pending {
		deliver(t, b, reopened.self, env)
	}
	viewA, _, _ := reopened.View(id)
	viewB, _, err := b.View(id)
	if err != nil || viewB.Head != viewA.Head {
		t.Fatalf("reconstructed envelopes did not converge b: head %s vs %s err=%v", viewB.Head, viewA.Head, err)
	}

	// Corrupting a .sync file MUST fail closed on Open and PendingPeerEnvelopes, never appear empty/converged!
	corruptTarget := reopened.peerSync.fileForKey("sync_" + id)
	if err := os.WriteFile(corruptTarget, []byte("COMLINKPEERSYNC1corrupt-ciphertext-bytes-0123456789abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.PendingPeerEnvelopes(b.self.ID(), ""); err == nil {
		t.Fatal("PendingPeerEnvelopes succeeded despite corrupt .sync record")
	}
	if _, err := reopened.PeerConvergence(id); err == nil {
		t.Fatal("PeerConvergence succeeded despite corrupt .sync record")
	}
	if _, err := Open(filepath.Join(dir, "handset.json"), identities[dir], "a"); err == nil {
		t.Fatal("Open succeeded despite corrupt .sync record")
	}
}

func TestReconcileNonForcedExecutesOnceAndRespectsBackoff(t *testing.T) {
	a, b := openPair(t)
	id, openRaw := openBoth(t, a, b)
	deliver(t, b, a.self, openRaw)

	// 1. First non-forced reconcile of a definitive refusal must return the underlying
	// refusal error (not ErrDefinitive) and increment Attempts only once.
	if err := a.stage(agreementInput(id, "stale-nonforced", "admit", "peer")); err != nil {
		t.Fatal(err)
	}
	deliver(t, b, a.self, mustApply(t, a, agreementInput(id, "fresh-admit", "admit", "peer")))

	resp, _, err := a.Reconcile("stale-nonforced", false)
	if !errors.Is(err, nex.ErrProtocolStale) || resp.Kind != adapter.ResultRefused || !resp.Definitive {
		t.Fatalf("expected underlying definitive refusal on first non-forced reconcile: %+v err %v", resp, err)
	}
	rec, err := a.box.Get(a.tenant, "stale-nonforced")
	if err != nil || rec.Attempts != 1 || rec.Status != adapter.StatusRefused {
		t.Fatalf("non-forced reconcile ran more than once or wrong status: %+v err %v", rec, err)
	}
	if _, _, err = a.Reconcile("stale-nonforced", false); !errors.Is(err, adapter.ErrDefinitive) {
		t.Fatalf("second reconcile of definitive refusal did not return ErrDefinitive: %v", err)
	}

	// 2. Retryable refusal on non-forced reconcile runs once and respects backoff on immediate retry.
	if err := a.stage(agreementInput(id, "early-commit", "commit", "peer")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Reconcile("early-commit", false); !errors.Is(err, nex.ErrProtocolPhase) {
		t.Fatalf("expected retryable ErrProtocolPhase, got %v", err)
	}
	rec, err = a.box.Get(a.tenant, "early-commit")
	if err != nil || rec.Attempts != 1 || rec.Status != adapter.StatusUnknown {
		t.Fatalf("retryable non-forced reconcile ran %d times (status=%s) err=%v", rec.Attempts, rec.Status, err)
	}
	if _, _, err := a.Reconcile("early-commit", false); !errors.Is(err, adapter.ErrNotDue) {
		t.Fatalf("immediate non-forced retry did not honor backoff ErrNotDue: %v", err)
	}
}

func TestV2PostDeadlineExactRetryRemainsIdempotent(t *testing.T) {
	a, b := openPair(t)
	var height uint64 = 1
	a.SetHeight(func() uint64 { return height })
	b.SetHeight(func() uint64 { return height })

	resp, openRaw, err := a.OpenAgreementV2(b.self, "verify post-deadline exact retry", "accepted work", "research/v2", "v2-post-deadline-retry")
	if err != nil {
		t.Fatal(err)
	}
	deliver(t, b, a.self, openRaw)

	height = 2
	admitA := mustApply(t, a, agreementInput(resp.AgreementID, "a-admit", "admit", "peer"))
	deliver(t, b, a.self, admitA)

	// Advance host height past ReturnByHeight (40).
	height = 45
	// Exact retry of already-committed admitA must remain idempotent and produce a valid ack.
	ackRaw, isAck, err := b.IncomingWithAck(admitA, a.self)
	if err != nil || isAck || len(ackRaw) == 0 {
		t.Fatalf("exact post-deadline retry failed: isAck=%v err=%v", isAck, err)
	}
	// A brand-new uncommitted non-incomplete command after ReturnByHeight must still be refused.
	a.SetHeight(func() uint64 { return 1 })
	resp2, openRaw2, err := a.OpenAgreementV2(b.self, "uncommitted after deadline", "refused", "research/v2", "v2-uncommitted-late")
	if err != nil {
		t.Fatal(err)
	}
	deliver(t, b, a.self, openRaw2)
	a.SetHeight(func() uint64 { return 2 })
	uncommittedAdmit := mustApply(t, a, agreementInput(resp2.AgreementID, "a-admit-2", "admit", "peer"))
	if err := b.Incoming(uncommittedAdmit, a.self); !errors.Is(err, nex.ErrProtocolAuthority) {
		t.Fatalf("uncommitted non-incomplete command accepted after ReturnByHeight: %v", err)
	}
}

func TestV2ReportWithDuplicateArtifactDigests(t *testing.T) {
	a, b := openPair(t)
	var height uint64 = 1
	a.SetHeight(func() uint64 { return height })
	b.SetHeight(func() uint64 { return height })

	terms := &Terms{
		Version:          nex.ProtocolVersionV2,
		SuccessArtifacts: []string{"primary", "mirror"},
		FailureArtifacts: []string{"failure"},
	}
	resp, openRaw, err := a.OpenTerms(b.self, nil, "two slots same content", "verified outputs", "research/v2", "v2-dup-digest", terms)
	if err != nil {
		t.Fatal(err)
	}
	deliver(t, b, a.self, openRaw)
	height = 2
	deliver(t, b, a.self, mustApply(t, a, agreementInput(resp.AgreementID, "a-admit", "admit", "peer")))
	height = 3
	deliver(t, a, b.self, mustApply(t, b, agreementInput(resp.AgreementID, "b-admit", "admit", "peer")))
	height = 4
	deliver(t, b, a.self, mustApply(t, a, agreementInput(resp.AgreementID, "a-commit", "commit", "peer")))

	content := []byte("identical artifact bytes")
	sum := sha256.Sum256(content)
	ref := nex.ArtifactRef{Digest: hex.EncodeToString(sum[:]), Size: uint64(len(content)), MediaType: "text/plain"}

	height = 5
	reportIn := agreementInput(resp.AgreementID, "b-report", "report", "peer")
	reportIn.Outcome = string(nex.WorkCompleted)
	reportIn.Stage = "build"
	reportIn.Reason = "both named slots share content"
	reportIn.Artifacts = []nex.NamedArtifact{
		{Name: "primary", Ref: ref},
		{Name: "mirror", Ref: ref},
	}
	deliver(t, a, b.self, mustApply(t, b, reportIn))

	height = 6
	resolveIn := agreementInput(resp.AgreementID, "a-resolve", "resolve", "peer")
	resolveIn.Resolution = "accept"
	resolveIn.Reason = "verified"
	deliver(t, b, a.self, mustApply(t, a, resolveIn))

	height = 7
	returnIn := agreementInput(resp.AgreementID, "b-return", "return", "peer")
	returnIn.Uncertainty = "none"
	returnIn.Assumption = "bounded"
	returnIn.Disposition = "continue"
	deliver(t, a, b.self, mustApply(t, b, returnIn))

	view, work, err := a.View(resp.AgreementID)
	if err != nil || !work || view.Phase != "complete" || len(view.Return.Artifacts) != 2 {
		t.Fatalf("duplicate-digest V2 return failed: %+v work=%v err=%v", view, work, err)
	}
}

func TestDurableOutboxAdmissionGateAndStats(t *testing.T) {
	dir := t.TempDir()
	a := openAt(t, dir, "a")
	b := openAt(t, t.TempDir(), "b")

	id, openRaw := openBoth(t, a, b)
	deliver(t, b, a.self, openRaw)

	// Stage one command before sealing admission.
	if err := a.stage(agreementInput(id, "pre-seal-admit", "admit", "peer")); err != nil {
		t.Fatal(err)
	}
	if err := a.SealAdmission(2); err != nil {
		t.Fatal(err)
	}
	sealed, gen, err := a.AdmissionSealed()
	if err != nil || !sealed || gen != 2 {
		t.Fatalf("AdmissionSealed=%v gen=%d err=%v", sealed, gen, err)
	}
	// New agreement and new staged apply must fail closed with ErrAdmissionStopped.
	if _, _, err := a.OpenAgreement(b.self, "blocked", "blocked", "scope", "nonce-blocked"); !errors.Is(err, adapter.ErrAdmissionStopped) {
		t.Fatalf("new OpenAgreement succeeded while sealed: %v", err)
	}
	if _, _, _, err := a.Apply(agreementInput(id, "post-seal-admit", "admit", "peer")); !errors.Is(err, adapter.ErrAdmissionStopped) {
		t.Fatalf("new Apply succeeded while sealed: %v", err)
	}
	// Exact retry of already-opened agreement and reconciliation of already-staged command still work.
	if _, _, err := a.OpenAgreement(b.self, "return a usable failure", "an answer or a documented defect", "research/reproducibility", "nonce-1"); err != nil {
		t.Fatalf("exact OpenAgreement retry failed while sealed: %v", err)
	}

	// Reopening in a fresh handle preserves the durable seal across restart.
	reopened := openAt(t, dir, "a")
	sealed, gen, err = reopened.AdmissionSealed()
	if err != nil || !sealed || gen != 2 {
		t.Fatalf("reopened AdmissionSealed=%v gen=%d err=%v", sealed, gen, err)
	}
	if _, _, _, err := reopened.Apply(agreementInput(id, "post-restart-blocked", "admit", "peer")); !errors.Is(err, adapter.ErrAdmissionStopped) {
		t.Fatalf("reopened Apply bypassed durable seal: %v", err)
	}
	// Previously staged command still drains via Reconcile.
	resp, env, err := reopened.Reconcile("pre-seal-admit", true)
	if err != nil || resp.Kind != adapter.ResultAgreement || len(env) == 0 {
		t.Fatalf("reconcile of pre-seal staged item failed: %+v err=%v", resp, err)
	}
	// Monotonic generation check on ResumeAdmission.
	if err := reopened.ResumeAdmission(1); !errors.Is(err, adapter.ErrActivation) {
		t.Fatalf("stale ResumeAdmission generation succeeded: %v", err)
	}
	if err := reopened.ResumeAdmission(2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.OpenAgreement(b.self, "unblocked", "unblocked", "scope", "nonce-unblocked"); err != nil {
		t.Fatalf("OpenAgreement failed after ResumeAdmission: %v", err)
	}
	stats := reopened.Stats()
	if stats.CheckpointWrites == 0 || stats.DirSyncOps == 0 {
		t.Fatalf("expected nonzero ProtocolStore stats, got %+v", stats)
	}
}
