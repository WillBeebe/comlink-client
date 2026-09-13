package handset

import (
	"context"
	"errors"

	"comlink/internal/agreement"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"nexum/adapter"
	"nexum/circuit"
	"nexum/nex"
)

type agreementToolResult struct {
	Agreement     adapter.AuthenticatedResponse `json:"agreement"`
	Sent          bool                          `json:"sent,omitempty"`
	Phase         string                        `json:"phase,omitempty"`
	WorkSucceeded bool                          `json:"work_succeeded"`
}

func addAgreementTools(server *mcp.Server, get func() (*Phone, error)) {
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_open", Description: "Create a Protocol agreement with the peer on an answered call and send the spec on an agree packet. Work and evidence, not communication consent. Same nonce retries the identical spec. After an uncertain send, call agreement_open again with that nonce; do not replay say."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Call    string      `json:"call"`
		Purpose string      `json:"purpose"`
		Outcome string      `json:"outcome"`
		Scope   string      `json:"scope"`
		Nonce   string      `json:"nonce"`
		Witness *nex.Public `json:"witness,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		p, sess, peer, _, err := agreementCall(get, in.Call)
		if err != nil {
			return agreementFailed(err)
		}
		var resp adapter.AuthenticatedResponse
		var raw []byte
		if in.Witness != nil {
			resp, raw, err = sess.OpenWithWitness(peer, *in.Witness, in.Purpose, in.Outcome, in.Scope, in.Nonce)
		} else {
			resp, raw, err = sess.OpenAgreement(peer, in.Purpose, in.Outcome, in.Scope, in.Nonce)
		}
		if err != nil {
			return agreementFailed(err)
		}
		sent, err := sendAgreement(ctx, p, in.Call, raw)
		out := agreementToolResult{Agreement: resp, Sent: sent, Phase: resp.Phase}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		return agreementResult(out)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_apply", Description: "Prepare a protocol command once, persist the exact signed bytes, apply them locally, then send those same bytes on an agree packet. Actions: admit, commit, report, review, resolve, return. After an uncertain send, use agreement_reconcile; never replay say and never mint a new request ID for the same command."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		Call        string `json:"call"`
		ProtocolID  string `json:"protocol_id"`
		Request     string `json:"request"`
		Action      string `json:"action"`
		Outcome     string `json:"outcome,omitempty"`
		Stage       string `json:"stage,omitempty"`
		Reason      string `json:"reason,omitempty"`
		Dissent     bool   `json:"dissent,omitempty"`
		Assessment  string `json:"assessment,omitempty"`
		Resolution  string `json:"resolution,omitempty"`
		Uncertainty string `json:"uncertainty,omitempty"`
		Assumption  string `json:"assumption,omitempty"`
		Disposition string `json:"disposition,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		p, sess, _, dest, err := agreementCall(get, in.Call)
		if err != nil {
			return agreementFailed(err)
		}
		resp, raw, send, err := sess.Apply(agreement.ApplyInput{
			ProtocolID: in.ProtocolID, Request: in.Request, Action: in.Action, Destination: dest,
			Outcome: in.Outcome, Stage: in.Stage, Reason: in.Reason, Dissent: in.Dissent,
			Assessment: in.Assessment, Resolution: in.Resolution, Uncertainty: in.Uncertainty,
			Assumption: in.Assumption, Disposition: in.Disposition,
		})
		out := agreementToolResult{Agreement: resp, Phase: resp.Phase}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		if !send {
			return agreementResult(out)
		}
		sent, err := sendAgreement(ctx, p, in.Call, raw)
		out.Sent = sent
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		_, work, viewErr := sess.View(in.ProtocolID)
		if viewErr == nil {
			out.WorkSucceeded = work
		}
		return agreementResult(out)
	})
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_view", Description: "Read the local Protocol view. Completion is not work success; work_succeeded is true only when the resolution accepted the work."}, func(_ context.Context, _ *mcp.CallToolRequest, in struct {
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
		return nil, map[string]any{"view": view, "work_succeeded": work}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "agreement_reconcile", Description: "Resend the original signed Nexum bytes on an agree packet after an uncertain send or a crash. Does not call Prepare and does not replay say. Peer delivery requires a new answered call. Omit request to reconcile due unresolved commands."}, func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
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
				if err != nil {
					sent = append(sent, one)
					continue
				}
				if in.Call != "" && len(raw) > 0 {
					ok, sendErr := sendAgreement(ctx, p, in.Call, raw)
					one.Sent = ok
					if sendErr != nil {
						sent = append(sent, one)
						continue
					}
				}
				sent = append(sent, one)
			}
			return nil, map[string]any{"reconciled": sent}, nil
		}
		resp, raw, err := sess.Reconcile(in.Request, true)
		out := agreementToolResult{Agreement: resp, Phase: resp.Phase}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		if in.Call == "" {
			return agreementResult(out)
		}
		sent, err := sendAgreement(ctx, p, in.Call, raw)
		out.Sent = sent
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		_, work, viewErr := sess.View(resp.AgreementID)
		if viewErr == nil {
			out.WorkSucceeded = work
		}
		return agreementResult(out)
	})
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

func agreementResult(out agreementToolResult) (*mcp.CallToolResult, any, error) {
	failed := out.Agreement.Kind == adapter.ResultRefused || out.Agreement.Kind == adapter.ResultConflict || out.Agreement.Kind == adapter.ResultUncertain
	if failed {
		return &mcp.CallToolResult{IsError: true}, out, nil
	}
	return nil, out, nil
}
