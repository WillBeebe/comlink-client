// Package agreement keeps a private Protocol store and adapter outbox beside a
// handset profile. Signed Nexum commands ride inside encrypted agree packets. Speech does not.
package agreement

import (
	"bytes"
	"encoding/json"
	"errors"

	"nexum/adapter"
	"nexum/nex"
)

const (
	magic         = "comlink-agreement-v1\n"
	version       = 1
	ackSignDomain = "comlink-agreement-ack/v1"
)

const (
	kindOpen  = "open"
	kindApply = "apply"
	kindAck   = "ack"
)

// AckPayload is signed by the receiving endpoint after durably applying an open
// or apply envelope to its local ProtocolStore. It contains zero speech.
type AckPayload struct {
	ProtocolID  string `json:"protocol_id"`
	CommandHash string `json:"command_hash"`
	ReceiptHead string `json:"receipt_head"`
	Phase       string `json:"phase"`
}

// Envelope is the versioned object carried inside an encrypted agree packet.
// It binds a protocol ID, command hash, and sender public key.
// Speech never uses this prefix.
type Envelope struct {
	Version      int               `json:"version"`
	Kind         string            `json:"kind"`
	ProtocolID   string            `json:"protocol_id"`
	Spec         *nex.ProtocolSpec `json:"spec,omitempty"`
	Signed       []byte            `json:"signed,omitempty"`
	Ack          *AckPayload       `json:"ack,omitempty"`
	AckSignature []byte            `json:"ack_signature,omitempty"`
	Sender       nex.Public        `json:"sender"`
	CommandHash  string            `json:"command_hash"`
}

func IsEnvelope(plain []byte) bool {
	return bytes.HasPrefix(plain, []byte(magic))
}

func encode(env Envelope) ([]byte, error) {
	env.Version = version
	body, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(magic)+len(body))
	out = append(out, magic...)
	out = append(out, body...)
	return out, nil
}

func decode(plain []byte) (Envelope, error) {
	if !IsEnvelope(plain) {
		return Envelope{}, errNotEnvelope
	}
	var env Envelope
	if err := json.Unmarshal(plain[len(magic):], &env); err != nil || env.Version != version {
		return Envelope{}, adapter.ErrForeign
	}
	if env.Sender.Validate() != nil || env.ProtocolID == "" || env.CommandHash == "" {
		return Envelope{}, adapter.ErrForeign
	}
	switch env.Kind {
	case kindOpen:
		if env.Spec == nil || len(env.Signed) != 0 || env.Ack != nil {
			return Envelope{}, adapter.ErrForeign
		}
	case kindApply:
		if env.Spec != nil || len(env.Signed) == 0 || env.Ack != nil {
			return Envelope{}, adapter.ErrForeign
		}
	case kindAck:
		if env.Spec != nil || len(env.Signed) != 0 || env.Ack == nil || len(env.AckSignature) == 0 ||
			env.Ack.ProtocolID != env.ProtocolID || env.Ack.CommandHash != env.CommandHash || env.Ack.ReceiptHead == "" {
			return Envelope{}, adapter.ErrForeign
		}
		if err := nex.Verify(env.Sender, ackSignDomain, *env.Ack, env.AckSignature); err != nil {
			return Envelope{}, adapter.ErrForeign
		}
	default:
		return Envelope{}, adapter.ErrForeign
	}
	return env, nil
}

var errNotEnvelope = errors.New("agreement: not an agreement envelope")
