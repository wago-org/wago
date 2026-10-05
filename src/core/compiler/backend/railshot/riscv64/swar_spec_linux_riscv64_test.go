//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type swarSpecInstance struct {
	image               *swarModuleImage
	module              *wasm.Module
	code                *coreruntime.CodeBuffer
	engine              *coreruntime.Engine
	job                 *coreruntime.JobMemory
	arena               *coreruntime.Arena
	args, results, trap []byte
	globals             *swarGlobalsOwner
	data                *swarDataOwner
	table               *swarTableOwner
}

func (in *swarSpecInstance) close() error {
	if in == nil {
		return nil
	}
	// Every invocation has joined. Detach all native bindings before destroying
	// the job, then release call arenas, engine stack and the shared code image.
	// On failure retain the remaining lifetime dependencies rather than unmap
	// underneath a failed owner release.
	if in.globals != nil {
		if err := in.globals.Close(); err != nil {
			return err
		}
		in.globals = nil
	}
	if in.data != nil {
		if err := in.data.Close(); err != nil {
			return err
		}
		in.data = nil
	}
	if in.table != nil {
		if err := in.table.Close(); err != nil {
			return err
		}
		in.table = nil
	}
	if in.job != nil {
		if err := in.job.Close(); err != nil {
			return err
		}
		in.job = nil
	}
	if in.arena != nil {
		if err := in.arena.Close(); err != nil {
			return err
		}
		in.arena = nil
	}
	if in.engine != nil {
		if err := in.engine.Close(); err != nil {
			return err
		}
		in.engine = nil
	}
	if in.code != nil {
		if err := in.code.Close(); err != nil {
			return err
		}
		in.code = nil
	}
	return nil
}

func swarSpecInstantiate(t *testing.T, source []byte) (*swarSpecInstance, error) {
	t.Helper()
	// Whole-module admission precedes code publication and every state arena.
	image, err := compileSWARTableModule(source, emissionMode{production: true, interruptible: true})
	if err != nil {
		return nil, err
	}
	decoded, err := wasm.DecodeModuleByteBackedWithLimits(source, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	in := &swarSpecInstance{image: image, module: decoded.Module}
	complete := false
	defer func() {
		if !complete {
			if err := in.close(); err != nil {
				t.Error("partial spec instance cleanup", err)
			}
		}
	}()
	initial, maximum := 0, 0
	if image.memory != nil {
		if image.memory.Limits.Min > 16 {
			t.Fatal("unexpected spec fixture initial memory exceeds 16-page test budget")
		}
		initial = int(image.memory.Limits.Min) * 65536
		maxPages := uint64(65536)
		if image.memory.Limits.HasMax {
			maxPages = image.memory.Limits.Max
		}
		// This reserves the exact memory32 ceiling (up to 4 GiB virtual address
		// space); only initial pages are backed. One module lives at a time.
		maximum = int(maxPages) * 65536
	}
	in.engine, err = coreruntime.NewEngineWithStackBytes(coreruntime.MinNativeStackBytes)
	if err != nil {
		t.Fatal(err)
	}
	in.job, err = coreruntime.NewJobMemoryGrowable(initial, maximum)
	if err != nil {
		t.Fatal(err)
	}
	in.arena, err = coreruntime.NewArena(8192)
	if err != nil {
		t.Fatal(err)
	}
	in.args, in.results, in.trap = in.arena.Alloc(2048), in.arena.Alloc(2048), in.arena.Alloc(coreruntime.TrapBufferBytes)
	in.job.SetStackFence(in.engine.StackLimit())
	if err := in.job.RebindTrapCell(in.trap); err != nil {
		t.Fatal(err)
	}
	in.code, err = coreruntime.NewCodeBuffer(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.code.Append(image.code); err != nil {
		t.Fatal(err)
	}
	if err := in.code.Seal(); err != nil {
		t.Fatal(err)
	}
	in.globals, err = image.instantiateGlobals(in.job)
	if err != nil {
		t.Fatal(err)
	}
	in.data, err = image.instantiateData(in.job)
	if err != nil {
		t.Fatal(err)
	}
	in.table, err = image.instantiateTables(in.job, in.code)
	if err != nil {
		t.Fatal(err)
	}
	complete = true
	return in, nil
}

func (in *swarSpecInstance) invoke(t *testing.T, command swarSpecCommand) {
	t.Helper()
	if command.Action.Type != "invoke" {
		t.Fatal("unsupported spec action", command.Action.Type)
	}
	index := -1
	for _, export := range in.module.Exports {
		if export.Name == command.Action.Field && export.Index.Kind == wasm.ExternFunc {
			index = int(export.Index.Index)
		}
	}
	if index < 0 || index >= len(in.image.functions) {
		t.Fatal("missing defined function export", command.Action.Field)
	}
	ft, ok := in.module.LocalFuncType(index)
	if !ok || len(ft.Params) != len(command.Action.Args) || len(ft.Results) != len(command.Expected) {
		t.Fatal("spec action/signature mismatch", command.Action.Field)
	}
	clear(in.trap)
	clear(in.args)
	for i := range in.results {
		in.results[i] = 0xa5
	}
	argBytes := 0
	for i, value := range command.Action.Args {
		typ, _, _, _ := swarSpecLayout(value)
		if typ != ft.Params[i] {
			t.Fatal("spec argument type mismatch", i)
		}
		arg, err := swarSpecInput(value)
		if err != nil || len(arg) > len(in.args)-argBytes {
			t.Fatal("spec argument encoding/bound", i, err)
		}
		copy(in.args[argBytes:], arg)
		// Exercise real caller-supplied upper bits in every i32/f32 parameter.
		if value.Type == "i32" || value.Type == "f32" {
			binary.LittleEndian.PutUint64(in.args[argBytes:], dirty(binary.LittleEndian.Uint32(arg)))
		}
		argBytes += len(arg)
	}
	resultBytes := 0
	for i, value := range command.Expected {
		typ, _, _, _ := swarSpecLayout(value)
		// Trap JSON carries result types without values/lane types.
		if command.Type == "assert_trap" && value.Type == "v128" {
			typ = wasm.V128
		}
		if typ != ft.Results[i] {
			t.Fatal("spec expected result type mismatch", i)
		}
		if typ == wasm.V128 {
			resultBytes += 16
		} else {
			resultBytes += 8
		}
	}
	if resultBytes > len(in.results) || argBytes/8 != in.image.functions[index].Params || resultBytes/8 != in.image.functions[index].Results {
		t.Fatal("spec compact carrier metadata/bound")
	}
	entry := in.code.Base() + uintptr(in.image.functions[index].Entry)
	err := in.engine.CallPrepared(entry, in.args, in.job.LinMemBase(), in.trap, in.results)
	if command.Type == "assert_trap" {
		var trap *coreruntime.TrapError
		if command.Text != "out of bounds memory access" || !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds {
			t.Fatalf("spec trap line %d %s: want %q, got %v", command.Line, command.Action.Field, command.Text, err)
		}
		if !bytes.Equal(in.results, bytes.Repeat([]byte{0xa5}, len(in.results))) {
			t.Fatal("pinned memory-trap fixture wrote the result arena")
		}
		return
	}
	if err != nil {
		t.Fatalf("spec return line %d %s: %v", command.Line, command.Action.Field, err)
	}
	offset := 0
	for i, value := range command.Expected {
		width := 8
		if value.Type == "v128" {
			width = 16
		}
		if err := swarSpecMatch(in.results[offset:offset+width], value); err != nil {
			t.Fatalf("spec line %d %s result %d: %v", command.Line, command.Action.Field, i, err)
		}
		offset += width
	}
	if !bytes.Equal(in.results[resultBytes:], bytes.Repeat([]byte{0xa5}, len(in.results)-resultBytes)) {
		t.Fatal("spec result write exceeded compact export signature")
	}
}

func TestSWARRV64PinnedSIMDSpecExecution(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_SPEC_JSON_DIR")
	if dir == "" {
		t.Skip("prepare repository-pinned fixtures with scripts/prepare-riscv64-simd-spec.py and set WAGO_RV64_SPEC_JSON_DIR")
	}
	manifest, err := swarSpecLoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for name := range manifest.Sources {
		if !strings.HasSuffix(name, ".wast") {
			t.Fatal("unexpected source fixture", name)
		}
		files = append(files, strings.TrimSuffix(name, ".wast")+".json")
	}
	sort.Strings(files)
	total := make(map[string]int)
	// Deliberately closed private policies: a memory-only module, a global-only
	// exporter, and a global importer. No generic unsupported skip is permitted.
	exclusions := map[string]string{
		"simd_const.112.wasm": "SWAR module state/features/function bound",
		"simd_linking.0.wasm": "SWAR module state/features/function bound",
		"simd_linking.1.wasm": "SWAR function import section",
	}
	seenExcluded := make(map[string]bool)
	for _, file := range files {
		t.Run(strings.TrimSuffix(file, ".json"), func(t *testing.T) {
			data, err := manifest.read(dir, file, 16<<20)
			if err != nil {
				t.Fatal(err)
			}
			var document swarSpecDocument
			if err := swarSpecDecode(data, &document); err != nil {
				t.Fatal(err)
			}
			if document.SourceFilename != strings.TrimSuffix(file, ".json")+".wast" {
				t.Fatal("incorrect source filename", document.SourceFilename)
			}
			counts := make(map[string]int)
			var current *swarSpecInstance
			var excluded error
			defer func() {
				if err := current.close(); err != nil {
					t.Error(err)
				}
			}()
			for _, command := range document.Commands {
				switch command.Type {
				case "module":
					if err := current.close(); err != nil {
						t.Fatal(err)
					}
					current = nil
					body, err := manifest.read(dir, command.Filename, MaxModuleBytes)
					if err != nil {
						t.Fatal(err)
					}
					current, excluded = swarSpecInstantiate(t, body)
					if excluded != nil {
						want, listed := exclusions[command.Filename]
						if !listed || seenExcluded[command.Filename] || !errors.Is(excluded, ErrUnsupported) || excluded.Error() != ErrUnsupported.Error()+": "+want {
							t.Fatalf("valid spec module %s: %v", command.Filename, excluded)
						}
						seenExcluded[command.Filename] = true
						counts["excluded_modules"]++
						t.Logf("excluded module %s: %v", command.Filename, excluded)
					} else {
						if _, listed := exclusions[command.Filename]; listed {
							t.Fatal("excluded policy widened; independently review fixture coverage", command.Filename)
						}
						counts["modules"]++
					}
				case "assert_return", "assert_trap":
					if excluded != nil {
						t.Fatal("execution assertion follows an excluded module", command.Line, excluded)
					}
					if current == nil {
						t.Fatal("action without current module")
					}
					current.invoke(t, command)
					counts[command.Type]++
				case "assert_invalid", "assert_malformed", "register":
					// This checkpoint qualifies native execution only. These
					// validation/linking commands are counted, never claimed.
					counts["excluded_"+command.Type]++
				default:
					t.Fatal("unexpected pinned spec command", command.Type)
				}
			}
			for kind, n := range counts {
				total[kind] += n
			}
			t.Logf("coverage %s %v", file, counts)
		})
	}
	t.Logf("pinned spec %s, WABT %s; executed/excluded coverage %v", manifest.SpecCommit, manifest.WabtVersion, total)
	want := map[string]int{"modules": 469, "excluded_modules": 3, "assert_return": 24273, "assert_trap": 54, "excluded_assert_invalid": 669, "excluded_assert_malformed": 510, "excluded_register": 1}
	if !reflect.DeepEqual(total, want) || len(seenExcluded) != len(exclusions) {
		t.Fatal("pinned native execution coverage changed", total, "want", want, "exclusions", seenExcluded)
	}
}
