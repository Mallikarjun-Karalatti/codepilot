package retrieval

type EvidenceOrigin string

const (
	EvidenceDirect EvidenceOrigin = "direct"
	EvidenceParent EvidenceOrigin = "parent"
	EvidenceCallee EvidenceOrigin = "callee"
)

type EvidenceCandidate struct {
	Chunk    CodeChunk
	Origin   EvidenceOrigin
	AnchorID int
	Score    float64
}
