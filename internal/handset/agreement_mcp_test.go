package handset

import (
	"errors"
	"testing"

	"nexum/adapter"
)

func TestAgreementErrorResultPreservesDiagnostic(t *testing.T) {
	res, payload, err := agreementErrorResult(agreementToolResult{}, errors.New("nex: protocol not found"))
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError=true, got %+v", res)
	}
	out, ok := payload.(agreementToolResult)
	if !ok {
		t.Fatalf("expected agreementToolResult payload, got %T", payload)
	}
	if out.Agreement.Error != "nex: protocol not found" {
		t.Fatalf("expected diagnostic error text preserved, got %q", out.Agreement.Error)
	}

	// Existing structured Agreement.Error must not be overwritten.
	existing := agreementToolResult{Agreement: adapter.AuthenticatedResponse{Error: "existing protocol refusal"}}
	_, payload2, _ := agreementErrorResult(existing, errors.New("secondary error"))
	out2 := payload2.(agreementToolResult)
	if out2.Agreement.Error != "existing protocol refusal" {
		t.Fatalf("existing error overwritten: got %q", out2.Agreement.Error)
	}
}
