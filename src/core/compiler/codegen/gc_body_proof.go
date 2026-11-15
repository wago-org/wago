package codegen

import "crypto/sha256"

// GCBodyProof binds compiler-only metadata to the exact body that produced it.
// The digest is private: consumers can verify integrity but cannot use its
// value as an optimization selector.
type GCBodyProof struct{ digest [32]byte }

func NewGCBodyProof(body []byte) GCBodyProof {
	return GCBodyProof{digest: sha256.Sum256(body)}
}

func (p GCBodyProof) Matches(body []byte) bool {
	return p.digest == sha256.Sum256(body)
}
