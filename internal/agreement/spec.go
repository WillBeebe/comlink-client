package agreement

import (
	"nexum/nex"
)

// TwoParty is the default live-call agreement. The initiator may commit and
// decide. The peer may work and return. Neither role both works and decides.
// Witnesses are empty; Protocol allows that when none are required.
func TwoParty(initiator, peer nex.Public, purpose, outcome, scope, nonce string) (nex.ProtocolSpec, error) {
	if initiator.Validate() != nil || peer.Validate() != nil || initiator.ID() == peer.ID() {
		return nex.ProtocolSpec{}, nex.ErrProtocolSpec
	}
	grant := func(id string, actor nex.Public, perms ...nex.ProtocolPermission) nex.RoleGrant {
		return nex.RoleGrant{
			ID:                 id,
			Actor:              actor.ID(),
			Label:              id,
			Scope:              scope,
			Permissions:        perms,
			ExpiresAtHeight:    40,
			LimitsHash:         nex.Hash("only this call"),
			EvidenceObligation: nex.Hash("cite evidence"),
		}
	}
	spec := nex.ProtocolSpec{
		Version:           nex.ProtocolVersion,
		Nonce:             nonce,
		Purpose:           purpose,
		IntendedOutcome:   outcome,
		Scope:             scope,
		SpecificationHash: nex.Hash("spec:" + nonce),
		AcceptanceHash:    nex.Hash("accept:" + nonce),
		FailurePolicyHash: nex.Hash("retain failure"),
		Initiator:         initiator.ID(),
		Participants:      []nex.Public{initiator, peer},
		Roles: []nex.RoleGrant{
			grant("initiator", initiator, nex.PermitCommit, nex.PermitDecide, nex.PermitReturn),
			grant("worker", peer, nex.PermitWork, nex.PermitReturn),
		},
		WorkByHeight:      20,
		ReturnByHeight:    40,
		RequiredWitnesses: nil,
		DissentPolicy:     "record",
		Return: nex.ReturnRequirements{
			SuccessArtifacts:   []string{"answer"},
			FailureArtifacts:   []string{"failure"},
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
	if initiator.Validate() != nil || worker.Validate() != nil || witness.Validate() != nil || initiator.ID() == worker.ID() || initiator.ID() == witness.ID() || worker.ID() == witness.ID() {
		return nex.ProtocolSpec{}, nex.ErrProtocolSpec
	}
	grant := func(id string, actor nex.Public, perms ...nex.ProtocolPermission) nex.RoleGrant {
		return nex.RoleGrant{
			ID:                 id,
			Actor:              actor.ID(),
			Label:              id,
			Scope:              scope,
			Permissions:        perms,
			ExpiresAtHeight:    40,
			LimitsHash:         nex.Hash("only this call"),
			EvidenceObligation: nex.Hash("cite evidence"),
		}
	}
	spec := nex.ProtocolSpec{
		Version:           nex.ProtocolVersion,
		Nonce:             nonce,
		Purpose:           purpose,
		IntendedOutcome:   outcome,
		Scope:             scope,
		SpecificationHash: nex.Hash("spec:" + nonce),
		AcceptanceHash:    nex.Hash("accept:" + nonce),
		FailurePolicyHash: nex.Hash("retain failure"),
		Initiator:         initiator.ID(),
		Participants:      []nex.Public{initiator, worker, witness},
		Roles: []nex.RoleGrant{
			grant("initiator", initiator, nex.PermitCommit, nex.PermitDecide, nex.PermitReturn),
			grant("worker", worker, nex.PermitWork, nex.PermitReturn),
			grant("witness", witness, nex.PermitReview),
		},
		WorkByHeight:      20,
		ReturnByHeight:    40,
		RequiredWitnesses: []string{witness.ID()},
		DissentPolicy:     "record",
		Return: nex.ReturnRequirements{
			SuccessArtifacts:   []string{"answer"},
			FailureArtifacts:   []string{"failure"},
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
