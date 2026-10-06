//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/simdfixture"
)

// These opt-in suites qualify execution through the public API and trusted
// artifacts. Invalid/malformed modules and linking commands are counted as
// exclusions, never advertised as full release conformance.
func TestRV64PublicPinnedSIMDSpecExecution(t *testing.T) {
	rv64PublicSpecExecution(t, "WAGO_RV64_SPEC_JSON_DIR", false)
}
func TestRV64PublicPinnedRelaxedSIMDSpecExecution(t *testing.T) {
	rv64PublicSpecExecution(t, "WAGO_RV64_RELAXED_SPEC_JSON_DIR", true)
}

type rv64PublicSpecInstance struct {
	compiled *Compiled
	instance *Instance
}

func (in *rv64PublicSpecInstance) close() error {
	if in == nil {
		return nil
	}
	// Synchronous calls have returned. Retain the compiled owner on an instance
	// close failure; it must not be disposed underneath remaining native state.
	if in.instance != nil {
		if err := in.instance.Close(); err != nil {
			return err
		}
		in.instance = nil
	}
	if in.compiled != nil {
		if err := in.compiled.Close(); err != nil {
			return err
		}
		in.compiled = nil
	}
	return nil
}
func rv64PublicSpecInstantiate(t *testing.T, source []byte, artifact bool) (*rv64PublicSpecInstance, error) {
	t.Helper()
	in := &rv64PublicSpecInstance{}
	complete := false
	defer func() {
		if !complete {
			if err := in.close(); err != nil {
				t.Error("partial public spec cleanup", err)
			}
		}
	}()
	var err error
	in.compiled, err = Compile(NewRuntimeConfig().WithNativeStackBytes(MinNativeStackBytes), source)
	if err != nil {
		return nil, err
	}
	if in.compiled.codeCache.base != 0 || in.compiled.requiredAMD64Features != 0 {
		t.Fatal("source compilation published code or selected a foreign ISA")
	}
	if artifact {
		blob, err := in.compiled.MarshalBinary()
		if err != nil {
			t.Fatal("marshal pinned module", err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			if loaded != nil {
				if cleanup := loaded.Close(); cleanup != nil {
					t.Error("partial artifact cleanup", cleanup)
				}
			}
			t.Fatal("load pinned module", err)
		}
		if err := in.compiled.Close(); err != nil {
			if cleanup := loaded.Close(); cleanup != nil {
				t.Error("loaded artifact cleanup", cleanup)
			}
			t.Fatal(err)
		}
		in.compiled = loaded
		// The streaming codec intentionally retains a readable/writable image.
		// Callable publication happens only after activation seals it and
		// synchronizes the Linux process-wide instruction cache.
		if loaded.codeCache.base == 0 || loaded.codeCache.sealed || loaded.codeCache.flags&compiledCacheWritableCode == 0 || loaded.requiredAMD64Features != 0 {
			t.Fatal("artifact did not retain its pending RW baseline image")
		}
	}
	if in.compiled.NumImports != 0 || in.compiled.memoryCount() > 1 {
		t.Fatal("unexpected public fixture resource shape")
	}
	if in.compiled.memoryCount() == 1 && in.compiled.memoryDef(0).Min > 16 {
		t.Fatal("pinned fixture exceeds the 16-page initial backing budget")
	}
	// One live module at a time; memory32 reserves virtual space up to the source
	// ceiling, while only its bounded initial pages are backed.
	in.instance, err = Instantiate(in.compiled, InstantiateOptions{nativeStackBytes: MinNativeStackBytes})
	if err != nil {
		t.Fatal("instantiate pinned module", err)
	}
	if in.instance.eng.StackBytes() != MinNativeStackBytes {
		t.Fatal("public spec native stack budget was not applied")
	}
	complete = true
	return in, nil
}
func (in *rv64PublicSpecInstance) invoke(t *testing.T, command simdfixture.Command) {
	t.Helper()
	if command.Action.Type != "invoke" {
		t.Fatal("unsupported public spec action", command.Action.Type)
	}
	params, results, err := in.compiled.Signature(command.Action.Field)
	patterns, patternErr := simdfixture.Patterns(command)
	if err != nil || patternErr != nil || len(params) != len(command.Action.Args) || len(patterns) == 0 {
		t.Fatal("public spec action/signature mismatch", command.Action.Field, err, patternErr)
	}
	for _, pattern := range patterns {
		if len(results) != len(pattern) {
			t.Fatal("public spec result arity mismatch", command.Action.Field)
		}
		for i, value := range pattern {
			// Trap JSON specifies types without values or lane types.
			if results[i].String() != value.Type {
				t.Fatal("public spec result type mismatch", i)
			}
		}
	}
	args := make([]uint64, 0, 256)
	for i, value := range command.Action.Args {
		if params[i].String() != value.Type {
			t.Fatal("public spec parameter type mismatch", i)
		}
		raw, err := simdfixture.Input(value)
		if err != nil || len(raw)%8 != 0 || len(raw)/8 > 256-len(args) {
			t.Fatal("public spec argument encoding/bound", i, err)
		}
		offset := len(args)
		for j := 0; j < len(raw); j += 8 {
			args = append(args, binary.LittleEndian.Uint64(raw[j:]))
		}
		if value.Type == "i32" || value.Type == "f32" {
			args[offset] |= 0xdeadbeef00000000
		}
	}
	resultCells := 0
	for _, typ := range results {
		resultCells++
		if typ == ValV128 {
			resultCells++
		}
	}
	if resultCells > 256 {
		t.Fatal("public spec compact result bound")
	}
	got, err := in.instance.Invoke(command.Action.Field, args...)
	if command.Type == "assert_trap" {
		var trap *coreruntime.TrapError
		if command.Text != "out of bounds memory access" || !errors.As(err, &trap) || trap.Code != coreruntime.TrapLinMemOutOfBounds || got != nil {
			t.Fatalf("public spec trap line %d %s: want %q and no result, got %v / %v", command.Line, command.Action.Field, command.Text, err, got)
		}
		return
	}
	if err != nil || len(got) != resultCells {
		t.Fatalf("public spec return line %d %s: cells %d want %d, error %v", command.Line, command.Action.Field, len(got), resultCells, err)
	}
	raw := make([]byte, len(got)*8)
	for i, cell := range got {
		binary.LittleEndian.PutUint64(raw[i*8:], cell)
	}
	if err := simdfixture.MatchPatterns(raw, patterns); err != nil {
		t.Fatalf("public spec line %d %s: %v", command.Line, command.Action.Field, err)
	}
}
func rv64PublicSpecExecution(t *testing.T, variable string, relaxed bool) {
	t.Helper()
	dir := os.Getenv(variable)
	if dir == "" {
		t.Skip("prepare repository-pinned fixtures with scripts/prepare-riscv64-simd-spec.py and set " + variable)
	}
	manifest, err := simdfixture.LoadManifest(dir, relaxed)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, 0, len(manifest.Sources))
	for name := range manifest.Sources {
		if !strings.HasSuffix(name, ".wast") {
			t.Fatal("unexpected public source fixture", name)
		}
		files = append(files, strings.TrimSuffix(name, ".wast")+".json")
	}
	sort.Strings(files)
	for _, artifact := range []bool{false, true} {
		mode := "source"
		if artifact {
			mode = "trusted-artifact"
		}
		t.Run(mode, func(t *testing.T) {
			total := make(map[string]int)
			// Exactly these three state/linking-only modules are outside this bounded
			// public execution milestone. Any other rejection or changed reason fails.
			exclusions := map[string]string{
				"simd_const.112.wasm": "function count outside 1..1024",
				"simd_linking.0.wasm": "SWAR module state/features/function bound",
				// Its imported vectors now select SWAR; it still defines no functions.
				"simd_linking.1.wasm": "SWAR module state/features/function bound",
			}
			want := map[string]int{"modules": 469, "excluded_modules": 3, "assert_return": 24273, "assert_trap": 54, "excluded_assert_invalid": 669, "excluded_assert_malformed": 510, "excluded_register": 1}
			if relaxed {
				exclusions = map[string]string{}
				want = map[string]int{"modules": 8, "assert_return": 69, "assert_return_either": 32}
			}
			seenExcluded := make(map[string]bool)
			for _, file := range files {
				t.Run(strings.TrimSuffix(file, ".json"), func(t *testing.T) {
					data, err := manifest.Read(dir, file, 16<<20)
					if err != nil {
						t.Fatal(err)
					}
					var document simdfixture.Document
					if err := simdfixture.Decode(data, &document); err != nil {
						t.Fatal(err)
					}
					if document.SourceFilename != strings.TrimSuffix(file, ".json")+".wast" {
						t.Fatal("incorrect public source filename", document.SourceFilename)
					}
					counts := make(map[string]int)
					var current *rv64PublicSpecInstance
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
							body, err := manifest.Read(dir, command.Filename, railshot.MaxModuleBytes)
							if err != nil {
								t.Fatal(err)
							}
							current, excluded = rv64PublicSpecInstantiate(t, body, artifact)
							if excluded != nil {
								reason, listed := exclusions[command.Filename]
								if !listed || seenExcluded[command.Filename] || !errors.Is(excluded, railshot.ErrUnsupported) || excluded.Error() != "compile: "+railshot.ErrUnsupported.Error()+": "+reason {
									t.Fatalf("valid public spec module %s: %v", command.Filename, excluded)
								}
								seenExcluded[command.Filename] = true
								counts["excluded_modules"]++
								t.Logf("excluded public module %s: %v", command.Filename, excluded)
							} else {
								if _, listed := exclusions[command.Filename]; listed {
									t.Fatal("public policy widened; independently review fixture coverage", command.Filename)
								}
								counts["modules"]++
							}
						case "assert_return", "assert_trap":
							if excluded != nil || current == nil {
								t.Fatal("public action without an admitted current module", command.Line, excluded)
							}
							current.invoke(t, command)
							counts[command.Type]++
							if len(command.Either) != 0 {
								counts["assert_return_either"]++
							}
						case "assert_invalid", "assert_malformed", "register":
							counts["excluded_"+command.Type]++
						default:
							t.Fatal("unexpected pinned public spec command", command.Type)
						}
					}
					for kind, n := range counts {
						total[kind] += n
					}
					t.Logf("public coverage %s %v", file, counts)
				})
			}
			t.Logf("public %s pinned spec %s, WABT %s; execution/exclusion coverage %v", mode, manifest.SpecCommit, manifest.WabtVersion, total)
			if !reflect.DeepEqual(total, want) || len(seenExcluded) != len(exclusions) {
				t.Fatal("pinned public execution coverage changed", total, "want", want, "exclusions", seenExcluded)
			}
		})
	}
}
