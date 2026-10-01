package profcapture

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"

	"github.com/wago-org/wago"
)

// These fields retain the catalog's existing semantic contract. This runner
// supports core numeric calls and exact memory oracles; imports remain outside
// its admission policy.
type semanticCase struct {
	ID         string `json:"id"`
	Artifact   string `json:"artifact"`
	Hash       string `json:"artifact_sha256"`
	ABI        string `json:"abi"`
	KnownIssue string `json:"known_issue"`
	Invoke     struct {
		Export          string  `json:"export"`
		Args            []int32 `json:"args"`
		Input           string  `json:"input"`
		InputPtrExport  string  `json:"input_ptr_export"`
		OutputPtrExport string  `json:"output_ptr_export"`
		Vectors         *struct {
			InputOffset     uint32 `json:"input_offset"`
			OutputOffset    uint32 `json:"output_offset"`
			InputPtrExport  string `json:"input_ptr_export"`
			OutputPtrExport string `json:"output_ptr_export"`
			OutputLen       int    `json:"output_len"`
			Mod             int    `json:"mod"`
			Cases           []struct {
				Len int    `json:"len"`
				Out string `json:"out"`
			} `json:"cases"`
		} `json:"vectors"`
	} `json:"invoke"`
	Expect struct {
		Return []string `json:"return"`
		Memory []struct {
			Offset uint32 `json:"offset"`
			Hex    string `json:"hex"`
		} `json:"memory"`
	} `json:"expect"`
}

func (s semanticCase) validate() error {
	if s.ABI != "core" || s.KnownIssue != "" || s.Invoke.Export == "" {
		return fmt.Errorf("unsupported ABI, known issue, or missing export")
	}
	if _, err := hex.DecodeString(s.Invoke.Input); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	for _, value := range s.Expect.Return {
		if _, err := strconv.ParseUint(value, 0, 64); err != nil {
			return fmt.Errorf("return oracle: %w", err)
		}
	}
	for _, m := range s.Expect.Memory {
		if b, err := hex.DecodeString(m.Hex); err != nil || len(b) == 0 {
			return fmt.Errorf("invalid memory oracle")
		}
	}
	if v := s.Invoke.Vectors; v != nil {
		if len(s.Invoke.Args) != 0 || s.Invoke.Input != "" || len(s.Expect.Return) != 0 || len(s.Expect.Memory) != 0 {
			return fmt.Errorf("mixed vector and single-call contracts")
		}
		if v.OutputLen <= 0 || uint64(v.OutputLen) > math.MaxUint32 || v.Mod < 0 || v.Mod > 256 || len(v.Cases) == 0 {
			return fmt.Errorf("invalid vector contract")
		}
		for _, c := range v.Cases {
			b, err := hex.DecodeString(c.Out)
			if c.Len < 0 || uint64(c.Len) > math.MaxUint32 || err != nil || len(b) != v.OutputLen {
				return fmt.Errorf("invalid vector input length or output oracle")
			}
		}
	} else if len(s.Expect.Return) == 0 && len(s.Expect.Memory) == 0 {
		return fmt.Errorf("missing exact oracle")
	}
	return nil
}

type memoryOracle struct {
	offset uint32
	want   []byte
}
type checkedCall struct {
	Call
	memory []memoryOracle
	input  []memoryOracle
}

func semanticPointer(in *wago.Instance, fallback uint32, export string) (uint32, error) {
	if export == "" {
		return fallback, nil
	}
	result, err := in.Invoke(export)
	if err != nil {
		return 0, fmt.Errorf("pointer export %s: %w", export, err)
	}
	if len(result) != 1 {
		return 0, fmt.Errorf("pointer export %s needs one result", export)
	}
	return uint32(wago.AsI32(result[0])), nil
}

func prepareCheckedCalls(in *wago.Instance, w Workload) ([]checkedCall, error) {
	calls := make([]checkedCall, 0, len(w.Calls))
	for _, call := range w.Calls {
		calls = append(calls, checkedCall{Call: call})
	}
	for _, s := range w.semantic {
		if err := s.validate(); err != nil {
			return nil, err
		}
		if v := s.Invoke.Vectors; v != nil {
			input, err := semanticPointer(in, v.InputOffset, v.InputPtrExport)
			if err != nil {
				return nil, err
			}
			output, err := semanticPointer(in, v.OutputOffset, v.OutputPtrExport)
			if err != nil {
				return nil, err
			}
			maxLen := 0
			for _, c := range v.Cases {
				if c.Len > maxLen {
					maxLen = c.Len
				}
			}
			// Check the range before allocating the input pattern.
			if _, ok := in.Read(input, uint32(maxLen)); !ok {
				return nil, fmt.Errorf("%s: input range outside memory", s.ID)
			}
			pattern := make([]byte, maxLen)
			if v.Mod != 0 {
				for i := range pattern {
					pattern[i] = byte(i % v.Mod)
				}
			}
			for i, c := range v.Cases {
				want, _ := hex.DecodeString(c.Out)
				call := checkedCall{Call: Call{Export: s.Invoke.Export, Args: []uint64{wago.I32(int32(input)), wago.I32(int32(c.Len)), wago.I32(int32(output))}, Want: []uint64{}}, memory: []memoryOracle{{output, want}}}
				if i == 0 {
					call.input = []memoryOracle{{input, pattern}}
				}
				calls = append(calls, call)
			}
			continue
		}
		call := checkedCall{Call: Call{Export: s.Invoke.Export, Want: []uint64{}}}
		for _, a := range s.Invoke.Args {
			call.Args = append(call.Args, wago.I32(a))
		}
		for _, v := range s.Expect.Return {
			n, _ := strconv.ParseUint(v, 0, 64)
			call.Want = append(call.Want, n)
		}
		if s.Invoke.Input != "" {
			input, err := semanticPointer(in, 0, s.Invoke.InputPtrExport)
			if err != nil {
				return nil, err
			}
			data, _ := hex.DecodeString(s.Invoke.Input)
			call.input = []memoryOracle{{input, data}}
		}
		if len(s.Expect.Memory) > 0 {
			output, err := semanticPointer(in, 0, s.Invoke.OutputPtrExport)
			if err != nil {
				return nil, err
			}
			for _, m := range s.Expect.Memory {
				if uint64(output)+uint64(m.Offset) > math.MaxUint32 {
					return nil, fmt.Errorf("%s: output offset overflow", s.ID)
				}
				data, _ := hex.DecodeString(m.Hex)
				call.memory = append(call.memory, memoryOracle{output + m.Offset, data})
			}
		}
		calls = append(calls, call)
	}
	return calls, nil
}
