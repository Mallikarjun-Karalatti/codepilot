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

	selected := make([]CodeChunk, 0, selector.MaxChunks)
	selectedIDs := make(map[int]struct{}, selector.MaxChunks)
	appendChunk := func(chunk CodeChunk) {
		if _, exists := selectedIDs[chunk.ID]; exists || len(selected) >= selector.MaxChunks {
			return
		}
		selectedIDs[chunk.ID] = struct{}{}
		selected = append(selected, chunk)
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
					appendChunk(candidate.Chunk)
					appendChunk(parentChunk)
					continue
				}
			}
			appendChunk(candidate.Chunk)
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
			appendChunk(candidate.Chunk)
		}
	}
	for _, candidate := range candidates {
		if candidate.Origin == EvidenceCallee {
			appendChunk(candidate.Chunk)
		}
	}
	return selected, nil
}
