package main

import "fmt"

type EvidenceSelectionPolicy string

const (
	EvidencePolicyRequiredParent EvidenceSelectionPolicy = "required_parent"
	EvidencePolicyDirectFirst    EvidenceSelectionPolicy = "direct_first"
)

type EvidenceSelector struct {
	MaxChunks int
	Policy    EvidenceSelectionPolicy
}

// Select applies the configured evidence policy. The empty policy defaults to
// required-parent selection for backwards compatibility.
func (selector *EvidenceSelector) Select(candidates []EvidenceCandidate) ([]CodeChunk, error) {
	evidence, err := selector.SelectEvidence(candidates)
	if err != nil {
		return nil, err
	}
	chunks := make([]CodeChunk, len(evidence))
	for i, candidate := range evidence {
		chunks[i] = candidate.Chunk
	}
	return chunks, nil
}

// SelectEvidence applies the configured selection policy while retaining the
// retrieval provenance associated with each selected chunk.
func (selector *EvidenceSelector) SelectEvidence(candidates []EvidenceCandidate) ([]EvidenceCandidate, error) {
	if selector == nil {
		return nil, fmt.Errorf("evidence selector must not be nil")
	}
	if selector.MaxChunks <= 0 {
		return nil, fmt.Errorf("max chunks must be greater than 0")
	}
	policy := selector.Policy
	if policy == "" {
		policy = EvidencePolicyRequiredParent
	}
	if policy != EvidencePolicyRequiredParent && policy != EvidencePolicyDirectFirst {
		return nil, fmt.Errorf("unsupported evidence selection policy %q", policy)
	}

	chunksByID := make(map[int]CodeChunk, len(candidates))
	directSeen := make(map[int]struct{})
	for _, candidate := range candidates {
		chunksByID[candidate.Chunk.ID] = candidate.Chunk
	}

	selected := make([]EvidenceCandidate, 0, selector.MaxChunks)
	selectedIDs := make(map[int]struct{}, selector.MaxChunks)
	appendCandidate := func(candidate EvidenceCandidate) {
		if _, exists := selectedIDs[candidate.Chunk.ID]; exists || len(selected) >= selector.MaxChunks {
			return
		}
		selectedIDs[candidate.Chunk.ID] = struct{}{}
		selected = append(selected, candidate)
	}

	appendDirect := func() error {
		for _, candidate := range candidates {
			if candidate.Origin != EvidenceDirect {
				continue
			}
			if _, duplicate := directSeen[candidate.Chunk.ID]; duplicate {
				continue
			}
			directSeen[candidate.Chunk.ID] = struct{}{}

			if policy == EvidencePolicyRequiredParent && candidate.Chunk.Kind == ChunkKindMethod && candidate.Chunk.ParentID != 0 {
				_, alreadySelected := selectedIDs[candidate.Chunk.ParentID]
				if !alreadySelected {
					parentChunk, available := chunksByID[candidate.Chunk.ParentID]
					if !available {
						return fmt.Errorf("required parent %d for method %q is missing from evidence", candidate.Chunk.ParentID, candidate.Chunk.Name)
					}
					if len(selected)+2 > selector.MaxChunks {
						continue
					}
					appendCandidate(candidate)
					parentEvidence := EvidenceCandidate{Chunk: parentChunk, Origin: EvidenceParent, AnchorID: candidate.Chunk.ID}
					for _, possibleParent := range candidates {
						if possibleParent.Chunk.ID == parentChunk.ID && possibleParent.Origin == EvidenceParent {
							parentEvidence = possibleParent
							break
						}
					}
					appendCandidate(parentEvidence)
					continue
				}
			}
			appendCandidate(candidate)
		}
		return nil
	}
	if err := appendDirect(); err != nil {
		return nil, err
	}

	// Direct-first uses only capacity left after all ranked direct hits for
	// structural evidence. Required-parent treats parents as dependencies before
	// optional callees.
	for _, candidate := range candidates {
		if policy == EvidencePolicyDirectFirst && candidate.Origin == EvidenceParent {
			appendCandidate(candidate)
		}
	}
	for _, candidate := range candidates {
		if candidate.Origin == EvidenceCallee {
			appendCandidate(candidate)
		}
	}
	return selected, nil
}
