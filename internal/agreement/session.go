package agreement

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"nexum/adapter"
	"nexum/nex"
)

const nonceIndexStoreKey = "__nonce_index__"

type peerEnvelopeStep struct {
	RequestID   string `json:"request_id,omitempty"`
	CommandHash string `json:"command_hash"`
	Envelope    []byte `json:"envelope"`
}

type protocolPeerSync struct {
	ProtocolID   string                           `json:"protocol_id"`
	Participants []string                         `json:"participants"`
	Steps        []peerEnvelopeStep               `json:"steps"`
	Acked        map[string]map[string]AckPayload `json:"acked"` // peerID -> commandHash -> verified AckPayload
}

// PeerSyncSummary reports durable per-peer delivery and cryptographic convergence
// state for one protocol without trusting switch_accepted or local StatusResolved.
type PeerSyncSummary struct {
	PeerID         string `json:"peer_id"`
	ConfirmedHead  string `json:"confirmed_head,omitempty"`
	ConfirmedPhase string `json:"confirmed_phase,omitempty"`
	PendingSteps   int    `json:"pending_steps"`
	Converged      bool   `json:"converged"`
}

// Session is the local Protocol store, adapter outbox, and per-destination
// agreement-convergence store beside a handset profile.
// All stores hold signed Nexum objects only. Speech and ciphertext stay out.
type Session struct {
	mu            sync.Mutex
	tenant        string
	identity      nex.Identity
	self          nex.Public
	agreementsDir string
	store         *nex.ProtocolStore
	box           *adapter.Outbox
	peerSync      *peerSyncStore
	auth          *adapter.ProtocolAuthority
}

func Open(profile string, identity nex.Identity, tenant string) (*Session, error) {
	if strings.TrimSpace(profile) == "" {
		return nil, errors.New("agreement: persistent profile required")
	}
	if err := adapterID(tenant); err != nil {
		return nil, err
	}
	self, err := identity.Public()
	if err != nil {
		return nil, err
	}
	key, err := storageKey(profile + ".agreement-key")
	if err != nil {
		return nil, err
	}
	agreementsDir := profile + ".agreements"
	store, err := nex.OpenProtocolStore(agreementsDir, key)
	if err != nil {
		return nil, err
	}
	box, err := adapter.OpenOutbox(profile+".outbox", key)
	if err != nil {
		return nil, err
	}
	peerSync, err := openPeerSyncStore(profile+".peersync", key)
	if err != nil {
		return nil, err
	}
	sess := &Session{
		tenant:        tenant,
		identity:      identity,
		self:          self,
		agreementsDir: agreementsDir,
		store:         store,
		box:           box,
		peerSync:      peerSync,
		auth:          &adapter.ProtocolAuthority{Tenant: tenant, Store: store},
	}
	sess.mu.Lock()
	err = sess.reconcileAllPeerSyncLocked()
	sess.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// SetHeight configures an authoritative host height source on the local
// ProtocolAuthority. Without it only version 1 agreements may be opened or advanced.
func (s *Session) SetHeight(fn func() uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth.Height = fn
}

// Stats returns storage telemetry from the local nex.ProtocolStore.
func (s *Session) Stats() nex.StoreStats {
	if s == nil || s.store == nil {
		return nex.StoreStats{}
	}
	return s.store.Stats()
}

// StopAdmission durably stops new agreement admissions on this endpoint's outbox
// while still allowing reconciliation of already-staged commands.
func (s *Session) StopAdmission() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.box.StopAdmission()
}

// SealAdmission durably seals new agreement admissions at generation across restarts.
func (s *Session) SealAdmission(generation uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.box.SealAdmission(generation)
}

// ResumeAdmission reopens agreement admission at or above the sealed generation.
func (s *Session) ResumeAdmission(generation uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.box.ResumeAdmission(generation)
}

// AdmissionSealed reports the durable outbox admission gate state and generation.
func (s *Session) AdmissionSealed() (bool, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.box.AdmissionSealed()
}

func (s *Session) OpenAgreement(peer nex.Public, purpose, outcome, scope, nonce string) (adapter.AuthenticatedResponse, []byte, error) {
	return s.OpenTerms(peer, nil, purpose, outcome, scope, nonce, nil)
}

func (s *Session) OpenAgreementV2(peer nex.Public, purpose, outcome, scope, nonce string) (adapter.AuthenticatedResponse, []byte, error) {
	return s.OpenTerms(peer, nil, purpose, outcome, scope, nonce, &Terms{Version: nex.ProtocolVersionV2})
}

func (s *Session) OpenWithWitness(peer, witness nex.Public, purpose, outcome, scope, nonce string) (adapter.AuthenticatedResponse, []byte, error) {
	return s.OpenTerms(peer, &witness, purpose, outcome, scope, nonce, nil)
}

func (s *Session) OpenWithWitnessV2(peer, witness nex.Public, purpose, outcome, scope, nonce string) (adapter.AuthenticatedResponse, []byte, error) {
	return s.OpenTerms(peer, &witness, purpose, outcome, scope, nonce, &Terms{Version: nex.ProtocolVersionV2})
}

// OpenTerms opens a two-party or three-party agreement with optional explicit Terms,
// enforcing exact-spec idempotency on nonce reuse and recording per-participant
// convergence obligations for all remote participants (including witness).
func (s *Session) OpenTerms(peer nex.Public, witness *nex.Public, purpose, outcome, scope, nonce string, terms *Terms) (adapter.AuthenticatedResponse, []byte, error) {
	if err := boundedText(purpose); err != nil || boundedText(outcome) != nil || boundedText(scope) != nil || boundedText(nonce) != nil {
		return adapter.AuthenticatedResponse{}, nil, adapter.ErrItem
	}
	var (
		spec nex.ProtocolSpec
		err  error
	)
	if witness != nil {
		spec, err = ThreePartyWithTerms(s.self, peer, *witness, purpose, outcome, scope, nonce, terms)
	} else {
		spec, err = TwoPartyWithTerms(s.self, peer, purpose, outcome, scope, nonce, terms)
	}
	if err != nil {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	return s.openSpec(spec)
}

func (s *Session) openSpec(spec nex.ProtocolSpec) (adapter.AuthenticatedResponse, []byte, error) {
	draft, err := nex.NewProtocol(spec)
	if err != nil {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	id := draft.View().ID
	s.mu.Lock()
	defer s.mu.Unlock()
	if spec.Version == nex.ProtocolVersionV2 && s.auth.Height == nil {
		return adapter.AuthenticatedResponse{}, nil, nex.ErrProtocolAuthority
	}
	sealed, _, gateErr := s.box.AdmissionSealed()
	if gateErr != nil {
		return adapter.AuthenticatedResponse{}, nil, gateErr
	}
	if sealed {
		if _, expErr := s.store.Export(id); errors.Is(expErr, nex.ErrNotFound) {
			return adapter.AuthenticatedResponse{}, nil, adapter.ErrAdmissionStopped
		} else if expErr != nil {
			return adapter.AuthenticatedResponse{}, nil, expErr
		}
	}
	// Detect conflicting reuse of the same nonce with altered terms/participants!
	if err := s.checkAndBindNonceLocked(spec.Nonce, id); err != nil {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	got, err := s.store.Create(spec)
	if err != nil && !errors.Is(err, nex.ErrExists) {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	if got != id {
		return adapter.AuthenticatedResponse{}, nil, adapter.ErrForeign
	}
	bundle, viewErr := s.store.Export(id)
	if viewErr != nil {
		return adapter.AuthenticatedResponse{}, nil, viewErr
	}
	if bundle.Spec.Initiator != s.self.ID() || nex.Hash(bundle.Spec) != id {
		return adapter.AuthenticatedResponse{}, nil, adapter.ErrForeign
	}
	spec = bundle.Spec
	raw, err := encode(Envelope{Kind: kindOpen, ProtocolID: id, Spec: &spec, Sender: s.self, CommandHash: id})
	if err != nil {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	if err := s.recordSyncStepLocked(id, spec.Participants, "__open__", id, raw); err != nil {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	resp, err := s.snapshot(id)
	return resp, raw, err
}

// ApplyInput is one structured protocol action. Request is stable across retries.
// A reused request never calls Prepare again; it returns the stored signed bytes.
type ApplyInput struct {
	ProtocolID  string
	Request     string
	Action      string
	Destination string
	Outcome     string
	Stage       string
	Reason      string
	Dissent     bool
	Assessment  string
	Resolution  string
	Uncertainty string
	Assumption  string
	Disposition string
	Method      string
	Claims      []string
	Inputs      []nex.NamedArtifact
	Artifacts   []nex.NamedArtifact
}

// Apply prepares a command at most once, persists the exact signed bytes, applies
// them locally, and records the verified result AND per-peer convergence obligations.
// send remains true until remote participants have confirmed convergence, even when
// local Outbox status is already StatusResolved.
func (s *Session) Apply(in ApplyInput) (resp adapter.AuthenticatedResponse, envelope []byte, send bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Action == "ack" {
		v, verr := s.store.View(in.ProtocolID)
		if verr != nil {
			return adapter.AuthenticatedResponse{}, nil, false, verr
		}
		ps, perr := s.ensureProtocolSyncLocked(in.ProtocolID)
		if perr != nil {
			return adapter.AuthenticatedResponse{}, nil, false, perr
		}
		cmdHash := v.Head
		if len(ps.Steps) > 0 {
			cmdHash = ps.Steps[len(ps.Steps)-1].CommandHash
		}
		ackEnv, aerr := s.buildSignedAckLocked(in.ProtocolID, cmdHash)
		if aerr != nil {
			return adapter.AuthenticatedResponse{}, nil, false, aerr
		}
		snap, serr := s.snapshot(in.ProtocolID)
		return snap, ackEnv, true, serr
	}
	existing, getErr := s.box.Get(s.tenant, in.Request)
	if getErr != nil && !errors.Is(getErr, adapter.ErrNotFound) {
		return adapter.AuthenticatedResponse{}, nil, false, getErr
	}
	if errors.Is(getErr, adapter.ErrNotFound) {
		if err = s.stage(in); err != nil {
			return adapter.AuthenticatedResponse{}, nil, false, err
		}
	} else {
		payload, decErr := adapter.DecodePayload(existing.Item.SignedObject)
		if decErr != nil || payload.Command == nil || existing.Item.AgreementID != in.ProtocolID || payload.Command.Action != in.Action {
			return adapter.AuthenticatedResponse{}, nil, false, adapter.ErrConflict
		}
	}
	resp, envelope, err = s.finish(in.Request, true, true)
	if err != nil {
		return resp, nil, false, err
	}
	bundle, berr := s.store.Export(in.ProtocolID)
	if berr != nil {
		return resp, nil, false, berr
	}
	rec, rerr := s.box.Get(s.tenant, in.Request)
	if rerr != nil {
		return resp, nil, false, rerr
	}
	cmdHash := adapter.CommandHash(rec.Item.SignedObject)
	if syncErr := s.recordSyncStepLocked(in.ProtocolID, bundle.Spec.Participants, in.Request, cmdHash, envelope); syncErr != nil {
		return resp, nil, false, syncErr
	}
	unconverged, convErr := s.hasUnconvergedPeerLocked(in.ProtocolID, cmdHash)
	if convErr != nil {
		return resp, nil, false, convErr
	}
	send = existing.Status != adapter.StatusResolved || unconverged
	return resp, envelope, send, nil
}

// Reconcile resubmits the stored signed bytes. It never calls Prepare.
// force ignores backoff. A crash between apply and RecordVerified is finished here.
func (s *Session) Reconcile(request string, force bool) (adapter.AuthenticatedResponse, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp, envelope, err := s.finish(request, true, force)
	if err != nil {
		return resp, nil, err
	}
	rec, rerr := s.box.Get(s.tenant, request)
	if rerr != nil {
		return resp, nil, rerr
	}
	bundle, berr := s.store.Export(rec.Item.AgreementID)
	if berr != nil {
		return resp, nil, berr
	}
	cmdHash := adapter.CommandHash(rec.Item.SignedObject)
	if syncErr := s.recordSyncStepLocked(rec.Item.AgreementID, bundle.Spec.Participants, request, cmdHash, envelope); syncErr != nil {
		return resp, nil, syncErr
	}
	return resp, envelope, nil
}

// PendingPeerEnvelopes returns causal-order exact agreement envelopes (open and apply)
// not yet cryptographically acknowledged by peerPublicID, even after local StatusResolved.
// If requestFilter is non-empty, returns pending envelopes up to and including requestFilter.
func (s *Session) PendingPeerEnvelopes(peerPublicID string, requestFilter string) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reconcileAllPeerSyncLocked(); err != nil {
		return nil, err
	}
	ids, err := s.listTrackedProtocolsLocked()
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, pid := range ids {
		ps, err := s.ensureProtocolSyncLocked(pid)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(ps.Participants, peerPublicID) {
			continue
		}
		if requestFilter != "" {
			hasReq := false
			for _, st := range ps.Steps {
				if st.RequestID == requestFilter {
					hasReq = true
					break
				}
			}
			if !hasReq {
				continue
			}
		}
		peerAcks := ps.Acked[peerPublicID]
		for _, st := range ps.Steps {
			if _, ok := peerAcks[st.CommandHash]; !ok {
				out = append(out, bytes.Clone(st.Envelope))
			}
			if requestFilter != "" && st.RequestID == requestFilter {
				break
			}
		}
	}
	return out, nil
}

// PeerConvergence summarizes per-participant convergence for protocolID.
func (s *Session) PeerConvergence(protocolID string) (map[string]PeerSyncSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, err := s.ensureProtocolSyncLocked(protocolID)
	if err != nil {
		return nil, err
	}
	res := make(map[string]PeerSyncSummary)
	for _, pid := range ps.Participants {
		if pid == s.self.ID() {
			continue
		}
		acks := ps.Acked[pid]
		pending := 0
		var lastHead, lastPhase string
		for _, st := range ps.Steps {
			if ack, ok := acks[st.CommandHash]; ok {
				lastHead = ack.ReceiptHead
				lastPhase = ack.Phase
			} else {
				pending++
			}
		}
		res[pid] = PeerSyncSummary{
			PeerID:         pid,
			ConfirmedHead:  lastHead,
			ConfirmedPhase: lastPhase,
			PendingSteps:   pending,
			Converged:      len(ps.Steps) > 0 && pending == 0,
		}
	}
	return res, nil
}

// applyWithoutRecord persists and applies the signed command but does not
// RecordVerified. Tests use it for a crash between result and recording.
func (s *Session) applyWithoutRecord(in ApplyInput) (adapter.AuthenticatedResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.box.Get(s.tenant, in.Request); errors.Is(err, adapter.ErrNotFound) {
		if err = s.stage(in); err != nil {
			return adapter.AuthenticatedResponse{}, err
		}
	} else if err != nil {
		return adapter.AuthenticatedResponse{}, err
	}
	return s.box.Reconcile(s.tenant, in.Request, s.auth)
}

func (s *Session) Due() ([]adapter.RecordedItem, error) {
	return s.box.Due(s.tenant)
}

func (s *Session) View(id string) (nex.ProtocolView, bool, error) {
	v, err := s.store.View(id)
	if err != nil {
		return nex.ProtocolView{}, false, err
	}
	return v, adapter.WorkSucceeded(v), nil
}

// ExportBundle exports the verified ProtocolBundle for independent evaluator/handoff checks.
func (s *Session) ExportBundle(id string) (nex.ProtocolBundle, error) {
	return s.store.Export(id)
}

// Incoming applies an exact envelope. A foreign agreement, tenant, or command
// is refused. Exact retry does not add a protocol transition.
func (s *Session) Incoming(plain []byte, sender nex.Public) error {
	_, _, err := s.IncomingWithAck(plain, sender)
	return err
}

// IncomingWithAck applies an incoming open, apply, or ack envelope. For valid
// open/apply envelopes, it returns a signed kindAck envelope attesting the local
// ProtocolStore's resulting head and phase so the sender can record convergence.
func (s *Session) IncomingWithAck(plain []byte, sender nex.Public) ([]byte, bool, error) {
	env, err := decode(plain)
	if err != nil {
		return nil, false, err
	}
	if env.Sender.ID() != sender.ID() {
		return nil, false, adapter.ErrForeign
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch env.Kind {
	case kindOpen:
		if err := s.incomingOpen(env, plain); err != nil {
			return nil, false, err
		}
		ackRaw, err := s.buildSignedAckLocked(env.ProtocolID, env.CommandHash)
		return ackRaw, false, err
	case kindApply:
		if err := s.incomingApply(env, plain); err != nil {
			return nil, false, err
		}
		ackRaw, err := s.buildSignedAckLocked(env.ProtocolID, env.CommandHash)
		return ackRaw, false, err
	case kindAck:
		return nil, true, s.incomingAckLocked(env)
	default:
		return nil, false, adapter.ErrForeign
	}
}

func (s *Session) incomingOpen(env Envelope, rawPlain []byte) error {
	if env.CommandHash != env.ProtocolID || env.Spec == nil {
		return adapter.ErrForeign
	}
	if env.Spec.Version == nex.ProtocolVersionV2 && s.auth.Height == nil {
		return adapter.ErrForeign
	}
	draft, err := nex.NewProtocol(*env.Spec)
	if err != nil {
		return adapter.ErrForeign
	}
	if draft.View().ID != env.ProtocolID || env.Spec.Initiator != env.Sender.ID() {
		return adapter.ErrForeign
	}
	if !participant(*env.Spec, s.self.ID()) {
		return adapter.ErrForeign
	}
	id, err := s.store.Create(*env.Spec)
	if errors.Is(err, nex.ErrExists) {
		if id != env.ProtocolID {
			return adapter.ErrForeign
		}
	} else if err != nil {
		return err
	} else if id != env.ProtocolID {
		return adapter.ErrForeign
	}
	if err := s.recordSyncStepLocked(id, env.Spec.Participants, "__open__", env.CommandHash, rawPlain); err != nil {
		return err
	}
	curView, verr := s.store.View(id)
	if verr != nil {
		return verr
	}
	return s.markPeerAckLocked(id, env.Sender.ID(), AckPayload{
		ProtocolID:  id,
		CommandHash: env.CommandHash,
		ReceiptHead: curView.Head,
		Phase:       curView.Phase,
	})
}

func (s *Session) incomingApply(env Envelope, _ []byte) error {
	payload, err := adapter.DecodePayload(env.Signed)
	if err != nil || payload.Kind != adapter.KindProtocol || payload.Command == nil {
		return adapter.ErrForeign
	}
	cmd := *payload.Command
	if adapterID(cmd.RequestID) != nil || adapter.CommandHash(env.Signed) != env.CommandHash || cmd.ProtocolID != env.ProtocolID {
		return adapter.ErrForeign
	}
	bundle, err := s.store.Export(env.ProtocolID)
	if err != nil {
		return adapter.ErrForeign
	}
	if !participant(bundle.Spec, s.self.ID()) || !participant(bundle.Spec, env.Sender.ID()) || !participant(bundle.Spec, cmd.Actor) {
		return adapter.ErrForeign
	}
	if s.auth.Height == nil {
		_, applyErr := s.store.ApplyV1(env.ProtocolID, cmd, payload.Signature, payload.Height)
		if errors.Is(applyErr, nex.ErrOperationConflict) {
			return adapter.ErrForeign
		}
		if applyErr != nil {
			return applyErr
		}
	} else {
		alreadyCommitted := false
		for _, ev := range bundle.Events {
			if ev.Command.Actor == cmd.Actor && ev.Command.RequestID == cmd.RequestID {
				alreadyCommitted = true
				break
			}
		}
		if !alreadyCommitted {
			nowHeight := s.auth.Height()
			if cmd.Height > nowHeight {
				return nex.ErrProtocolStale
			}
			if cmd.Action != "incomplete" && nowHeight >= bundle.Spec.ReturnByHeight {
				return nex.ErrProtocolAuthority
			}
		}
		_, applyErr := s.store.Apply(env.ProtocolID, cmd, payload.Signature, cmd.Height)
		if errors.Is(applyErr, nex.ErrOperationConflict) {
			return adapter.ErrForeign
		}
		if applyErr != nil {
			return applyErr
		}
	}
	curView, verr := s.store.View(env.ProtocolID)
	if verr != nil {
		return verr
	}
	relayEnv, encErr := encode(Envelope{
		Kind:        kindApply,
		ProtocolID:  env.ProtocolID,
		Signed:      env.Signed,
		Sender:      s.self,
		CommandHash: env.CommandHash,
	})
	if encErr != nil {
		return encErr
	}
	if err := s.recordSyncStepLocked(env.ProtocolID, bundle.Spec.Participants, cmd.RequestID, env.CommandHash, relayEnv); err != nil {
		return err
	}
	ack := AckPayload{
		ProtocolID:  env.ProtocolID,
		CommandHash: env.CommandHash,
		ReceiptHead: curView.Head,
		Phase:       curView.Phase,
	}
	if err := s.markPeerAckLocked(env.ProtocolID, env.Sender.ID(), ack); err != nil {
		return err
	}
	if cmd.Actor != env.Sender.ID() && cmd.Actor != s.self.ID() {
		if err := s.markPeerAckLocked(env.ProtocolID, cmd.Actor, ack); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) buildSignedAckLocked(protocolID, commandHash string) ([]byte, error) {
	v, err := s.store.View(protocolID)
	if err != nil {
		return nil, err
	}
	ack := AckPayload{
		ProtocolID:  protocolID,
		CommandHash: commandHash,
		ReceiptHead: v.Head,
		Phase:       v.Phase,
	}
	sig := nex.Sign(s.identity, ackSignDomain, ack)
	return encode(Envelope{
		Kind:         kindAck,
		ProtocolID:   protocolID,
		Ack:          &ack,
		AckSignature: sig,
		Sender:       s.self,
		CommandHash:  commandHash,
	})
}

func (s *Session) incomingAckLocked(env Envelope) error {
	bundle, err := s.store.Export(env.ProtocolID)
	if err != nil {
		return adapter.ErrForeign
	}
	if !participant(bundle.Spec, s.self.ID()) || !participant(bundle.Spec, env.Sender.ID()) {
		return adapter.ErrForeign
	}
	return s.markPeerAckLocked(env.ProtocolID, env.Sender.ID(), *env.Ack)
}

func (s *Session) checkAndBindNonceLocked(nonce, protocolID string) error {
	var index map[string]string
	raw, err := s.peerSync.Read(nonceIndexStoreKey)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if jerr := json.Unmarshal(raw, &index); jerr != nil {
			return jerr
		}
	}
	if index == nil {
		index = make(map[string]string)
	}
	if prior, exists := index[nonce]; exists && prior != protocolID {
		return adapter.ErrConflict
	}
	index[nonce] = protocolID
	encoded, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return s.peerSync.Write(nonceIndexStoreKey, encoded)
}

func (s *Session) listTrackedProtocolsLocked() ([]string, error) {
	var list []string
	raw, err := s.peerSync.Read("__protocol_list__")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if jerr := json.Unmarshal(raw, &list); jerr != nil {
			return nil, jerr
		}
	}
	return list, nil
}

func (s *Session) loadPeerSyncLocked(protocolID string) (protocolPeerSync, error) {
	raw, err := s.peerSync.Read("sync_" + protocolID)
	if err != nil {
		return protocolPeerSync{}, err
	}
	var ps protocolPeerSync
	if err := json.Unmarshal(raw, &ps); err != nil {
		return protocolPeerSync{}, err
	}
	if ps.ProtocolID != protocolID {
		return protocolPeerSync{}, errors.New("agreement: mismatched peer sync protocol ID")
	}
	if ps.Acked == nil {
		ps.Acked = make(map[string]map[string]AckPayload)
	}
	return ps, nil
}

func (s *Session) savePeerSyncLocked(ps protocolPeerSync) error {
	raw, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	if err := s.peerSync.Write("sync_"+ps.ProtocolID, raw); err != nil {
		return err
	}
	list, err := s.listTrackedProtocolsLocked()
	if err != nil {
		return err
	}
	if !slices.Contains(list, ps.ProtocolID) {
		list = append(list, ps.ProtocolID)
		lraw, merr := json.Marshal(list)
		if merr != nil {
			return merr
		}
		if werr := s.peerSync.Write("__protocol_list__", lraw); werr != nil {
			return werr
		}
	}
	return nil
}

type protocolChainStep struct {
	commandHash string
	receiptHead string
	phase       string
}

func buildProtocolChainSteps(bundle nex.ProtocolBundle) ([]protocolChainStep, error) {
	p, err := nex.NewProtocol(bundle.Spec)
	if err != nil {
		return nil, err
	}
	v0 := p.View()
	steps := make([]protocolChainStep, 0, len(bundle.Events)+1)
	steps = append(steps, protocolChainStep{
		commandHash: v0.ID,
		receiptHead: v0.Head,
		phase:       v0.Phase,
	})
	for _, ev := range bundle.Events {
		signed, err := adapter.EncodeProtocol(ev.Command, ev.Signature)
		if err != nil {
			return nil, err
		}
		if _, err := p.Apply(ev.Command, ev.Signature, ev.Command.Height); err != nil {
			return nil, err
		}
		v := p.View()
		steps = append(steps, protocolChainStep{
			commandHash: adapter.CommandHash(signed),
			receiptHead: v.Head,
			phase:       v.Phase,
		})
	}
	return steps, nil
}

// ensureProtocolSyncLocked loads sync_<protocolID>, reconstructing any missing local
// obligations from ProtocolStore if a crash occurred between ProtocolStore commit
// and peerSync persistence, while failing closed on any storage corruption.
func (s *Session) ensureProtocolSyncLocked(protocolID string) (protocolPeerSync, error) {
	ps, err := s.loadPeerSyncLocked(protocolID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return protocolPeerSync{}, err
	}
	existsOnDisk := err == nil
	bundle, berr := s.store.Export(protocolID)
	if berr != nil {
		if existsOnDisk {
			return ps, nil
		}
		return protocolPeerSync{}, berr
	}
	return s.reconstructProtocolSyncFromBundleLocked(protocolID, bundle, ps, existsOnDisk)
}

func (s *Session) reconstructProtocolSyncFromBundleLocked(protocolID string, bundle nex.ProtocolBundle, ps protocolPeerSync, existsOnDisk bool) (protocolPeerSync, error) {
	dirty := !existsOnDisk
	if !existsOnDisk {
		pids := make([]string, 0, len(bundle.Spec.Participants))
		for _, p := range bundle.Spec.Participants {
			pids = append(pids, p.ID())
		}
		ps = protocolPeerSync{
			ProtocolID:   protocolID,
			Participants: pids,
			Acked:        make(map[string]map[string]AckPayload),
		}
	}
	knownStep := make(map[string]bool, len(ps.Steps))
	for _, st := range ps.Steps {
		knownStep[st.CommandHash] = true
	}

	if !knownStep[protocolID] {
		var initPub nex.Public
		foundInit := false
		for _, p := range bundle.Spec.Participants {
			if p.ID() == bundle.Spec.Initiator {
				initPub = p
				foundInit = true
				break
			}
		}
		if foundInit {
			specCopy := bundle.Spec
			openRaw, err := encode(Envelope{
				Kind:        kindOpen,
				ProtocolID:  protocolID,
				Spec:        &specCopy,
				Sender:      initPub,
				CommandHash: protocolID,
			})
			if err != nil {
				return protocolPeerSync{}, err
			}
			ps.Steps = append([]peerEnvelopeStep{{
				RequestID:   "__open__",
				CommandHash: protocolID,
				Envelope:    openRaw,
			}}, ps.Steps...)
			knownStep[protocolID] = true
			if initPub.ID() != s.self.ID() {
				if ps.Acked[initPub.ID()] == nil {
					ps.Acked[initPub.ID()] = make(map[string]AckPayload)
				}
				ps.Acked[initPub.ID()][protocolID] = AckPayload{
					ProtocolID:  protocolID,
					CommandHash: protocolID,
					ReceiptHead: protocolID,
					Phase:       "admission",
				}
			}
			dirty = true
		}
	}

	chain, err := buildProtocolChainSteps(bundle)
	if err != nil {
		return protocolPeerSync{}, err
	}

	for idx, ev := range bundle.Events {
		signed, err := adapter.EncodeProtocol(ev.Command, ev.Signature)
		if err != nil {
			return protocolPeerSync{}, err
		}
		cmdHash := adapter.CommandHash(signed)
		if !knownStep[cmdHash] {
			envBytes, err := encode(Envelope{
				Kind:        kindApply,
				ProtocolID:  protocolID,
				Signed:      signed,
				Sender:      s.self,
				CommandHash: cmdHash,
			})
			if err != nil {
				return protocolPeerSync{}, err
			}
			ps.Steps = append(ps.Steps, peerEnvelopeStep{
				RequestID:   ev.Command.RequestID,
				CommandHash: cmdHash,
				Envelope:    envBytes,
			})
			knownStep[cmdHash] = true
			dirty = true
		}
		if ev.Command.Actor != s.self.ID() && idx+1 < len(chain) {
			actorAck := AckPayload{
				ProtocolID:  protocolID,
				CommandHash: cmdHash,
				ReceiptHead: chain[idx+1].receiptHead,
				Phase:       chain[idx+1].phase,
			}
			if ps.Acked[ev.Command.Actor] == nil {
				ps.Acked[ev.Command.Actor] = make(map[string]AckPayload)
			}
			for _, st := range ps.Steps {
				if _, already := ps.Acked[ev.Command.Actor][st.CommandHash]; !already {
					ps.Acked[ev.Command.Actor][st.CommandHash] = actorAck
					dirty = true
				}
				if st.CommandHash == cmdHash {
					break
				}
			}
		}
	}

	if bundle.Spec.Initiator == s.self.ID() {
		if err := s.checkAndBindNonceLocked(bundle.Spec.Nonce, protocolID); err != nil {
			return protocolPeerSync{}, err
		}
	}
	if dirty {
		if err := s.savePeerSyncLocked(ps); err != nil {
			return protocolPeerSync{}, err
		}
	}
	return ps, nil
}

func (s *Session) reconcileAllPeerSyncLocked() error {
	// 1. Validate nonce index if present.
	if raw, err := s.peerSync.Read(nonceIndexStoreKey); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else if err == nil {
		var idx map[string]string
		if jerr := json.Unmarshal(raw, &idx); jerr != nil {
			return jerr
		}
	}
	// 2. Collect protocols from __protocol_list__ and agreementsDir/*.nex.
	tracked, err := s.listTrackedProtocolsLocked()
	if err != nil {
		return err
	}
	pidSet := make(map[string]bool, len(tracked))
	for _, pid := range tracked {
		pidSet[pid] = true
	}
	entries, err := os.ReadDir(s.agreementsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".nex") {
			continue
		}
		pid := strings.TrimSuffix(name, ".nex")
		if rawID, herr := hex.DecodeString(pid); herr == nil && len(rawID) == 32 {
			pidSet[pid] = true
		}
	}
	for pid := range pidSet {
		if _, err := s.ensureProtocolSyncLocked(pid); err != nil {
			return err
		}
	}
	// 3. Ensure no orphaned/corrupt .sync file exists in peerSync.dir that escaped index lookup.
	expectedFiles := map[string]bool{
		filepath.Base(s.peerSync.fileForKey(nonceIndexStoreKey)):  true,
		filepath.Base(s.peerSync.fileForKey("__protocol_list__")): true,
	}
	finalTracked, err := s.listTrackedProtocolsLocked()
	if err != nil {
		return err
	}
	for _, pid := range finalTracked {
		expectedFiles[filepath.Base(s.peerSync.fileForKey("sync_"+pid))] = true
	}
	syncEntries, err := os.ReadDir(s.peerSync.dir)
	if err != nil {
		return err
	}
	for _, ent := range syncEntries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".sync") {
			continue
		}
		if !expectedFiles[name] {
			return errors.New("agreement: unindexed or corrupt peer sync record")
		}
	}
	return nil
}

func (s *Session) recordSyncStepLocked(protocolID string, participants []nex.Public, requestID, cmdHash string, envBytes []byte) error {
	ps, err := s.loadPeerSyncLocked(protocolID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		pids := make([]string, 0, len(participants))
		for _, p := range participants {
			pids = append(pids, p.ID())
		}
		ps = protocolPeerSync{
			ProtocolID:   protocolID,
			Participants: pids,
			Acked:        make(map[string]map[string]AckPayload),
		}
	}
	for _, st := range ps.Steps {
		if st.CommandHash == cmdHash {
			return nil
		}
	}
	ps.Steps = append(ps.Steps, peerEnvelopeStep{
		RequestID:   requestID,
		CommandHash: cmdHash,
		Envelope:    bytes.Clone(envBytes),
	})
	return s.savePeerSyncLocked(ps)
}

func (s *Session) markPeerAckLocked(protocolID, peerID string, ack AckPayload) error {
	if peerID == s.self.ID() {
		return nil
	}
	if ack.ProtocolID != protocolID || ack.CommandHash == "" || ack.ReceiptHead == "" || ack.Phase == "" {
		return adapter.ErrForeign
	}
	ps, err := s.ensureProtocolSyncLocked(protocolID)
	if err != nil {
		return err
	}
	if !slices.Contains(ps.Participants, peerID) {
		return adapter.ErrForeign
	}
	stepIdx := -1
	for i, st := range ps.Steps {
		if st.CommandHash == ack.CommandHash {
			stepIdx = i
			break
		}
	}
	if stepIdx < 0 {
		return adapter.ErrForeign
	}

	bundle, err := s.store.Export(protocolID)
	if err != nil {
		return err
	}
	chain, err := buildProtocolChainSteps(bundle)
	if err != nil {
		return err
	}
	cmdChainIdx := -1
	headChainIdx := -1
	for i, st := range chain {
		if st.commandHash == ack.CommandHash {
			cmdChainIdx = i
		}
		if st.receiptHead == ack.ReceiptHead {
			headChainIdx = i
		}
	}
	if cmdChainIdx < 0 || headChainIdx < cmdChainIdx || chain[headChainIdx].phase != ack.Phase {
		return adapter.ErrForeign
	}

	if ps.Acked[peerID] == nil {
		ps.Acked[peerID] = make(map[string]AckPayload)
	}
	// Because Nexum Protocol commands form a strict hash chain (c.Head == prior view.Head),
	// a verified command or ack at CommandHash with compatible ReceiptHead & Phase proves
	// the peer has applied every prior step up to stepIdx!
	for i := 0; i <= stepIdx; i++ {
		ps.Acked[peerID][ps.Steps[i].CommandHash] = ack
	}
	return s.savePeerSyncLocked(ps)
}

func (s *Session) hasUnconvergedPeerLocked(protocolID, cmdHash string) (bool, error) {
	ps, err := s.ensureProtocolSyncLocked(protocolID)
	if err != nil {
		return true, err
	}
	for _, pid := range ps.Participants {
		if pid == s.self.ID() {
			continue
		}
		if _, ok := ps.Acked[pid][cmdHash]; !ok {
			return true, nil
		}
	}
	return false, nil
}

func (s *Session) stage(in ApplyInput) error {
	if err := adapterID(in.Request); err != nil || adapterID(in.ProtocolID) != nil || adapterID(in.Destination) != nil {
		return adapter.ErrItem
	}
	for _, field := range []string{in.Stage, in.Reason, in.Assessment, in.Uncertainty, in.Assumption, in.Disposition, in.Outcome, in.Resolution, in.Method} {
		if field != "" && boundedText(field) != nil {
			return adapter.ErrItem
		}
	}
	for _, claim := range in.Claims {
		if boundedText(claim) != nil {
			return adapter.ErrItem
		}
	}
	for _, list := range [][]nex.NamedArtifact{in.Inputs, in.Artifacts} {
		for _, a := range list {
			if boundedText(a.Name) != nil {
				return adapter.ErrItem
			}
		}
	}
	bundle, err := s.store.Export(in.ProtocolID)
	if err != nil {
		return err
	}
	view, err := s.store.View(in.ProtocolID)
	if err != nil {
		return err
	}
	grant, scope, err := roleFor(bundle.Spec, s.self.ID(), in.Action)
	if err != nil {
		return err
	}
	height := view.Height + 1
	if s.auth.Height != nil {
		height = s.auth.Height()
	}
	cmd, err := s.store.Prepare(in.ProtocolID, s.self.ID(), grant, scope, in.Action, in.Request, height)
	if err != nil {
		return err
	}
	if err = fillCommand(&cmd, view, bundle.Spec, in); err != nil {
		return err
	}
	signed, err := adapter.EncodeProtocol(cmd, nex.Sign(s.identity, "protocol", cmd))
	if err != nil {
		return err
	}
	return s.box.Enqueue(adapter.OutboundItem{
		Version:      adapter.Version,
		Tenant:       s.tenant,
		AgreementID:  in.ProtocolID,
		OperationID:  in.Request,
		Kind:         adapter.KindProtocol,
		SignedObject: signed,
		Destination:  in.Destination,
	})
}

func (s *Session) finish(request string, record, force bool) (adapter.AuthenticatedResponse, []byte, error) {
	var (
		resp adapter.AuthenticatedResponse
		err  error
	)
	if force {
		resp, err = s.box.Reconcile(s.tenant, request, s.auth)
	} else {
		resp, err = s.box.ReconcileDue(s.tenant, request, s.auth)
	}
	if errors.Is(err, adapter.ErrNotDue) {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	rec, getErr := s.box.Get(s.tenant, request)
	if getErr != nil {
		if err != nil {
			return resp, nil, err
		}
		return resp, nil, getErr
	}
	payload, decErr := adapter.DecodePayload(rec.Item.SignedObject)
	if decErr != nil || payload.Command == nil || payload.Command.Actor != s.self.ID() {
		return resp, nil, adapter.ErrForeign
	}
	raw, encErr := encode(Envelope{
		Kind:        kindApply,
		ProtocolID:  rec.Item.AgreementID,
		Signed:      rec.Item.SignedObject,
		Sender:      s.self,
		CommandHash: adapter.CommandHash(rec.Item.SignedObject),
	})
	if err != nil {
		if resp.Kind == "" && rec.Original != nil {
			resp = *rec.Original
		}
		return resp, nil, err
	}
	if encErr != nil {
		return resp, nil, encErr
	}
	if rec.Status == adapter.StatusResolved {
		if rec.Original != nil {
			resp = *rec.Original
		}
		return resp, raw, nil
	}
	if record && resp.Kind == adapter.ResultAgreement {
		if recErr := s.record(request, resp); recErr != nil {
			return resp, nil, recErr
		}
	}
	return resp, raw, nil
}

func (s *Session) record(request string, resp adapter.AuthenticatedResponse) error {
	if resp.RequestID == "" {
		resp.RequestID = request
	}
	return s.box.RecordVerified(s.tenant, request, resp, s.self, adapter.SignResponse(s.identity, resp))
}

func (s *Session) snapshot(id string) (adapter.AuthenticatedResponse, error) {
	v, err := s.store.View(id)
	if err != nil {
		return adapter.AuthenticatedResponse{}, err
	}
	resp := adapter.AuthenticatedResponse{
		Version:     adapter.Version,
		Tenant:      s.tenant,
		AgreementID: id,
		Kind:        adapter.ResultAgreement,
		ReceiptHead: v.Head,
		Phase:       v.Phase,
	}
	if v.Report != nil {
		resp.WorkOutcome = string(v.Report.Outcome)
	}
	return resp, nil
}

func fillCommand(cmd *nex.ProtocolCommand, view nex.ProtocolView, spec nex.ProtocolSpec, in ApplyInput) error {
	switch in.Action {
	case "admit", "commit":
		return nil
	case "report":
		if in.Stage == "" || in.Reason == "" || !knownOutcome(in.Outcome) || nex.WorkOutcome(in.Outcome) == nex.WorkUnreported {
			return nex.ErrProtocolEvidence
		}
		report := &nex.WorkReport{
			Outcome:    nex.WorkOutcome(in.Outcome),
			Stage:      in.Stage,
			ReasonHash: nex.Hash(in.Reason),
			Evidence:   []string{nex.Hash(in.Reason)},
		}
		if len(in.Artifacts) > 0 {
			evDigests := make([]string, 0, len(in.Artifacts))
			seen := make(map[string]bool, len(in.Artifacts))
			for _, a := range in.Artifacts {
				if !seen[a.Ref.Digest] {
					seen[a.Ref.Digest] = true
					evDigests = append(evDigests, a.Ref.Digest)
				}
			}
			report.Evidence = evDigests
		}
		if spec.Version == nex.ProtocolVersionV2 && (report.Outcome == nex.WorkCompleted || len(in.Artifacts) > 0 || len(in.Inputs) > 0 || in.Method != "" || len(in.Claims) > 0) {
			method := in.Method
			if method == "" {
				method = "method:" + in.Stage
			}
			var claims []string
			for _, claim := range in.Claims {
				claims = append(claims, nex.Hash(claim))
			}
			manifest := &nex.EvidenceManifest{
				Inputs:     append([]nex.NamedArtifact(nil), in.Inputs...),
				Outputs:    append([]nex.NamedArtifact(nil), in.Artifacts...),
				MethodHash: nex.Hash(method),
				Claims:     claims,
			}
			if err := manifest.Validate(); err != nil {
				return err
			}
			report.Manifest = manifest
		}
		cmd.Report = report
		return nil
	case "failure":
		if in.Stage == "" || in.Reason == "" {
			return nex.ErrProtocolEvidence
		}
		cmd.Report = &nex.WorkReport{
			Outcome:    nex.WorkUnreported,
			Stage:      in.Stage,
			ReasonHash: nex.Hash(in.Reason),
			Evidence:   []string{nex.Hash(in.Reason)},
		}
		return nil
	case "review":
		if view.Report == nil || in.Assessment == "" {
			return nex.ErrProtocolEvidence
		}
		cmd.Review = &nex.WitnessReview{
			ReportHash:     nex.Hash(view.Report),
			AssessmentHash: nex.Hash(in.Assessment),
			Dissent:        in.Dissent,
		}
		return nil
	case "resolve":
		if view.Report == nil || (in.Resolution != "accept" && in.Resolution != "reject" && in.Resolution != "breach") || in.Reason == "" {
			return nex.ErrProtocolEvidence
		}
		cmd.Resolution = &nex.Resolution{
			ReportHash: nex.Hash(view.Report),
			Outcome:    in.Resolution,
			ReasonHash: nex.Hash(in.Reason),
		}
		return nil
	case "return":
		if view.Resolution == nil || view.Report == nil || in.Uncertainty == "" || in.Assumption == "" || in.Disposition == "" {
			return nex.ErrProtocolEvidence
		}
		required := spec.Return.FailureArtifacts
		if view.Resolution.Outcome == "accept" {
			required = spec.Return.SuccessArtifacts
		}
		if len(required) == 0 {
			return nex.ErrProtocolEvidence
		}
		artifacts := make([]nex.ReturnArtifact, 0, len(required))
		for _, id := range required {
			commitment := nex.Hash(in.Uncertainty)
			if view.Resolution.Outcome == "accept" && view.Report.Manifest != nil {
				matched := false
				for _, output := range view.Report.Manifest.Outputs {
					if output.Name == id {
						commitment = output.Ref.Digest
						matched = true
						break
					}
				}
				if !matched {
					return nex.ErrProtocolEvidence
				}
			}
			artifacts = append(artifacts, nex.ReturnArtifact{
				ID:                id,
				Commitment:        commitment,
				ReuseRestrictions: "preserve the recorded limitation",
			})
		}
		cmd.Return = &nex.ReturnManifest{
			ResolutionHash:  nex.Hash(view.Resolution),
			ReviewsHash:     nex.Hash(view.Reviews),
			Evidence:        append([]string(nil), view.Report.Evidence...),
			Resources:       view.Report.Resources,
			Uncertainty:     in.Uncertainty,
			SafeAssumptions: []string{in.Assumption},
			Artifacts:       artifacts,
			Disposition:     in.Disposition,
		}
		return nil
	case "incomplete":
		if spec.Version != nex.ProtocolVersionV2 || in.Reason == "" {
			return nex.ErrProtocolEvidence
		}
		cmd.Incomplete = &nex.IncompleteReturn{
			ReasonHash: nex.Hash(in.Reason),
			Evidence:   []string{nex.Hash(in.Reason)},
			Artifacts:  append([]nex.NamedArtifact(nil), in.Artifacts...),
		}
		return nil
	default:
		return nex.ErrProtocolPhase
	}
}

func roleFor(spec nex.ProtocolSpec, actor, action string) (grant, scope string, err error) {
	if action == "admit" {
		return "", "", nil
	}
	need := permission(action)
	if need == "" {
		return "", "", nex.ErrProtocolPhase
	}
	for _, g := range spec.Roles {
		if g.Actor == actor && hasPermission(g, need) {
			return g.ID, spec.Scope, nil
		}
	}
	return "", "", adapter.ErrForeign
}

func permission(action string) nex.ProtocolPermission {
	switch action {
	case "commit":
		return nex.PermitCommit
	case "report":
		return nex.PermitWork
	case "review":
		return nex.PermitReview
	case "resolve", "failure":
		return nex.PermitDecide
	case "return", "incomplete":
		return nex.PermitReturn
	default:
		return ""
	}
}

func hasPermission(g nex.RoleGrant, p nex.ProtocolPermission) bool {
	for _, v := range g.Permissions {
		if v == p {
			return true
		}
	}
	return false
}

func participant(spec nex.ProtocolSpec, id string) bool {
	for _, p := range spec.Participants {
		if p.ID() == id {
			return true
		}
	}
	return false
}

func knownOutcome(outcome string) bool {
	switch nex.WorkOutcome(outcome) {
	case nex.WorkCompleted, nex.WorkAttempted, nex.WorkRejected, nex.WorkFailedSafely, nex.WorkViolated, nex.WorkSpecDefect, nex.WorkUnreported:
		return true
	default:
		return false
	}
}

func adapterID(id string) error {
	if len(id) < 1 || len(id) > 128 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "/\\") {
		return adapter.ErrItem
	}
	return nil
}

func boundedText(s string) error {
	if s == "" || len(s) > 4096 {
		return adapter.ErrItem
	}
	for _, r := range s {
		if r < 32 || r == 127 || unicode.Is(unicode.Cs, r) {
			return adapter.ErrItem
		}
	}
	return nil
}

func storageKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || len(raw) != 32 {
			return nil, adapter.ErrStorageKey
		}
		return raw, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err = f.Write(key); err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	return key, nil
}

func RandomNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
