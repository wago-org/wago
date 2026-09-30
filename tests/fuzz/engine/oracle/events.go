package oracle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const (
	Schema  = "starshine.engine-state-events.v1"
	I32Salt = uint64(0x693332)
	I64Salt = uint64(0x693634)
)

// Event is one positionally encoded semantic event. The schema permits only
// fixed ASCII strings, bounded non-negative integers, nested string arrays, and
// no maps, floats, or implementation identities.
type Event []any

// Marshal returns the exact compact JSON bytes used by both engine oracles.
func Marshal(events []Event) ([]byte, error) { return json.Marshal(events) }

// Hash returns the lowercase SHA-256 identity of canonical event bytes.
func Hash(canonical []byte) string {
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// MixInput64 implements the fixed engine-state input mixer.
func MixInput64(seed uint64, channel uint32, salt uint64) uint64 {
	value := seed ^ uint64(channel)*0x9e3779b97f4a7c15 ^ salt
	value = (value ^ value>>30) * 0xbf58476d1ce4e5b9
	value = (value ^ value>>27) * 0x94d049bb133111eb
	return value ^ value>>31
}
