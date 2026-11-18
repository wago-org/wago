//go:build amd64

package amd64

import "os"

const streamMaxHomes = 8

var streamingCopyProofEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_COPY_PROOF") == "1"

var streamingResidencyEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_RESIDENCY") == "1"
var streamingWideHomesEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_WIDE_HOMES") == "1"

var streamingLocalLifetimesEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_LIFETIMES") == "1"

var streamingRegisterHomesEnabled = os.Getenv("WAGO_EXPERIMENTAL_STREAM_HOMES") == "1"

func (s *streamSummary) selectHomes(scores []uint32) {
	limit := 2
	if streamingResidencyEnabled && streamingWideHomesEnabled {
		limit = streamMaxHomes
	}
	if s.Nodes > 16384-limit {
		return
	}
	for k := 0; k < limit; k++ {
		best, score := -1, uint32(0)
		for i, raw := range scores {
			if i >= 256 {
				continue
			}
			selected := false
			for j := 0; j < k; j++ {
				if s.HomeLocals[j] == uint16(i+1) {
					selected = true
					break
				}
			}
			if selected {
				continue
			}
			if v := localHotness(raw); v > score {
				best, score = i, v
			}
		}
		if best >= 0 {
			s.HomeLocals[k] = uint16(best + 1)
			s.Nodes++
		}
	}
}

const (
	streamEventWrite uint32 = 1 << 24
	streamEventDead  uint32 = 1 << 25
)

// streamSummary is admission metadata, not an instruction array. Admission is
// performed on validated bytecode before any function instruction is emitted.
type streamSummary struct {
	LocalEvents []uint32 // straight-line local events; PC16, local8, write/dead bits

	HomeLocals               [streamMaxHomes]uint16 // selected local indices plus one; zero means absent
	Eligible, HasIf          bool
	ResultN                  int
	ResultWide, Memory, Bulk bool
	MaxControlDepth          uint8
	MaxStack, Nodes          int
}
