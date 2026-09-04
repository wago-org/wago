package wagobench

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero/api"
	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/semanticcorpus"
)

const semanticCorpusRoot = "../tests/corpora"

var (
	semanticManifestOnce sync.Once
	semanticManifest     *semanticcorpus.Manifest
	semanticManifestErr  error
)

func semanticExecCases(tb testing.TB, corpus corpusModule) []semanticcorpus.Module {
	tb.Helper()
	if len(corpus.SemanticExec) == 0 {
		return nil
	}
	semanticManifestOnce.Do(func() {
		semanticManifest, semanticManifestErr = semanticcorpus.LoadManifest(filepath.Join(semanticCorpusRoot, "MANIFEST.json"))
	})
	if semanticManifestErr != nil {
		tb.Fatalf("load semantic corpus manifest: %v", semanticManifestErr)
	}

	byID := make(map[string]semanticcorpus.Module, len(semanticManifest.Modules))
	for _, mod := range semanticManifest.Modules {
		byID[mod.ID] = mod
	}
	wantArtifact := filepath.ToSlash(corpus.Path)
	const corpusPrefix = "../tests/corpora/"
	if !strings.HasPrefix(wantArtifact, corpusPrefix) {
		tb.Fatalf("%s: semantic corpus path %q is outside %s", corpus.name(), corpus.Path, corpusPrefix)
	}
	wantArtifact = strings.TrimPrefix(wantArtifact, corpusPrefix)
	result := make([]semanticcorpus.Module, 0, len(corpus.SemanticExec))
	for _, id := range corpus.SemanticExec {
		mod, ok := byID[id]
		if !ok {
			tb.Fatalf("%s: semantic_exec case %q is not in tests/corpora/MANIFEST.json", corpus.name(), id)
		}
		if mod.Artifact != wantArtifact {
			tb.Fatalf("%s: semantic_exec case %q uses %s, want %s", corpus.name(), id, mod.Artifact, wantArtifact)
		}
		if mod.KnownIssue != "" {
			tb.Fatalf("%s: semantic_exec case %q still has known_issue: %s", corpus.name(), id, mod.KnownIssue)
		}
		result = append(result, mod)
	}
	return result
}

func TestCorpusSemanticExec(t *testing.T) {
	for _, corpus := range loadCorpus(t) {
		for _, semantic := range semanticExecCases(t, corpus) {
			semantic := semantic
			t.Run(semantic.ID, func(t *testing.T) {
				if err := runSemanticOracle(semantic); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func runSemanticOracle(mod semanticcorpus.Module) error {
	return semanticcorpus.Run(semanticCorpusRoot, mod)
}

type wagoSemanticExec struct {
	fn    *wago.PreparedFunction
	calls [][]uint64
}

func prepareWagoSemanticExec(in *wago.Instance, mod semanticcorpus.Module) (*wagoSemanticExec, error) {
	fn, err := in.PrepareFunction(mod.Invoke.Export)
	if err != nil {
		return nil, err
	}
	if mod.Invoke.Vectors != nil {
		v := mod.Invoke.Vectors
		inputOffset, err := wagoPointer(in, v.InputOffset, v.InputPtrExport)
		if err != nil {
			return nil, err
		}
		outputOffset, err := wagoPointer(in, v.OutputOffset, v.OutputPtrExport)
		if err != nil {
			return nil, err
		}
		maxLen := 0
		for _, vector := range v.Cases {
			if vector.Len > maxLen {
				maxLen = vector.Len
			}
		}
		input := semanticPattern(maxLen, v.Mod)
		if !in.Write(inputOffset, input) {
			return nil, fmt.Errorf("write %d vector input bytes at %d", len(input), inputOffset)
		}
		calls := make([][]uint64, len(v.Cases))
		for i, vector := range v.Cases {
			calls[i] = []uint64{wago.I32(int32(inputOffset)), wago.I32(int32(vector.Len)), wago.I32(int32(outputOffset))}
		}
		return &wagoSemanticExec{fn: fn, calls: calls}, nil
	}
	if err := writeWagoSemanticInput(in, mod); err != nil {
		return nil, err
	}
	args := make([]uint64, len(mod.Invoke.Args))
	for i, arg := range mod.Invoke.Args {
		args[i] = wago.I32(arg)
	}
	return &wagoSemanticExec{fn: fn, calls: [][]uint64{args}}, nil
}

func (e *wagoSemanticExec) invoke() error {
	for _, args := range e.calls {
		if _, err := e.fn.Invoke(args...); err != nil {
			return err
		}
	}
	return nil
}

func wagoPointer(in *wago.Instance, fallback uint32, export string) (uint32, error) {
	if export == "" {
		return fallback, nil
	}
	result, err := in.Invoke(export)
	if err != nil {
		return 0, fmt.Errorf("resolve pointer export %s: %w", export, err)
	}
	if len(result) != 1 {
		return 0, fmt.Errorf("resolve pointer export %s: returned %d values, want 1", export, len(result))
	}
	return uint32(wago.AsI32(result[0])), nil
}

func writeWagoSemanticInput(in *wago.Instance, mod semanticcorpus.Module) error {
	if mod.Invoke.Input == "" {
		return nil
	}
	input, err := hex.DecodeString(mod.Invoke.Input)
	if err != nil {
		return err
	}
	offset, err := wagoPointer(in, 0, mod.Invoke.InputPtrExport)
	if err != nil {
		return err
	}
	if !in.Write(offset, input) {
		return fmt.Errorf("write %d input bytes at %d", len(input), offset)
	}
	return nil
}

type wazeroSemanticExec struct {
	fn    api.Function
	calls [][]uint64
}

func prepareWazeroSemanticExec(ctx context.Context, in api.Module, mod semanticcorpus.Module) (*wazeroSemanticExec, error) {
	fn := in.ExportedFunction(mod.Invoke.Export)
	if fn == nil {
		return nil, fmt.Errorf("export %s not found", mod.Invoke.Export)
	}
	if mod.Invoke.Vectors != nil {
		v := mod.Invoke.Vectors
		inputOffset, err := wazeroPointer(ctx, in, v.InputOffset, v.InputPtrExport)
		if err != nil {
			return nil, err
		}
		outputOffset, err := wazeroPointer(ctx, in, v.OutputOffset, v.OutputPtrExport)
		if err != nil {
			return nil, err
		}
		maxLen := 0
		for _, vector := range v.Cases {
			if vector.Len > maxLen {
				maxLen = vector.Len
			}
		}
		input := semanticPattern(maxLen, v.Mod)
		if !in.Memory().Write(inputOffset, input) {
			return nil, fmt.Errorf("write %d vector input bytes at %d", len(input), inputOffset)
		}
		calls := make([][]uint64, len(v.Cases))
		for i, vector := range v.Cases {
			calls[i] = []uint64{uint64(inputOffset), uint64(uint32(vector.Len)), uint64(outputOffset)}
		}
		return &wazeroSemanticExec{fn: fn, calls: calls}, nil
	}
	if mod.Invoke.Input != "" {
		input, err := hex.DecodeString(mod.Invoke.Input)
		if err != nil {
			return nil, err
		}
		offset, err := wazeroPointer(ctx, in, 0, mod.Invoke.InputPtrExport)
		if err != nil {
			return nil, err
		}
		if !in.Memory().Write(offset, input) {
			return nil, fmt.Errorf("write %d input bytes at %d", len(input), offset)
		}
	}
	args := make([]uint64, len(mod.Invoke.Args))
	for i, arg := range mod.Invoke.Args {
		args[i] = uint64(uint32(arg))
	}
	return &wazeroSemanticExec{fn: fn, calls: [][]uint64{args}}, nil
}

func (e *wazeroSemanticExec) invoke(ctx context.Context) error {
	for _, args := range e.calls {
		if _, err := e.fn.Call(ctx, args...); err != nil {
			return err
		}
	}
	return nil
}

func wazeroPointer(ctx context.Context, in api.Module, fallback uint32, export string) (uint32, error) {
	if export == "" {
		return fallback, nil
	}
	fn := in.ExportedFunction(export)
	if fn == nil {
		return 0, fmt.Errorf("pointer export %s not found", export)
	}
	result, err := fn.Call(ctx)
	if err != nil {
		return 0, fmt.Errorf("resolve pointer export %s: %w", export, err)
	}
	if len(result) != 1 {
		return 0, fmt.Errorf("resolve pointer export %s: returned %d values, want 1", export, len(result))
	}
	return uint32(result[0]), nil
}

func semanticPattern(length, mod int) []byte {
	result := make([]byte, length)
	if mod > 0 {
		for i := range result {
			result[i] = byte(i % mod)
		}
	}
	return result
}
