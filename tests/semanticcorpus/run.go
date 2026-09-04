package semanticcorpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wago "github.com/wago-org/wago"
)

// Run executes one corpus case against the wago core API and returns the exact
// observed result plus any oracle mismatch. It runs a fresh instance and a
// second instance from the same compiled module; both must reproduce the exact
// result (lifecycle/determinism check).
func Run(root string, mod Module) error {
	timeout := time.Duration(mod.Limits.TimeoutMS) * time.Millisecond

	wasm, err := readArtifact(root, mod)
	if err != nil {
		return err
	}
	compiled, err := wago.Compile(nil, wasm)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}

	if mod.Invoke.Vectors != nil {
		return runVectors(compiled, mod, timeout)
	}

	first, err := runInstance(compiled, mod, timeout)
	if err != nil {
		return err
	}
	second, err := runInstance(compiled, mod, timeout)
	if err != nil {
		return fmt.Errorf("second instance: %w", err)
	}
	if err := compareOutcomes(first, second); err != nil {
		return fmt.Errorf("fresh/second instance disagree: %w", err)
	}
	return nil
}

// RunRepeated executes and checks the same semantic case repeatedly on one
// instance. It catches porting layers that pass once but retain or exhaust
// guest state when used as a steady-state workload.
func RunRepeated(root string, mod Module, repetitions int) error {
	if repetitions <= 0 {
		return fmt.Errorf("repetitions must be positive")
	}
	timeout := time.Duration(mod.Limits.TimeoutMS) * time.Millisecond
	wasm, err := readArtifact(root, mod)
	if err != nil {
		return err
	}
	compiled, err := wago.Compile(nil, wasm)
	if err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	inst, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
	if err != nil {
		return fmt.Errorf("instantiate: %w", err)
	}
	defer inst.Close()
	for i := 0; i < repetitions; i++ {
		if mod.Invoke.Vectors != nil {
			if _, err := runVectorCasesOnInstance(inst, mod, mod.Invoke.Vectors, timeout); err != nil {
				return fmt.Errorf("repetition %d: %w", i+1, err)
			}
			continue
		}
		if _, err := runOnInstance(inst, mod, timeout); err != nil {
			return fmt.Errorf("repetition %d: %w", i+1, err)
		}
	}
	return nil
}

type outcome struct {
	results []uint64
	memory  [][]byte
}

func readArtifact(root string, mod Module) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(mod.Artifact))
	wasm, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact %s: %w", mod.Artifact, err)
	}
	sum := sha256.Sum256(wasm)
	if got := hex.EncodeToString(sum[:]); got != mod.ArtifactSHA256 {
		return nil, fmt.Errorf("artifact %s SHA-256 = %s, want %s (run tests/corpora/<corpus>/build.sh to rebuild, then review the diff before re-pinning)", mod.Artifact, got, mod.ArtifactSHA256)
	}
	return wasm, nil
}

func runInstance(compiled *wago.Compiled, mod Module, timeout time.Duration) (*outcome, error) {
	inst, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
	if err != nil {
		return nil, fmt.Errorf("instantiate: %w", err)
	}
	defer inst.Close()
	return runOnInstance(inst, mod, timeout)
}

func runOnInstance(inst *wago.Instance, mod Module, timeout time.Duration) (*outcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var err error

	if mod.Invoke.Input != "" {
		inputBase := uint32(0)
		if mod.Invoke.InputPtrExport != "" {
			inputBase, err = resolvePointerContext(ctx, inst, mod.Invoke.InputPtrExport)
			if err != nil {
				return nil, err
			}
		}
		data, err := hex.DecodeString(mod.Invoke.Input)
		if err != nil {
			return nil, fmt.Errorf("invoke.input: %w", err)
		}
		if !inst.Write(inputBase, data) {
			return nil, fmt.Errorf("write %d input bytes at offset %d failed", len(data), inputBase)
		}
	}

	args := make([]uint64, len(mod.Invoke.Args))
	for i, a := range mod.Invoke.Args {
		args[i] = wago.I32(a)
	}

	results, err := inst.InvokeContext(ctx, mod.Invoke.Export, args...)
	if err != nil {
		return nil, fmt.Errorf("invoke %s: %w", mod.Invoke.Export, err)
	}

	if err := checkReturn(mod, results); err != nil {
		return nil, err
	}
	mem, err := captureMemory(ctx, inst, mod)
	if err != nil {
		return nil, err
	}
	return &outcome{results: results, memory: mem}, nil
}

func checkReturn(mod Module, results []uint64) error {
	if len(mod.Expect.Return) == 0 {
		return nil
	}
	if len(results) != len(mod.Expect.Return) {
		return fmt.Errorf("export %s returned %d values, want %d", mod.Invoke.Export, len(results), len(mod.Expect.Return))
	}
	for i, wantHex := range mod.Expect.Return {
		want, err := parseHexUint64(wantHex)
		if err != nil {
			return err
		}
		if results[i] != want {
			return fmt.Errorf("result[%d] = 0x%016x, want 0x%016x", i, results[i], want)
		}
	}
	return nil
}

func captureMemory(ctx context.Context, inst *wago.Instance, mod Module) ([][]byte, error) {
	outputBase := uint32(0)
	if mod.Invoke.OutputPtrExport != "" {
		var err error
		outputBase, err = resolvePointerContext(ctx, inst, mod.Invoke.OutputPtrExport)
		if err != nil {
			return nil, err
		}
	}
	out := make([][]byte, len(mod.Expect.Memory))
	for i, cell := range mod.Expect.Memory {
		want, err := hex.DecodeString(cell.Hex)
		if err != nil {
			return nil, fmt.Errorf("memory oracle %d hex: %w", i, err)
		}
		offset := outputBase + cell.Offset
		got, ok := inst.Read(offset, uint32(len(want)))
		if !ok {
			return nil, fmt.Errorf("memory oracle %d: read at offset %d length %d failed", i, offset, len(want))
		}
		if !bytesEqual(got, want) {
			return nil, fmt.Errorf("memory[%d] @0x%x = %s, want %s", i, offset, hex.EncodeToString(got), cell.Hex)
		}
		out[i] = got
	}
	return out, nil
}

// runVectors drives the published-vector oracle: for every case it generates
// the input pattern, invokes Export(inputOffset, len, outputOffset), and
// compares the output against the published digest.
func runVectors(compiled *wago.Compiled, mod Module, timeout time.Duration) error {
	v := mod.Invoke.Vectors
	first, err := runVectorCases(compiled, mod, v, timeout)
	if err != nil {
		return err
	}
	second, err := runVectorCases(compiled, mod, v, timeout)
	if err != nil {
		return fmt.Errorf("second instance: %w", err)
	}
	for i := range first {
		if !bytesEqual(first[i], second[i]) {
			return fmt.Errorf("vectors case %d: fresh/second instance disagree: %s vs %s",
				i, hex.EncodeToString(first[i]), hex.EncodeToString(second[i]))
		}
	}
	return nil
}

func runVectorCases(compiled *wago.Compiled, mod Module, v *Vectors, timeout time.Duration) ([][]byte, error) {
	inst, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
	if err != nil {
		return nil, fmt.Errorf("instantiate: %w", err)
	}
	defer inst.Close()
	return runVectorCasesOnInstance(inst, mod, v, timeout)
}

func runVectorCasesOnInstance(inst *wago.Instance, mod Module, v *Vectors, timeout time.Duration) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var err error

	inputOffset := v.InputOffset
	if v.InputPtrExport != "" {
		inputOffset, err = resolvePointerContext(ctx, inst, v.InputPtrExport)
		if err != nil {
			return nil, err
		}
	}
	outputOffset := v.OutputOffset
	if v.OutputPtrExport != "" {
		outputOffset, err = resolvePointerContext(ctx, inst, v.OutputPtrExport)
		if err != nil {
			return nil, err
		}
	}

	outs := make([][]byte, len(v.Cases))
	for i, c := range v.Cases {
		input := make([]byte, c.Len)
		for j := range input {
			if v.Mod > 0 {
				input[j] = byte(j % v.Mod)
			}
		}
		if !inst.Write(inputOffset, input) {
			return nil, fmt.Errorf("vectors case %d: write %d bytes at offset %d failed", i, c.Len, inputOffset)
		}
		if _, err := inst.InvokeContext(ctx, mod.Invoke.Export,
			wago.I32(int32(inputOffset)), wago.I32(int32(c.Len)), wago.I32(int32(outputOffset))); err != nil {
			return nil, fmt.Errorf("vectors case %d (len %d): invoke %s: %w", i, c.Len, mod.Invoke.Export, err)
		}
		got, ok := inst.Read(outputOffset, uint32(v.OutputLen))
		if !ok {
			return nil, fmt.Errorf("vectors case %d: read %d bytes at offset %d failed", i, v.OutputLen, outputOffset)
		}
		want, err := hex.DecodeString(c.Out)
		if err != nil {
			return nil, fmt.Errorf("vectors case %d: expected hex: %w", i, err)
		}
		if !bytesEqual(got, want) {
			return nil, fmt.Errorf("vectors case %d (len %d): got %s, want %s", i, c.Len, hex.EncodeToString(got), c.Out)
		}
		outs[i] = got
	}
	return outs, nil
}

func resolvePointerContext(ctx context.Context, inst *wago.Instance, export string) (uint32, error) {
	results, err := inst.InvokeContext(ctx, export)
	if err != nil {
		return 0, fmt.Errorf("resolve pointer export %s: %w", export, err)
	}
	if len(results) != 1 {
		return 0, fmt.Errorf("resolve pointer export %s: returned %d values, want 1", export, len(results))
	}
	return uint32(wago.AsI32(results[0])), nil
}

func compareOutcomes(a, b *outcome) error {
	if len(a.results) != len(b.results) {
		return fmt.Errorf("result count %d vs %d", len(a.results), len(b.results))
	}
	for i := range a.results {
		if a.results[i] != b.results[i] {
			return fmt.Errorf("result[%d] 0x%016x vs 0x%016x", i, a.results[i], b.results[i])
		}
	}
	if len(a.memory) != len(b.memory) {
		return fmt.Errorf("memory cell count %d vs %d", len(a.memory), len(b.memory))
	}
	for i := range a.memory {
		if !bytesEqual(a.memory[i], b.memory[i]) {
			return fmt.Errorf("memory[%d] %s vs %s", i, hex.EncodeToString(a.memory[i]), hex.EncodeToString(b.memory[i]))
		}
	}
	return nil
}

func parseHexUint64(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		s = "0"
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	if len(s) > 16 {
		return 0, fmt.Errorf("hex value %q exceeds 64 bits", s)
	}
	v, err := hex.DecodeString(s)
	if err != nil {
		return 0, fmt.Errorf("hex value %q: %w", s, err)
	}
	var out uint64
	for _, b := range v {
		out = out<<8 | uint64(b)
	}
	return out, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
