// Package agreement keeps a Protocol store and adapter outbox beside a handset
// profile. Signed Nexum commands ride in encrypted agree packets. Speech does not.
package agreement

import (
	"bytes"
	"encoding/json"
	"errors"

	"nexum/adapter"
	"nexum/nex"
)

const (
	magic   = "comlink-agreement-v1\n"
	version = 1
)

const (
	kindOpen  = "open"
	kindApply = "apply"
)

// Envelope is the versioned object carried inside an encrypted say.
// It binds a protocol ID, command hash, and sender public key.
// Speech never uses this prefix.
type Envelope struct {
	Version     int               `json:"version"`
	Kind        string            `json:"kind"`
	ProtocolID  string            `json:"protocol_id"`
	Spec        *nex.ProtocolSpec `json:"spec,omitempty"`
	Signed      []byte            `json:"signed,omitempty"`
	Sender      nex.Public        `json:"sender"`
	CommandHash string            `json:"command_hash"`
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
		if env.Spec == nil || len(env.Signed) != 0 {
			return Envelope{}, adapter.ErrForeign
		}
	case kindApply:
		if env.Spec != nil || len(env.Signed) == 0 {
			return Envelope{}, adapter.ErrForeign
		}
	default:
		return Envelope{}, adapter.ErrForeign
	}
	return env, nil
}

var errNotEnvelope = errors.New("agreement: not an agreement envelope")
