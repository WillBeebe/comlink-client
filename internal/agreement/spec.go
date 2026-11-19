package agreement

import (
	"slices"

	"nexum/nex"
)

// Terms parameterizes real immutable agreement specifications without embedding
// product- or benchmark-specific rules into Nexum or Comlink. Omitted fields
// fall back to safe defaults so existing V1 callers remain 100% compatible.
type Terms struct {
	Version           int      `json:"version,omitempty"`
	TaskID            string   `json:"task_id,omitempty"`
	Generation        uint64   `json:"generation,omitempty"`
	SpecificationHash string   `json:"specification_hash,omitempty"`
	AcceptanceHash    string   `json:"acceptance_hash,omitempty"`
	FailurePolicyHash string   `json:"failure_policy_hash,omitempty"`
	LimitsHash        string   `json:"limits_hash,omitempty"`
	WorkByHeight      uint64   `json:"work_by_height,omitempty"`
	ReturnByHeight    uint64   `json:"return_by_height,omitempty"`
	DissentPolicy     string   `json:"dissent_policy,omitempty"`
	SuccessArtifacts  []string `json:"success_artifacts,omitempty"`
	FailureArtifacts  []string `json:"failure_artifacts,omitempty"`
}

func applyTermsDefaults(nonce string, t *Terms) Terms {
	out := Terms{
		Version:           nex.ProtocolVersion,
		SpecificationHash: nex.Hash("spec:" + nonce),
		AcceptanceHash:    nex.Hash("accept:" + nonce),
		FailurePolicyHash: nex.Hash("retain failure"),
		LimitsHash:        nex.Hash("only this call"),
		WorkByHeight:      20,
		ReturnByHeight:    40,
		DissentPolicy:     "record",
		SuccessArtifacts:  []string{"answer"},
		FailureArtifacts:  []string{"failure"},
	}
	if t == nil {
		return out
	}
	if t.Version != 0 {
		out.Version = t.Version
	}
	if t.TaskID != "" {
		out.TaskID = t.TaskID
	}
	if t.Generation != 0 {
		out.Generation = t.Generation
	}
	if t.SpecificationHash != "" {
		out.SpecificationHash = t.SpecificationHash
	}
	if t.AcceptanceHash != "" {
		out.AcceptanceHash = t.AcceptanceHash
	}
	if t.FailurePolicyHash != "" {
		out.FailurePolicyHash = t.FailurePolicyHash
	}
	if t.LimitsHash != "" {
		out.LimitsHash = t.LimitsHash
	}
	if t.WorkByHeight != 0 {
		out.WorkByHeight = t.WorkByHeight
	}
	if t.ReturnByHeight != 0 {
		out.ReturnByHeight = t.ReturnByHeight
	}
	if t.DissentPolicy != "" {
		out.DissentPolicy = t.DissentPolicy
	}
	if len(t.SuccessArtifacts) > 0 {
		out.SuccessArtifacts = slices.Clone(t.SuccessArtifacts)
	}
	if len(t.FailureArtifacts) > 0 {
		out.FailureArtifacts = slices.Clone(t.FailureArtifacts)
	}
	return out
}

// TwoParty is the default live-call agreement. The initiator may commit and
// decide. The peer may work and return. Neither role both works and decides.
// Witnesses are empty; Protocol allows that when none are required.
func TwoParty(initiator, peer nex.Public, purpose, outcome, scope, nonce string) (nex.ProtocolSpec, error) {
	return TwoPartyWithTerms(initiator, peer, purpose, outcome, scope, nonce, nil)
}

// TwoPartyV2 builds a ProtocolVersionV2 two-party agreement requiring
// host-supplied heights and reviewed output manifests on completed work.
func TwoPartyV2(initiator, peer nex.Public, purpose, outcome, scope, nonce string) (nex.ProtocolSpec, error) {
	return TwoPartyWithTerms(initiator, peer, purpose, outcome, scope, nonce, &Terms{Version: nex.ProtocolVersionV2})
}

// TwoPartyWithTerms builds a two-party agreement binding explicit task terms,
// deadlines, artifact slots, and hashes.
func TwoPartyWithTerms(initiator, peer nex.Public, purpose, outcome, scope, nonce string, terms *Terms) (nex.ProtocolSpec, error) {
	if initiator.Validate() != nil || peer.Validate() != nil || initiator.ID() == peer.ID() {
		return nex.ProtocolSpec{}, nex.ErrProtocolSpec
	}
	eff := applyTermsDefaults(nonce, terms)
	grant := func(id string, actor nex.Public, perms ...nex.ProtocolPermission) nex.RoleGrant {
		return nex.RoleGrant{
			ID:                 id,
			Actor:              actor.ID(),
			Label:              id,
			Scope:              scope,
			Permissions:        perms,
			ExpiresAtHeight:    eff.ReturnByHeight,
			LimitsHash:         eff.LimitsHash,
			EvidenceObligation: eff.AcceptanceHash,
		}
	}
	spec := nex.ProtocolSpec{
		Version:           eff.Version,
		TaskID:            eff.TaskID,
		Generation:        eff.Generation,
		Nonce:             nonce,
		Purpose:           purpose,
		IntendedOutcome:   outcome,
		Scope:             scope,
		SpecificationHash: eff.SpecificationHash,
		AcceptanceHash:    eff.AcceptanceHash,
		FailurePolicyHash: eff.FailurePolicyHash,
		Initiator:         initiator.ID(),
		Participants:      []nex.Public{initiator, peer},
		Roles: []nex.RoleGrant{
			grant("initiator", initiator, nex.PermitCommit, nex.PermitDecide, nex.PermitReturn),
			grant("worker", peer, nex.PermitWork, nex.PermitReturn),
		},
		WorkByHeight:      eff.WorkByHeight,
		ReturnByHeight:    eff.ReturnByHeight,
		RequiredWitnesses: nil,
		DissentPolicy:     eff.DissentPolicy,
		Return: nex.ReturnRequirements{
			SuccessArtifacts:   eff.SuccessArtifacts,
			FailureArtifacts:   eff.FailureArtifacts,
			AllowedDisposition: []string{"retry", "continue", "refuse"},
			AllowSuccessors:    true,
		},
		MaxEvents: 32,
	}
	if _, err := nex.NewProtocol(spec); err != nil {
		return nex.ProtocolSpec{}, err
	}
	return spec, nil
}

// ThreeParty adds a reviewer who cannot work or decide. RequiredWitnesses is
// that reviewer, so resolve cannot skip them.
func ThreeParty(initiator, worker, witness nex.Public, purpose, outcome, scope, nonce string) (nex.ProtocolSpec, error) {
	return ThreePartyWithTerms(initiator, worker, witness, purpose, outcome, scope, nonce, nil)
}

// ThreePartyV2 builds a ProtocolVersionV2 three-party agreement with a required
// witness, host-supplied heights, and reviewed output manifests.
func ThreePartyV2(initiator, worker, witness nex.Public, purpose, outcome, scope, nonce string) (nex.ProtocolSpec, error) {
	return ThreePartyWithTerms(initiator, worker, witness, purpose, outcome, scope, nonce, &Terms{Version: nex.ProtocolVersionV2})
}

// ThreePartyWithTerms builds a three-party witnessed agreement binding explicit
// task terms, generation, deadlines, dissent policy, and artifact slots.
func ThreePartyWithTerms(initiator, worker, witness nex.Public, purpose, outcome, scope, nonce string, terms *Terms) (nex.ProtocolSpec, error) {
	if initiator.Validate() != nil || worker.Validate() != nil || witness.Validate() != nil || initiator.ID() == worker.ID() || initiator.ID() == witness.ID() || worker.ID() == witness.ID() {
		return nex.ProtocolSpec{}, nex.ErrProtocolSpec
	}
	eff := applyTermsDefaults(nonce, terms)
	grant := func(id string, actor nex.Public, perms ...nex.ProtocolPermission) nex.RoleGrant {
		return nex.RoleGrant{
			ID:                 id,
			Actor:              actor.ID(),
			Label:              id,
			Scope:              scope,
			Permissions:        perms,
			ExpiresAtHeight:    eff.ReturnByHeight,
			LimitsHash:         eff.LimitsHash,
			EvidenceObligation: eff.AcceptanceHash,
		}
	}
	spec := nex.ProtocolSpec{
		Version:           eff.Version,
		TaskID:            eff.TaskID,
		Generation:        eff.Generation,
		Nonce:             nonce,
		Purpose:           purpose,
		IntendedOutcome:   outcome,
		Scope:             scope,
		SpecificationHash: eff.SpecificationHash,
		AcceptanceHash:    eff.AcceptanceHash,
		FailurePolicyHash: eff.FailurePolicyHash,
		Initiator:         initiator.ID(),
		Participants:      []nex.Public{initiator, worker, witness},
		Roles: []nex.RoleGrant{
			grant("initiator", initiator, nex.PermitCommit, nex.PermitDecide, nex.PermitReturn),
			grant("worker", worker, nex.PermitWork, nex.PermitReturn),
			grant("witness", witness, nex.PermitReview),
		},
		WorkByHeight:      eff.WorkByHeight,
		ReturnByHeight:    eff.ReturnByHeight,
		RequiredWitnesses: []string{witness.ID()},
		DissentPolicy:     eff.DissentPolicy,
		Return: nex.ReturnRequirements{
			SuccessArtifacts:   eff.SuccessArtifacts,
			FailureArtifacts:   eff.FailureArtifacts,
			AllowedDisposition: []string{"retry", "continue", "refuse"},
			AllowSuccessors:    true,
		},
		MaxEvents: 32,
	}
	if _, err := nex.NewProtocol(spec); err != nil {
		return nex.ProtocolSpec{}, err
	}
	return spec, nil
}
