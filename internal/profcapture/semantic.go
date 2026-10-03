package profcapture

import (
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"

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
		if _, err := semanticReturn(value); err != nil {
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
	returnOracle bool
	memory       []memoryOracle
	input        []memoryOracle
	inputExport  string
	outputExport string
	vector       *semanticVectorGroup
	vectorFirst  bool
}

// Vector pointers are resolved once per contract execution, then retained for
// its cases, matching the catalog runner's pointer lifetime.
type semanticVectorGroup struct {
	input, output                 uint32
	inputFallback, outputFallback uint32
	inputExport, outputExport     string
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
		calls = append(calls, checkedCall{Call: call, returnOracle: true})
	}
	for _, s := range w.semantic {
		if err := s.validate(); err != nil {
			return nil, err
		}
		if v := s.Invoke.Vectors; v != nil {
			input, output := v.InputOffset, v.OutputOffset
			maxLen := 0
			for _, c := range v.Cases {
				if c.Len > maxLen {
					maxLen = c.Len
				}
			}
			// Check the range before allocating the input pattern.
			if _, ok := in.Read(0, uint32(maxLen)); !ok {
				return nil, fmt.Errorf("%s: input range outside memory", s.ID)
			}
			pattern := make([]byte, maxLen)
			if v.Mod != 0 {
				for i := range pattern {
					pattern[i] = byte(i % v.Mod)
				}
			}
			group := &semanticVectorGroup{inputFallback: v.InputOffset, outputFallback: v.OutputOffset, inputExport: v.InputPtrExport, outputExport: v.OutputPtrExport}
			for i, c := range v.Cases {
				want, _ := hex.DecodeString(c.Out)
				call := checkedCall{Call: Call{Export: s.Invoke.Export, Args: []uint64{wago.I32(int32(input)), wago.I32(int32(c.Len)), wago.I32(int32(output))}, Want: []uint64{}}, memory: []memoryOracle{{output, want}}}
				call.input = []memoryOracle{{input, pattern[:c.Len]}}
				call.vector = group
				call.vectorFirst = i == 0
				calls = append(calls, call)
			}
			continue
		}
		call := checkedCall{Call: Call{Export: s.Invoke.Export, Want: []uint64{}}, returnOracle: len(s.Expect.Return) != 0}
		for _, a := range s.Invoke.Args {
			call.Args = append(call.Args, wago.I32(a))
		}
		for _, v := range s.Expect.Return {
			n, _ := semanticReturn(v)
			call.Want = append(call.Want, n)
		}
		if s.Invoke.Input != "" {
			input := uint32(0)
			data, _ := hex.DecodeString(s.Invoke.Input)
			call.input = []memoryOracle{{input, data}}
			call.inputExport = s.Invoke.InputPtrExport
		}
		if len(s.Expect.Memory) > 0 {
			for _, m := range s.Expect.Memory {
				data, _ := hex.DecodeString(m.Hex)
				call.memory = append(call.memory, memoryOracle{m.Offset, data})
				call.outputExport = s.Invoke.OutputPtrExport
			}
		}
		calls = append(calls, call)
	}
	return calls, nil
}

// Semantic return strings are hexadecimal ABI bits, including without 0x.
func semanticReturn(value string) (uint64, error) {
	value = strings.TrimPrefix(value, "0x")
	if value == "" {
		value = "0"
	}
	if len(value) > 16 {
		return 0, fmt.Errorf("return oracle exceeds 64 bits")
	}
	if len(value)%2 != 0 {
		value = "0" + value
	}
	data, err := hex.DecodeString(value)
	if err != nil {
		return 0, err
	}
	var result uint64
	for _, b := range data {
		result = result<<8 | uint64(b)
	}
	return result, nil
}

// initialize refreshes dynamic pointers and restores the exact input before
// every call. Argument slices are private to each checked call.
func (c *checkedCall) initialize(in *wago.Instance) error {
	if g := c.vector; g != nil {
		if c.vectorFirst {
			var err error
			g.input, err = semanticPointer(in, g.inputFallback, g.inputExport)
			if err != nil {
				return err
			}
			g.output, err = semanticPointer(in, g.outputFallback, g.outputExport)
			if err != nil {
				return err
			}
		}
		c.Args[0], c.Args[2] = wago.I32(int32(g.input)), wago.I32(int32(g.output))
		c.input[0].offset = g.input
		c.memory[0].offset = g.output
	} else if len(c.input) != 0 {
		offset, err := semanticPointer(in, 0, c.inputExport)
		if err != nil {
			return err
		}
		c.input[0].offset = offset
	}
	for _, input := range c.input {
		if !in.Write(input.offset, input.want) {
			return fmt.Errorf("%s: input initialization outside memory", c.Export)
		}
	}
	return nil
}

func (c *checkedCall) validateMemory(in *wago.Instance) error {
	base := uint32(0)
	if c.vector == nil && len(c.memory) != 0 {
		var err error
		base, err = semanticPointer(in, 0, c.outputExport)
		if err != nil {
			return err
		}
	}
	for _, check := range c.memory {
		offset := uint64(base) + uint64(check.offset)
		if offset > math.MaxUint32 {
			return fmt.Errorf("%s: output offset overflow", c.Export)
		}
		actual, ok := in.Read(uint32(offset), uint32(len(check.want)))
		if !ok || !slices.Equal(actual, check.want) {
			return fmt.Errorf("%s: memory validation failed at %d", c.Export, offset)
		}
	}
	return nil
}
