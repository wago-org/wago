//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/simdfixture"
)

// These opt-in suites qualify execution through the public API and trusted
// artifacts. Binary invalid modules must fail in decoding/validation, never
// merely in RV64 admission. Text syntax assertions remain explicit exclusions:
// the public API does not implement the WAT frontend.
func TestRV64PublicPinnedSIMDSpecExecution(t *testing.T) {
	rv64PublicSpecExecution(t, "WAGO_RV64_SPEC_JSON_DIR", false)
}
func TestRV64PublicPinnedRelaxedSIMDSpecExecution(t *testing.T) {
	rv64PublicSpecExecution(t, "WAGO_RV64_RELAXED_SPEC_JSON_DIR", true)
}

type rv64PublicSpecInstance struct {
	compiled *Compiled
	instance *Instance
	globals  []*Global
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
	for _, global := range in.globals {
		if err := global.Close(); err != nil {
			return err
		}
	}
	in.globals = nil
	if in.compiled != nil {
		if err := in.compiled.Close(); err != nil {
			return err
		}
		in.compiled = nil
	}
	return nil
}
func rv64PublicSpecInstantiate(t *testing.T, source []byte, artifact bool, registered map[string]*rv64PublicSpecInstance) (*rv64PublicSpecInstance, error) {
	t.Helper()
	return rv64PublicSpecInstantiatePolicy(t, source, artifact, registered, rv64PublicSpecDefaultPolicy())
}

// Each corpus selects its resource/feature envelope explicitly. Per-memory and
// aggregate bounds are checked before instantiation backs any linear memory.
type rv64PublicSpecPolicy struct {
	features                              CoreFeatures
	maxMemories                           int
	maxInitialPages, maxTotalInitialPages uint64
	memory64                              bool
}

func rv64PublicSpecDefaultPolicy() rv64PublicSpecPolicy {
	return rv64PublicSpecPolicy{maxMemories: 1, maxInitialPages: 16, maxTotalInitialPages: 16}
}

func (p rv64PublicSpecPolicy) config() *RuntimeConfig {
	cfg := NewRuntimeConfig().WithNativeStackBytes(MinNativeStackBytes)
	if p.features != 0 {
		cfg = cfg.WithCoreFeatures(p.features)
	}
	return cfg
}

func (p rv64PublicSpecPolicy) checkMemories(c *Compiled) error {
	if c.memoryCount() > p.maxMemories {
		return fmt.Errorf("pinned fixture exceeds memory count budget %d", p.maxMemories)
	}
	remaining := p.maxTotalInitialPages
	for i := 0; i < c.memoryCount(); i++ {
		def := c.memoryDef(i)
		if def.Addr64 != p.memory64 || def.Shared || def.Min > p.maxInitialPages || def.Min > remaining {
			return fmt.Errorf("pinned fixture memory %d exceeds address-width or initial-page budget", i)
		}
		remaining -= def.Min
	}
	return nil
}

func rv64PublicSpecInstantiatePolicy(t *testing.T, source []byte, artifact bool, registered map[string]*rv64PublicSpecInstance, policy rv64PublicSpecPolicy) (*rv64PublicSpecInstance, error) {
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
	in.compiled, err = Compile(policy.config(), source)
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
		if loaded.requiredAMD64Features != 0 {
			t.Fatal("artifact selected a foreign ISA")
		}
		if loaded.hasNoFunctionCode() {
			if loaded.codeCache.base != 0 || loaded.codeCache.mem != nil || loaded.codeCache.sealed || loaded.codeCache.flags&compiledCacheWritableCode != 0 {
				t.Fatal("state-only artifact allocated or published a code mapping")
			}
		} else if loaded.codeCache.base == 0 || loaded.codeCache.sealed || loaded.codeCache.flags&compiledCacheWritableCode == 0 {
			t.Fatal("artifact did not retain its pending RW baseline image")
		}
	}
	if in.compiled.NumImports != 0 || in.compiled.memoryImportCount() != 0 || in.compiled.tableImportCount() != 0 || len(in.compiled.GlobalImports) > 16 {
		t.Fatal("unexpected public fixture resource shape")
	}
	if err := policy.checkMemories(in.compiled); err != nil {
		t.Fatal(err)
	}
	var imports *Imports
	if len(in.compiled.GlobalImports) != 0 {
		imports = NewImports()
		for _, imp := range in.compiled.GlobalImports {
			owner := registered[imp.Module]
			if owner == nil || owner.instance == nil {
				return nil, fmt.Errorf("unregistered public spec module %q", imp.Module)
			}
			global, err := owner.instance.ExportedGlobalObject(imp.Name)
			if err != nil {
				return nil, err
			}
			in.globals = append(in.globals, global)
			imports.Global(imp.Module, imp.Name, global)
		}
	}
	// Only the current module and explicitly registered owners stay live.
	// Memory32 backs only the bounded initial pages.
	in.instance, err = Instantiate(in.compiled, InstantiateOptions{nativeStackBytes: MinNativeStackBytes, Imports: imports})
	if err != nil {
		t.Fatal("instantiate pinned module", err)
	}
	if in.instance.eng.StackBytes() != MinNativeStackBytes {
		t.Fatal("public spec native stack budget was not applied")
	}
	if in.compiled.hasNoFunctionCode() && (in.compiled.codeCache.base != 0 || in.compiled.codeCache.mem != nil || in.compiled.codeCache.sealed || in.compiled.codeCache.flags&compiledCacheWritableCode != 0) {
		t.Fatal("state-only activation allocated or published a code mapping")
	}
	complete = true
	return in, nil
}
func (in *rv64PublicSpecInstance) invoke(t *testing.T, command simdfixture.Command, overflowTrap coreruntime.TrapCode) {
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
		want, known := rv64PublicSpecTrapCode(command.Text, overflowTrap)
		if !known || !errors.As(err, &trap) || trap.Code != want || got != nil {
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
	want := map[string]int{"modules": 472, "register": 1, "assert_return": 24273, "assert_trap": 54, "assert_invalid": 669, "assert_invalid_decode": 1, "assert_invalid_validate": 668, "excluded_text_assert_malformed": 510}
	if relaxed {
		want = map[string]int{"modules": 8, "assert_return": 69, "assert_return_either": 32}
	}
	rv64PublicSpecRun(t, dir, manifest, want)
}

func rv64PublicSpecRun(t *testing.T, dir string, manifest *simdfixture.Manifest, want map[string]int) {
	t.Helper()
	rv64PublicSpecRunPolicy(t, dir, manifest, want, rv64PublicSpecDefaultPolicy())
}

func rv64PublicSpecRunPolicy(t *testing.T, dir string, manifest *simdfixture.Manifest, want map[string]int, policy rv64PublicSpecPolicy) {
	t.Helper()
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
			// Every valid pinned module must compile and instantiate.
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
					// The conversion corpus uses the float-to-int overflow trap;
					// integer arithmetic files use signed division overflow.
					overflowTrap := coreruntime.TrapDivOverflow
					if file == "conversions.json" {
						overflowTrap = coreruntime.TrapTruncOverflow
					}
					counts := make(map[string]int)
					var current *rv64PublicSpecInstance
					registered := make(map[string]*rv64PublicSpecInstance)
					var registeredOwners []*rv64PublicSpecInstance
					defer func() {
						if err := current.close(); err != nil {
							t.Error(err)
							return // Do not close providers under a still-live consumer.
						}
						for i := len(registeredOwners) - 1; i >= 0; i-- {
							if err := registeredOwners[i].close(); err != nil {
								t.Error(err)
								return
							}
						}
					}()
					for _, command := range document.Commands {
						switch command.Type {
						case "module":
							retained := false
							for _, owner := range registeredOwners {
								retained = retained || owner == current
							}
							if !retained {
								if err := current.close(); err != nil {
									t.Fatal(err)
								}
							}
							current = nil
							body, err := manifest.Read(dir, command.Filename, railshot.MaxModuleBytes)
							if err != nil {
								t.Fatal(err)
							}
							current, err = rv64PublicSpecInstantiatePolicy(t, body, artifact, registered, policy)
							if err != nil {
								t.Fatalf("valid public spec module %s: %v", command.Filename, err)
							}
							counts["modules"]++
						case "assert_return", "assert_trap":
							if current == nil {
								t.Fatal("public action without an admitted current module", command.Line)
							}
							current.invoke(t, command, overflowTrap)
							counts[command.Type]++
							if len(command.Either) != 0 {
								counts["assert_return_either"]++
							}
						case "assert_invalid":
							if command.ModuleType != "binary" {
								t.Fatal("unexpected invalid fixture representation", command.ModuleType)
							}
							body, err := manifest.Read(dir, command.Filename, railshot.MaxModuleBytes)
							if err != nil {
								t.Fatal(err)
							}
							stage := rv64PublicSpecAssertInvalid(t, body, command, policy)
							counts["assert_invalid_"+stage]++
							counts[command.Type]++
						case "assert_malformed":
							if command.ModuleType != "text" {
								t.Fatal("unexpected malformed fixture representation", command.ModuleType)
							}
							if _, err := manifest.Read(dir, command.Filename, railshot.MaxModuleBytes); err != nil {
								t.Fatal(err)
							}
							counts["excluded_text_assert_malformed"]++
						case "register":
							if command.As == "" || registered[command.As] != nil || len(registeredOwners) >= 1 {
								t.Fatal("unexpected pinned public registration", command.As)
							}
							if current == nil {
								t.Fatal("registration without a current module")
							}
							registered[command.As] = current
							registeredOwners = append(registeredOwners, current)
							counts[command.Type]++
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
			if !reflect.DeepEqual(total, want) {
				t.Fatal("pinned public execution coverage changed", total, "want", want)
			}
		})
	}
}

// A target-profile or resource-limit rejection is not a validation assertion.
// Match the pinned source diagnostic to a specific frontend error category.
func rv64PublicSpecAssertInvalid(t *testing.T, source []byte, command simdfixture.Command, policy rv64PublicSpecPolicy) string {
	t.Helper()
	compiled, err := Compile(policy.config(), source)
	if compiled != nil {
		if cleanup := compiled.Close(); cleanup != nil {
			t.Error(cleanup)
		}
		t.Fatal("invalid pinned module produced compiled code", command.Filename)
	}
	if mismatch := rv64PublicSpecInvalidError(command.Text, err); mismatch != nil {
		t.Fatalf("invalid pinned module %s line %d: %v", command.Filename, command.Line, mismatch)
	}
	var decode *wasm.DecodeError
	decodedRejection := errors.As(err, &decode)
	// The pinned shuffle with an out-of-range immediate is the sole invalid
	// binary rejected by decoding. A different decoder failure is a regression.
	if decodedRejection != (command.Filename == "simd_lane.96.wasm") {
		t.Fatalf("invalid pinned module %s rejected at unexpected stage: %v", command.Filename, err)
	}
	if decodedRejection {
		// This manifest-pinned binary has one code body starting at byte 23.
		// Its shuffle starts at byte 60; the final lane is 255 at byte 77.
		// DecodeError.Offset is relative to that body, not the whole module.
		if len(source) != 79 || source[19] != 10 || source[20] != 58 ||
			source[21] != 1 || source[22] != 56 || source[23] != 0 ||
			source[60] != 0xfd || source[61] != 13 || source[77] != 255 || source[78] != 0x0b {
			t.Fatal("pinned invalid shuffle instruction/source layout changed")
		}
		for lane := 0; lane < 15; lane++ {
			if source[62+lane] != byte(lane) {
				t.Fatal("pinned shuffle has an earlier invalid or changed lane", lane)
			}
		}
		return "decode"
	}
	return "validate"
}

func rv64PublicSpecInvalidError(text string, err error) error {
	want := wasm.ErrTypeMismatch
	switch text {
	case "type mismatch":
	case "invalid lane index":
		// Shuffle lane bytes are rejected by decoding; other lane bounds are
		// checked by validation. Both reject the specified invalid immediate.
		var decode *wasm.DecodeError
		if errors.As(err, &decode) && decode.Code == wasm.ErrInvalidInstruction &&
			decode.Offset == 54 && decode.SectionID == 10 && decode.SectionStart == 21 && decode.SectionEnd == 79 {
			return nil
		}
	case "alignment must not be larger than natural":
		want = wasm.ErrInvalidAlignment
	case "unknown local 2", "unknown local":
		want = wasm.ErrUnknownLocal
	case "unknown label":
		want = wasm.ErrUnknownLabel
	default:
		return fmt.Errorf("unknown pinned validation diagnostic %q", text)
	}
	var validation *wasm.ValidationError
	if !errors.As(err, &validation) || validation.Code != want {
		return fmt.Errorf("want validation %s for %q, got %v", want, text, err)
	}
	if text == "invalid lane index" && validation.Detail != "simd lane out of range" {
		return fmt.Errorf("lane assertion failed for unrelated type error: %v", err)
	}
	return nil
}

func rv64PublicSpecTrapCode(text string, overflowTrap coreruntime.TrapCode) (coreruntime.TrapCode, bool) {
	switch text {
	case "out of bounds memory access":
		return coreruntime.TrapLinMemOutOfBounds, true
	case "integer divide by zero":
		return coreruntime.TrapDivZero, true
	case "undefined element":
		return coreruntime.TrapIndirectOutOfBounds, true
	case "integer overflow":
		return overflowTrap, overflowTrap == coreruntime.TrapDivOverflow || overflowTrap == coreruntime.TrapTruncOverflow
	case "invalid conversion to integer":
		return coreruntime.TrapTruncOverflow, true
	default:
		return 0, false
	}
}
