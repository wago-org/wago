package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/wago-org/wago/internal/jitprofile"
)

// ReadSamply joins only sampled leaf symbols from a Firefox processed profile.
// Observations can include off-CPU time; neither sample weights nor thread CPU
// deltas are relabelled as CPU time. Ancestor frames are deliberately ignored.
func ReadSamply(r io.Reader, events []jitprofile.Event) (Report, error) {
	var input struct {
		Threads []struct {
			Strings []string `json:"stringArray"`
			Samples struct {
				Stack  []*int  `json:"stack"`
				Weight []int64 `json:"weight"`
			} `json:"samples"`
			Stack struct {
				Frame []int `json:"frame"`
			} `json:"stackTable"`
			Frames struct {
				Function []int `json:"func"`
			} `json:"frameTable"`
			Functions struct {
				Name []int `json:"name"`
			} `json:"funcTable"`
		} `json:"threads"`
	}
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return Report{}, err
	}
	result := Report{Unit: "observations"}
	symbols := make(map[string]int)
	keys := make(map[string]int)
	for _, e := range events {
		if e.Image != nil {
			im := e.Image
			for _, region := range im.Regions {
				if !executable(region) || region.Kind == "unknown" {
					continue
				}
				kind := region.Kind
				offset := region.Offset
				if region.Function >= 0 {
					kind = "function"
					offset = 0
				}
				key := fmt.Sprintf("%s/%s/%d/%s/%d", im.ModuleID, im.ArtifactID, region.Function, kind, offset)
				index, ok := keys[key]
				if !ok {
					row := Row{ModuleID: im.ModuleID, ArtifactID: im.ArtifactID, Function: region.Function, Kind: kind, Name: region.Name, RegionOffset: offset}
					for _, fn := range im.Functions {
						if fn.Index == region.Function {
							f := fn
							row.Static = &f
							break
						}
					}
					index = len(result.Rows)
					result.Rows = append(result.Rows, row)
					keys[key] = index
				}
				symbols[Symbol(*im, region)] = index
			}
		}
	}
	for _, thread := range input.Threads {
		for i, stack := range thread.Samples.Stack {
			weight := int64(1)
			if len(thread.Samples.Weight) > 0 {
				if i >= len(thread.Samples.Weight) {
					return result, fmt.Errorf("truncated samply weights")
				}
				weight = thread.Samples.Weight[i]
			}
			if weight < 0 {
				return result, fmt.Errorf("negative samply sample weight")
			}
			result.Samples += uint64(weight)
			result.Weight += uint64(weight)
			unknown := func() { result.UnknownSamples += uint64(weight); result.UnknownWeight += uint64(weight) }
			if stack == nil {
				unknown()
				continue
			}
			if *stack < 0 || *stack >= len(thread.Stack.Frame) {
				return result, fmt.Errorf("invalid samply stack")
			}
			frame := thread.Stack.Frame[*stack]
			if frame < 0 || frame >= len(thread.Frames.Function) {
				return result, fmt.Errorf("invalid samply frame")
			}
			fn := thread.Frames.Function[frame]
			if fn < 0 || fn >= len(thread.Functions.Name) {
				return result, fmt.Errorf("invalid samply function")
			}
			name := thread.Functions.Name[fn]
			if name < 0 || name >= len(thread.Strings) {
				return result, fmt.Errorf("invalid samply symbol")
			}
			row, ok := symbols[thread.Strings[name]]
			if !ok {
				unknown()
				continue
			}
			result.Rows[row].Samples += uint64(weight)
			result.Rows[row].Weight += uint64(weight)
		}
	}
	kept := result.Rows[:0]
	for _, row := range result.Rows {
		if row.Samples > 0 {
			kept = append(kept, row)
		}
	}
	result.Rows = kept
	sort.Slice(result.Rows, func(i, j int) bool { return result.Rows[i].Weight > result.Rows[j].Weight })
	return result, nil
}
