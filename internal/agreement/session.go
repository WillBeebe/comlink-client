package agreement

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"unicode"

	"nexum/adapter"
	"nexum/nex"
)

// Session is the local Protocol store and adapter outbox beside a handset profile.
// The outbox holds signed Nexum objects only. Speech and ciphertext stay out.
type Session struct {
	mu       sync.Mutex
	tenant   string
	identity nex.Identity
	self     nex.Public
	store    *nex.ProtocolStore
	box      *adapter.Outbox
	auth     *adapter.ProtocolAuthority
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
	store, err := nex.OpenProtocolStore(profile+".agreements", key)
	if err != nil {
		return nil, err
	}
	box, err := adapter.OpenOutbox(profile+".outbox", key)
	if err != nil {
		return nil, err
	}
	return &Session{
		tenant:   tenant,
		identity: identity,
		self:     self,
		store:    store,
		box:      box,
		auth:     &adapter.ProtocolAuthority{Tenant: tenant, Store: store},
	}, nil
}

func (s *Session) OpenAgreement(peer nex.Public, purpose, outcome, scope, nonce string) (adapter.AuthenticatedResponse, []byte, error) {
	if err := boundedText(purpose); err != nil || boundedText(outcome) != nil || boundedText(scope) != nil || boundedText(nonce) != nil {
		return adapter.AuthenticatedResponse{}, nil, adapter.ErrItem
	}
	spec, err := TwoParty(s.self, peer, purpose, outcome, scope, nonce)
	if err != nil {
		return adapter.AuthenticatedResponse{}, nil, err
	}
	return s.openSpec(spec)
}

func (s *Session) OpenWithWitness(peer, witness nex.Public, purpose, outcome, scope, nonce string) (adapter.AuthenticatedResponse, []byte, error) {
	if err := boundedText(purpose); err != nil || boundedText(outcome) != nil || boundedText(scope) != nil || boundedText(nonce) != nil {
		return adapter.AuthenticatedResponse{}, nil, adapter.ErrItem
	}
	spec, err := ThreeParty(s.self, peer, witness, purpose, outcome, scope, nonce)
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
}

// Apply prepares a command at most once, persists the exact signed bytes, applies
// them locally, and records the verified result. The returned bytes are what a
// later reconcile must resend. send is false when this request was already resolved.
func (s *Session) Apply(in ApplyInput) (resp adapter.AuthenticatedResponse, envelope []byte, send bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, getErr := s.box.Get(s.tenant, in.Request)
	if getErr != nil && !errors.Is(getErr, adapter.ErrNotFound) {
		return adapter.AuthenticatedResponse{}, nil, false, getErr
	}
	if errors.Is(getErr, adapter.ErrNotFound) {
		if err = s.stage(in); err != nil {
			return adapter.AuthenticatedResponse{}, nil, false, err
		}
	}
	resp, envelope, err = s.finish(in.Request, true)
	if err != nil {
		return resp, nil, false, err
	}
	send = existing.Status != adapter.StatusResolved
	return resp, envelope, send, err
}

// Reconcile resubmits the stored signed bytes. It never calls Prepare.
// force ignores backoff. A crash between apply and RecordVerified is finished here.
func (s *Session) Reconcile(request string, force bool) (adapter.AuthenticatedResponse, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force {
		if _, err := s.box.ReconcileDue(s.tenant, request, s.auth); errors.Is(err, adapter.ErrNotDue) {
			return adapter.AuthenticatedResponse{}, nil, err
		}
	}
	return s.finish(request, true)
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

// Incoming applies an exact envelope. A foreign agreement, tenant, or command
// is refused. Exact retry does not add a protocol transition.
func (s *Session) Incoming(plain []byte, sender nex.Public) error {
	env, err := decode(plain)
	if err != nil {
		return err
	}
	if env.Sender.ID() != sender.ID() {
		return adapter.ErrForeign
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch env.Kind {
	case kindOpen:
		return s.incomingOpen(env)
	case kindApply:
		return s.incomingApply(env)
	default:
		return adapter.ErrForeign
	}
}

func (s *Session) incomingOpen(env Envelope) error {
	if env.CommandHash != env.ProtocolID || env.Spec == nil {
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
		return nil
	}
	if err != nil {
		return err
	}
	if id != env.ProtocolID {
		return adapter.ErrForeign
	}
	return nil
}

func (s *Session) incomingApply(env Envelope) error {
	payload, err := adapter.DecodePayload(env.Signed)
	if err != nil || payload.Kind != adapter.KindProtocol || payload.Command == nil {
		return adapter.ErrForeign
	}
	cmd := *payload.Command
	if adapter.CommandHash(env.Signed) != env.CommandHash || cmd.ProtocolID != env.ProtocolID || cmd.Actor != env.Sender.ID() {
		return adapter.ErrForeign
	}
	bundle, err := s.store.Export(env.ProtocolID)
	if err != nil {
		return adapter.ErrForeign
	}
	if !participant(bundle.Spec, s.self.ID()) || !participant(bundle.Spec, cmd.Actor) {
		return adapter.ErrForeign
	}
	item := adapter.OutboundItem{
		Version:      adapter.Version,
		Tenant:       s.tenant,
		AgreementID:  env.ProtocolID,
		OperationID:  cmd.RequestID,
		Kind:         adapter.KindProtocol,
		SignedObject: append([]byte(nil), env.Signed...),
		Destination:  "local",
	}
	if err = s.box.Enqueue(item); err != nil && !errors.Is(err, adapter.ErrConflict) {
		return err
	}
	if errors.Is(err, adapter.ErrConflict) {
		return adapter.ErrForeign
	}
	resp, err := s.box.Reconcile(s.tenant, cmd.RequestID, s.auth)
	if err != nil {
		return err
	}
	return s.record(cmd.RequestID, resp)
}

func (s *Session) stage(in ApplyInput) error {
	if err := adapterID(in.Request); err != nil || adapterID(in.ProtocolID) != nil || adapterID(in.Destination) != nil {
		return adapter.ErrItem
	}
	for _, field := range []string{in.Stage, in.Reason, in.Assessment, in.Uncertainty, in.Assumption, in.Disposition, in.Outcome, in.Resolution} {
		if field != "" && boundedText(field) != nil {
			return adapter.ErrItem
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
	cmd, err := s.store.Prepare(in.ProtocolID, s.self.ID(), grant, scope, in.Action, in.Request, view.Height+1)
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

func (s *Session) finish(request string, record bool) (adapter.AuthenticatedResponse, []byte, error) {
	resp, err := s.box.Reconcile(s.tenant, request, s.auth)
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
		if in.Stage == "" || in.Reason == "" || !knownOutcome(in.Outcome) {
			return nex.ErrProtocolEvidence
		}
		cmd.Report = &nex.WorkReport{
			Outcome:    nex.WorkOutcome(in.Outcome),
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
		artifact := ""
		if view.Resolution.Outcome == "accept" && len(spec.Return.SuccessArtifacts) > 0 {
			artifact = spec.Return.SuccessArtifacts[0]
		}
		if view.Resolution.Outcome != "accept" && len(spec.Return.FailureArtifacts) > 0 {
			artifact = spec.Return.FailureArtifacts[0]
		}
		if artifact == "" {
			return nex.ErrProtocolEvidence
		}
		cmd.Return = &nex.ReturnManifest{
			ResolutionHash:  nex.Hash(view.Resolution),
			ReviewsHash:     nex.Hash(view.Reviews),
			Evidence:        append([]string(nil), view.Report.Evidence...),
			Resources:       view.Report.Resources,
			Uncertainty:     in.Uncertainty,
			SafeAssumptions: []string{in.Assumption},
			Artifacts:       []nex.ReturnArtifact{{ID: artifact, Commitment: nex.Hash(in.Uncertainty), ReuseRestrictions: "preserve the recorded limitation"}},
			Disposition:     in.Disposition,
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
	case "resolve":
		return nex.PermitDecide
	case "return":
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
