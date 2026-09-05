// Package admission provides Comlink's local subscriber authority. It has no
// model, cloud application, family mesh, speech or conversation-store API.
package admission

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"time"

	"nexum/circuit"
	"nexum/identity"
)

const SubjectNamespace = "agent.comlink.public/v1"
const Scope = "comlink:connect"
const ChallengeTTL = time.Minute
const AccessTTL = 15 * time.Minute
const BindingTTL = 24 * time.Hour
const MaxPeers = 128

var ErrDenied = errors.New("comlink: admission refused")
var ErrCapacity = errors.New("comlink: admission capacity")
var numberPattern = regexp.MustCompile(`^[0-9a-z]{128}$`)
var idPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type CallPolicy struct {
	Incoming     bool     `json:"incoming"`
	AllowUnknown bool     `json:"allow_unknown"`
	Allowed      []string `json:"allowed"`
	Blocked      []string `json:"blocked"`
}

func (p CallPolicy) Valid() bool {
	if len(p.Allowed) > MaxPeers || len(p.Blocked) > MaxPeers {
		return false
	}
	for _, list := range [][]string{p.Allowed, p.Blocked} {
		seen := map[string]bool{}
		for _, n := range list {
			if !numberPattern.MatchString(n) || seen[n] {
				return false
			}
			seen[n] = true
		}
	}
	return true
}
func (p CallPolicy) Accepts(number string) bool {
	if !p.Incoming {
		return false
	}
	for _, n := range p.Blocked {
		if n == number {
			return false
		}
	}
	if p.AllowUnknown {
		return true
	}
	for _, n := range p.Allowed {
		if n == number {
			return true
		}
	}
	return false
}
func (p CallPolicy) canonical() CallPolicy {
	p.Allowed = append([]string{}, p.Allowed...)
	p.Blocked = append([]string{}, p.Blocked...)
	sort.Strings(p.Allowed)
	sort.Strings(p.Blocked)
	return p
}

type ChallengeRequest struct {
	Audience     string         `json:"audience"`
	Nonce        string         `json:"nonce"`
	At           time.Time      `json:"at"`
	Signature    []byte         `json:"signature"`
	Purpose      string         `json:"purpose"`
	Owner        circuit.Public `json:"owner"`
	SubscriberID string         `json:"subscriber_id,omitempty"`
	Policy       *CallPolicy    `json:"policy,omitempty"`
}

// Request proves possession before server-side challenge allocation. Completion
// still requires a fresh server nonce, preventing captured requests from issuing access.
func Request(owner circuit.Identity, purpose, subscriber, audience string, policy *CallPolicy, now time.Time) (ChallengeRequest, error) {
	p, err := owner.Public()
	if err != nil {
		return ChallengeRequest{}, ErrDenied
	}
	nonce, err := randomID()
	if err != nil {
		return ChallengeRequest{}, err
	}
	r := ChallengeRequest{Purpose: purpose, Owner: p, SubscriberID: subscriber, Audience: audience, Nonce: nonce, At: now.UTC(), Policy: policy}
	r.Signature = ed25519.Sign(owner.Signing, wire("request", r))
	return r, nil
}
func (r ChallengeRequest) valid(audience string, now time.Time) bool {
	signature := r.Signature
	r.Signature = nil
	return r.Owner.Validate() == nil && r.Audience == audience && idPattern.MatchString(r.Nonce) && !r.At.After(now.Add(5*time.Second)) && now.Sub(r.At) <= 30*time.Second && ed25519.Verify(r.Owner.Signing, wire("request", r), signature)
}

type Challenge struct {
	Version      int                `json:"version"`
	Generation   int64              `json:"generation"`
	ID           string             `json:"id"`
	Purpose      string             `json:"purpose"`
	Audience     string             `json:"audience"`
	Owner        circuit.Public     `json:"owner"`
	SubscriberID string             `json:"subscriber_id"`
	Credential   circuit.Credential `json:"credential"`
	Binding      *identity.Binding  `json:"binding,omitempty"`
	Policy       *CallPolicy        `json:"policy,omitempty"`
	Issuer       circuit.Public     `json:"issuer"`
	At           time.Time          `json:"at"`
	ExpiresAt    time.Time          `json:"expires_at"`
	Signature    []byte             `json:"signature"`
}
type Completion struct {
	ChallengeID      string `json:"challenge_id"`
	OwnerSignature   []byte `json:"owner_signature"`
	BindingSignature []byte `json:"binding_signature,omitempty"`
}
type Enrollment struct {
	SubscriberID string             `json:"subscriber_id"`
	Credential   circuit.Credential `json:"credential"`
	Binding      identity.Binding   `json:"binding"`
	Policy       CallPolicy         `json:"policy"`
	AccessToken  string             `json:"access_token,omitempty"`
	ExpiresAt    time.Time          `json:"expires_at,omitempty"`
	Revoked      bool               `json:"revoked"`
}

func wire(domain string, v any) []byte {
	raw, _ := json.Marshal(v)
	return append([]byte("comlink/admission/v1/"+domain+"\x00"), raw...)
}
func (c Challenge) unsigned() Challenge { c.Signature = nil; return c }
func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Approve verifies a typed server challenge against the endpoint's pinned roots
// and exact configured resource before signing. Callers additionally compare the
// purpose/subscriber/policy with the operation they requested. Never sign opaque
// bytes returned by a server. This returns signatures, never private keys.
func Approve(owner circuit.Identity, c Challenge, audience string, roots map[string]string, now time.Time) (Completion, error) {
	p, err := owner.Public()
	if err != nil || c.Version != 1 || !idPattern.MatchString(c.ID) || !idPattern.MatchString(c.SubscriberID) || c.Audience != audience || c.Owner.ID() != p.ID() || c.Issuer.Validate() != nil || c.Credential.Admitted(roots) != nil || !bytes.Equal(c.Credential.Mother, c.Issuer.Signing) || c.At.After(now.Add(5*time.Second)) || !now.Before(c.ExpiresAt) || !c.ExpiresAt.After(c.At) || c.ExpiresAt.Sub(c.At) > ChallengeTTL || !ed25519.Verify(c.Issuer.Signing, wire("challenge", c.unsigned()), c.Signature) {
		return Completion{}, ErrDenied
	}
	if c.Purpose == "suspend" {
		if p.ID() != c.Issuer.ID() {
			return Completion{}, ErrDenied
		}
	} else if c.Credential.Owner.ID() != p.ID() {
		return Completion{}, ErrDenied
	}
	out := Completion{ChallengeID: c.ID}
	switch c.Purpose {
	case "enroll", "access":
		if c.Binding == nil || c.Policy != nil {
			return Completion{}, ErrDenied
		}
		b := c.Binding
		endpoint, err := identity.FromComlink(c.Credential, roots)
		if err != nil || b.Issuer.ID() != c.Issuer.ID() || b.Subject != (identity.Reference{Namespace: SubjectNamespace, Authority: c.Issuer.ID(), ID: c.SubscriberID}) || b.Audience != audience || b.Status != "active" || len(b.Endpoints) != 1 || !bytes.Equal(wire("endpoint", b.Endpoints[0]), wire("endpoint", endpoint)) || b.At.After(c.At) || b.ExpiresAt.Sub(b.At) != BindingTTL || b.Revision < 1 || b.Revision > 1023 || (b.Revision == 1 && b.Previous != "") || (b.Revision > 1 && !idPattern.MatchString(b.Previous)) {
			return Completion{}, ErrDenied
		}
		raw, err := b.SigningBytes()
		if err != nil || !ed25519.Verify(c.Issuer.Signing, raw, b.Signatures[c.Issuer.ID()]) {
			return Completion{}, ErrDenied
		}
		// Fresh binding drafts have only the issuer signature. An unexpired
		// current binding may already have this endpoint signature; access renewal
		// need not append another revision to its durable identity history.
		if len(b.Signatures) == 1 && !b.At.Equal(c.At) {
			return Completion{}, ErrDenied
		}
		if len(b.Signatures) == 2 && (!ed25519.Verify(p.Signing, raw, b.Signatures[p.ID()]) || b.ExpiresAt.Sub(c.At) <= AccessTTL) {
			return Completion{}, ErrDenied
		}
		if len(b.Signatures) != 1 && len(b.Signatures) != 2 {
			return Completion{}, ErrDenied
		}
		out.BindingSignature = ed25519.Sign(owner.Signing, raw)
	case "policy":
		if c.Policy == nil || !c.Policy.Valid() || c.Binding != nil {
			return Completion{}, ErrDenied
		}
	case "revoke", "suspend":
		if c.Policy != nil || c.Binding != nil {
			return Completion{}, ErrDenied
		}
	default:
		return Completion{}, ErrDenied
	}
	out.OwnerSignature = ed25519.Sign(owner.Signing, wire("owner-proof", c))
	return out, nil
}
