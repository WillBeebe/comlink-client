package handset

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"comlink/internal/admission"
)

// Native is the in-process mobile API. It uses the same handset, identity,
// admission and encrypted circuit protocol as the MCP client. Speech is RAM-only.
// The owner explicitly answers every incoming circuit, including trusted peers.
type Native struct {
	mu                       sync.Mutex
	profile, roots, endpoint string
	phone                    *Phone
	cancel                   context.CancelFunc
	release                  func()
	events                   chan Event
	peer, call               string
	accepted                 bool
}

func NewNative(profile, roots, endpoint string) *Native {
	return &Native{profile: profile, roots: roots, endpoint: endpoint, events: make(chan Event, 64)}
}
func (n *Native) book() addressBook { return addressBook{path: n.profile + ".contacts.json"} }
func (n *Native) Open() (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.phone != nil {
		return "", errors.New("disconnect before reconnecting")
	}
	release, err := leaseProfile(n.profile)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	ok := false
	defer func() {
		if !ok {
			cancel()
			release()
		}
	}()
	cfg, err := PublicProfile(n.profile, n.roots, n.endpoint)
	if err != nil {
		return "", err
	}
	purpose := "access"
	if cfg.SubscriberID == "" {
		purpose = "enroll"
	}
	out, err := EnrollmentOperation(ctx, cfg, n.endpoint, "", purpose, cfg.SubscriberID, nil)
	if err != nil {
		return "", err
	}
	cfg.SubscriberID = out.SubscriberID
	cfg.Credential = out.Credential
	if err = saveProfile(n.profile, cfg); err != nil {
		return "", err
	}
	// Admission permits rings, never speech. Answer requires explicit local trust.
	// This profile belongs only to this app; do not import an agent profile.
	policy := admission.CallPolicy{Incoming: true, AllowUnknown: true, Allowed: []string{}, Blocked: []string{}}
	if !equal(canonicalPolicy(out.Policy), policy) {
		if _, err = EnrollmentOperation(ctx, cfg, n.endpoint, "", "policy", cfg.SubscriberID, &policy); err != nil {
			return "", err
		}
		out, err = EnrollmentOperation(ctx, cfg, n.endpoint, "", "access", cfg.SubscriberID, nil)
		if err != nil {
			return "", err
		}
		cfg.Credential = out.Credential
		if err = saveProfile(n.profile, cfg); err != nil {
			return "", err
		}
	}
	token := out.AccessToken
	n.events = make(chan Event, 64)
	events := n.events
	p, err := Connect(ctx, cfg, n.endpoint, token, "", func(e Event) {
		select {
		case events <- e:
		default:
			cancel()
		} // overflow terminates transport; never silently lose speech
	})
	if err != nil {
		return "", err
	}
	n.phone = p
	n.cancel = cancel
	n.release = release
	n.peer = ""
	n.call = ""
	n.accepted = false
	ok = true
	return cfg.Credential.Number, nil
}
func (n *Native) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.cancel != nil {
		n.cancel()
	}
	if n.phone != nil {
		n.phone.Close()
	}
	if n.release != nil {
		n.release()
	}
	n.phone = nil
	n.release = nil
	n.cancel = nil
	n.peer = ""
	n.call = ""
	n.accepted = false
	for {
		select {
		case <-n.events:
		default:
			return
		}
	}
}
func (n *Native) Contacts() (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	all, err := n.book().access(nil)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(all)
	return string(raw), err
}
func (n *Native) SaveContact(name, number string, approved bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	number, err := contactNumber(number)
	if err != nil {
		return err
	}
	if err = n.book().save(Contact{Name: name, Number: number, CommunicationApproved: approved}); err != nil {
		return err
	}
	if !approved && n.peer == number && n.phone != nil {
		// Revocation is immediate locally even when remote cleanup fails.
		n.accepted = false
		n.cancel()
		n.phone.Close()
		n.call = ""
		n.peer = ""
	}
	return nil
}
func (n *Native) permission(number string) (known, approved bool, err error) {
	all, err := n.book().access(nil)
	if err != nil {
		return false, false, err
	}
	for _, c := range all {
		if c.Number == number {
			return true, c.CommunicationApproved, nil
		}
	}
	return false, false, nil
}
func (n *Native) Dial(number string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	number, err := contactNumber(number)
	if err != nil {
		return "", err
	}
	_, approved, err := n.permission(number)
	if err != nil {
		return "", err
	}
	if !approved {
		return "", errors.New("trust this number before calling")
	}
	if n.phone == nil {
		return "", errors.New("connect first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	id, err := n.phone.Dial(ctx, number)
	if err == nil {
		n.call = id
		n.peer = number
		n.accepted = false
	}
	return id, err
}
func (n *Native) Act(action, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.phone == nil || n.call == "" {
		return errors.New("no live conversation")
	}
	if action != "answer" && action != "reject" && action != "hangup" && action != "say" {
		return errors.New("unsupported action")
	}
	if action == "answer" || action == "say" {
		_, approved, err := n.permission(n.peer)
		if err != nil {
			return err
		}
		if !approved {
			return errors.New("explicit contact trust required")
		}
	}
	if action == "say" && (!n.accepted || strings.TrimSpace(text) == "" || len([]byte(text)) > 4096) {
		return errors.New("answered conversation and 1–4096 bytes of text required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	err := n.phone.Act(ctx, n.call, action, text)
	if err == nil {
		if action == "answer" {
			n.accepted = true
		}
		if action == "reject" || action == "hangup" {
			n.call = ""
			n.peer = ""
			n.accepted = false
		}
	}
	return err
}

// NextEvent returns only authenticated handset events; call on a background queue.
// There must be one consumer. Empty means no event during the bounded wait.
func (n *Native) NextEvent() (string, error) {
	n.mu.Lock()
	events := n.events
	n.mu.Unlock()
	var e Event
	select {
	case e = <-events:
	case <-time.After(500 * time.Millisecond):
		n.mu.Lock()
		disconnected := n.phone != nil && !n.phone.Connected()
		n.mu.Unlock()
		if disconnected {
			return `{"type":"disconnected"}`, nil
		}
		return "", nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if events != n.events {
		return "", nil
	}
	if e.Type == "ring" {
		known, approved, err := n.permission(e.Packet.From)
		if err != nil {
			return "", err
		}
		if known && !approved {
			if n.phone != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = n.phone.Act(ctx, e.Call, "reject", "")
			}
			return "", nil
		}
		n.call = e.Call
		n.peer = e.Packet.From
		n.accepted = false
	}
	if e.Call == n.call && e.Type == "answer" {
		n.accepted = true
	}
	if e.Type == "disconnected" || e.Call == n.call && (e.Type == "hangup" || e.Type == "reject" || e.Type == "closed") {
		n.call = ""
		n.peer = ""
		n.accepted = false
	}
	if e.Type == "say" {
		_, approved, err := n.permission(n.peer)
		if err != nil {
			return "", err
		}
		if !approved || !n.accepted || e.Call != n.call {
			return "", nil
		}
	}
	// Do not expose keys, credentials, signatures or the internal envelope to UI.
	out := struct {
		Type string `json:"type"`
		Call string `json:"call,omitempty"`
		Peer string `json:"peer,omitempty"`
		Text string `json:"text,omitempty"`
	}{e.Type, e.Call, e.Packet.From, e.Text}
	raw, err := json.Marshal(out)
	return string(raw), err
}
