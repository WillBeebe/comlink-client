package agreement

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
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
