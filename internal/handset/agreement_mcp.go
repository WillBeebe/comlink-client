package handset

import (
	"context"
	"encoding/json"
	"errors"

	"comlink/internal/agreement"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"nexum/adapter"
	"nexum/circuit"
	"nexum/nex"
)

// Go []byte fields on nex.Public marshal as base64 JSON strings rather than JSON arrays.
var agreementOpenSchema = json.RawMessage(`{
 "type":"object",
 "additionalProperties":false,
 "required":["call","purpose","outcome","scope","nonce"],
 "properties":{
  "call":{"type":"string"},
  "purpose":{"type":"string"},
  "outcome":{"type":"string"},
  "scope":{"type":"string"},
  "nonce":{"type":"string"},
  "witness":{
   "type":["object","null"],
   "additionalProperties":false,
   "required":["signing_key","encryption_key"],
   "properties":{
    "signing_key":{"type":"string","maxLength":44},
    "encryption_key":{"type":"string","maxLength":44}
   }
  },
  "terms":{
   "type":["object","null"],
   "additionalProperties":false,
   "properties":{
    "version":{"type":"integer"},
    "task_id":{"type":"string"},
    "generation":{"type":"integer","minimum":0},
    "specification_hash":{"type":"string"},
    "acceptance_hash":{"type":"string"},
    "failure_policy_hash":{"type":"string"},
    "limits_hash":{"type":"string"},
    "work_by_height":{"type":"integer","minimum":0},
    "return_by_height":{"type":"integer","minimum":0},
    "dissent_policy":{"type":"string"},
    "success_artifacts":{"type":["array","null"],"items":{"type":"string"}},
    "failure_artifacts":{"type":["array","null"],"items":{"type":"string"}}
   }
  }
 }
}`)

type agreementToolResult struct {
	Agreement         adapter.AuthenticatedResponse        `json:"agreement"`
	Sent              bool                                 `json:"sent,omitempty"`
	Phase             string                               `json:"phase,omitempty"`
	WorkSucceeded     bool                                 `json:"work_succeeded"`
	PeerEnvelopesSent int                                  `json:"peer_envelopes_sent,omitempty"`
	PeerConvergence   map[string]agreement.PeerSyncSummary `json:"peer_convergence,omitempty"`
}

func addAgreementTools(server *mcp.Server, get func() (*Phone, error)) {
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_open", InputSchema: agreementOpenSchema, Description: "Create a two-party or witnessed three-party Protocol agreement with the peer on an answered call and send the spec on an agree packet. Work and evidence, not communication consent. Same nonce retries the identical spec; altered terms on the same nonce fail closed. Optional terms bind ProtocolVersionV2, task_id, generation, content-addressed specification/acceptance hashes, deadlines, and artifact slots."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Call    string           `json:"call"`
		Purpose string           `json:"purpose"`
		Outcome string           `json:"outcome"`
		Scope   string           `json:"scope"`
		Nonce   string           `json:"nonce"`
		Witness *nex.Public      `json:"witness,omitempty"`
		Terms   *agreement.Terms `json:"terms,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		p, sess, peer, _, err := agreementCall(get, in.Call)
		if err != nil {
			return agreementFailed(err)
		}
		resp, raw, err := sess.OpenTerms(peer, in.Witness, in.Purpose, in.Outcome, in.Scope, in.Nonce, in.Terms)
		if err != nil {
			return agreementFailed(err)
		}
		sent, err := sendAgreement(ctx, p, in.Call, raw)
		out := agreementToolResult{Agreement: resp, Sent: sent, Phase: resp.Phase}
		fillAgreementView(sess, resp.AgreementID, &out)
		if err != nil {
			return agreementErrorResult(out, err)
		}
		return agreementResult(out)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_apply", Description: "Prepare a protocol command once, persist the exact signed bytes, apply them locally, then send those same bytes on an agree packet. Actions: admit, commit, report, failure, review, resolve, return, incomplete, ack. After an uncertain send, use agreement_reconcile; never replay say and never mint a new request ID for the same command."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Call        string              `json:"call"`
		ProtocolID  string              `json:"protocol_id"`
		Request     string              `json:"request"`
		Action      string              `json:"action"`
		Outcome     string              `json:"outcome,omitempty"`
		Stage       string              `json:"stage,omitempty"`
		Reason      string              `json:"reason,omitempty"`
		Dissent     bool                `json:"dissent,omitempty"`
		Assessment  string              `json:"assessment,omitempty"`
		Resolution  string              `json:"resolution,omitempty"`
		Uncertainty string              `json:"uncertainty,omitempty"`
		Assumption  string              `json:"assumption,omitempty"`
		Disposition string              `json:"disposition,omitempty"`
		Method      string              `json:"method,omitempty"`
		Claims      []string            `json:"claims,omitempty"`
		Inputs      []nex.NamedArtifact `json:"inputs,omitempty"`
		Artifacts   []nex.NamedArtifact `json:"artifacts,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		p, sess, _, dest, err := agreementCall(get, in.Call)
		if err != nil {
			return agreementFailed(err)
		}
		resp, raw, send, err := sess.Apply(agreement.ApplyInput{
			ProtocolID: in.ProtocolID, Request: in.Request, Action: in.Action, Destination: dest,
			Outcome: in.Outcome, Stage: in.Stage, Reason: in.Reason, Dissent: in.Dissent,
			Assessment: in.Assessment, Resolution: in.Resolution, Uncertainty: in.Uncertainty,
			Assumption: in.Assumption, Disposition: in.Disposition, Method: in.Method,
			Claims: in.Claims, Inputs: in.Inputs, Artifacts: in.Artifacts,
		})
		out := agreementToolResult{Agreement: resp, Phase: resp.Phase}
		fillAgreementView(sess, in.ProtocolID, &out)
		if err != nil {
			return agreementErrorResult(out, err)
		}
		if !send {
			return agreementResult(out)
		}
		sent, err := sendAgreement(ctx, p, in.Call, raw)
		out.Sent = sent
		if err != nil {
			return agreementErrorResult(out, err)
		}
		fillAgreementView(sess, in.ProtocolID, &out)
		return agreementResult(out)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_view", Description: "Read the local Protocol view and per-peer cryptographic convergence status. Completion is not work success; work_succeeded is true only when the resolution accepted the work."}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
		ProtocolID string `json:"protocol_id"`
	}) (*mcp.CallToolResult, any, error) {
		_, sess, err := agreementSession(get)
		if err != nil {
			return nil, nil, err
		}
		view, work, err := sess.View(in.ProtocolID)
		if err != nil {
			return nil, nil, err
		}
		conv, _ := sess.PeerConvergence(in.ProtocolID)
		return nil, map[string]any{"view": view, "work_succeeded": work, "peer_convergence": conv}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_reconcile", Description: "Resend the original signed Nexum bytes on an agree packet after an uncertain send, crash, or for a third-party witness joining an agreement. Does not call Prepare and does not replay say. Peer delivery requires an answered call. Omit request to reconcile both due unresolved commands and un-converged remote peer envelopes."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Call    string `json:"call,omitempty"`
		Request string `json:"request,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		p, sess, err := agreementSession(get)
		if err != nil {
			return agreementFailed(err)
		}
		if in.Request == "" {
			due, err := sess.Due()
			if err != nil {
				return agreementFailed(err)
			}
			var sent []agreementToolResult
			for _, item := range due {
				resp, raw, err := sess.Reconcile(item.Item.OperationID, false)
				one := agreementToolResult{Agreement: resp, Phase: resp.Phase}
				fillAgreementView(sess, item.Item.AgreementID, &one)
				if err != nil {
					if one.Agreement.Error == "" {
						one.Agreement.Error = err.Error()
					}
					sent = append(sent, one)
					continue
				}
				if in.Call != "" && len(raw) > 0 {
					ok, sendErr := sendAgreement(ctx, p, in.Call, raw)
					one.Sent = ok
					if sendErr != nil {
						if one.Agreement.Error == "" {
							one.Agreement.Error = sendErr.Error()
						}
						sent = append(sent, one)
						continue
					}
				}
				fillAgreementView(sess, item.Item.AgreementID, &one)
				sent = append(sent, one)
			}
			var peerSynced int
			if in.Call != "" {
				peer, _, perr := p.acceptedPeer(in.Call)
				if perr != nil {
					return agreementFailed(perr)
				}
				pendingEnvs, perr := sess.PendingPeerEnvelopes(peer.ID(), "")
				if perr != nil {
					return agreementFailed(perr)
				}
				for _, raw := range pendingEnvs {
					if _, sendErr := sendAgreement(ctx, p, in.Call, raw); sendErr != nil {
						return &mcp.CallToolResult{IsError: true}, map[string]any{"reconciled": sent, "peer_envelopes_sent": peerSynced, "error": sendErr.Error()}, nil
					}
					peerSynced++
				}
			}
			return nil, map[string]any{"reconciled": sent, "peer_envelopes_sent": peerSynced}, nil
		}
		resp, raw, err := sess.Reconcile(in.Request, true)
		out := agreementToolResult{Agreement: resp, Phase: resp.Phase}
		fillAgreementView(sess, resp.AgreementID, &out)
		if err != nil {
			if in.Call != "" && errors.Is(err, adapter.ErrNotFound) {
				if peer, _, perr := p.acceptedPeer(in.Call); perr == nil {
					if pendingEnvs, perr := sess.PendingPeerEnvelopes(peer.ID(), in.Request); perr == nil && len(pendingEnvs) > 0 {
						for _, envBytes := range pendingEnvs {
							if _, sendErr := sendAgreement(ctx, p, in.Call, envBytes); sendErr != nil {
								return agreementErrorResult(out, sendErr)
							}
						}
						out.Sent = true
						out.PeerEnvelopesSent = len(pendingEnvs)
						return nil, out, nil
					}
				}
			}
			return agreementErrorResult(out, err)
		}
		if in.Call == "" {
			return agreementResult(out)
		}
		peer, _, perr := p.acceptedPeer(in.Call)
		if perr != nil {
			return agreementFailed(perr)
		}
		pendingEnvs, _ := sess.PendingPeerEnvelopes(peer.ID(), in.Request)
		if len(pendingEnvs) > 0 {
			for _, envBytes := range pendingEnvs {
				if _, sendErr := sendAgreement(ctx, p, in.Call, envBytes); sendErr != nil {
					return agreementErrorResult(out, sendErr)
				}
			}
			out.Sent = true
			out.PeerEnvelopesSent = len(pendingEnvs)
		} else {
			sent, sendErr := sendAgreement(ctx, p, in.Call, raw)
			out.Sent = sent
			if sendErr != nil {
				return agreementErrorResult(out, sendErr)
			}
			out.PeerEnvelopesSent = 1
		}
		fillAgreementView(sess, resp.AgreementID, &out)
		return agreementResult(out)
	})
}

func fillAgreementView(sess *agreement.Session, protocolID string, out *agreementToolResult) {
	if protocolID == "" {
		return
	}
	if view, work, err := sess.View(protocolID); err == nil {
		out.Phase = view.Phase
		out.WorkSucceeded = work
	}
	if conv, err := sess.PeerConvergence(protocolID); err == nil {
		out.PeerConvergence = conv
	}
}

func agreementSession(get func() (*Phone, error)) (*Phone, *agreement.Session, error) {
	p, err := get()
	if err != nil {
		return nil, nil, err
	}
	sess := p.Agreements()
	if sess == nil {
		return nil, nil, errors.New("persistent profile required for agreements")
	}
	return p, sess, nil
}

func agreementCall(get func() (*Phone, error), call string) (*Phone, *agreement.Session, nex.Public, string, error) {
	p, sess, err := agreementSession(get)
	if err != nil {
		return nil, nil, nex.Public{}, "", err
	}
	peer, dest, err := p.acceptedPeer(call)
	if err != nil {
		return nil, nil, nex.Public{}, "", err
	}
	return p, sess, peer, dest, nil
}

func sendAgreement(ctx context.Context, p *Phone, call string, raw []byte) (bool, error) {
	if len(raw) == 0 || len(raw) > circuit.MaxText {
		return false, errors.New("agreement envelope exceeds circuit text limit")
	}
	if err := p.Act(ctx, call, "agree", string(raw)); err != nil {
		return false, err
	}
	return true, nil
}

func agreementFailed(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
}

func agreementErrorResult(out agreementToolResult, err error) (*mcp.CallToolResult, any, error) {
	if err != nil && out.Agreement.Error == "" {
		out.Agreement.Error = err.Error()
	}
	return &mcp.CallToolResult{IsError: true}, out, nil
}

func agreementResult(out agreementToolResult) (*mcp.CallToolResult, any, error) {
	failed := out.Agreement.Kind == adapter.ResultRefused || out.Agreement.Kind == adapter.ResultConflict || out.Agreement.Kind == adapter.ResultUncertain
	if failed {
		return &mcp.CallToolResult{IsError: true}, out, nil
	}
	return nil, out, nil
}
