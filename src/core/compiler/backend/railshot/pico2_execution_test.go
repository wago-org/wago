package railshot

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/arm32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/internal/qemu32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type pico2ExecutionScript struct {
	Commands []pico2ExecutionCommand `json:"commands"`
}

type pico2ExecutionCommand struct {
	Type     string                `json:"type"`
	Filename string                `json:"filename"`
	Name     string                `json:"name"`
	As       string                `json:"as"`
	Line     int                   `json:"line"`
	Text     string                `json:"text"`
	Action   *pico2ExecutionAction `json:"action"`
	Expected []pico2ExecutionValue `json:"expected"`
}

type pico2ExecutionAction struct {
	Type   string                `json:"type"`
	Module string                `json:"module"`
	Field  string                `json:"field"`
	Args   []pico2ExecutionValue `json:"args"`
}

type pico2ExecutionValue struct {
	Type     string          `json:"type"`
	LaneType string          `json:"lane_type"`
	Value    json.RawMessage `json:"value"`
}

type pico2QEMUTargetModule struct {
	name     string
	compiled *shared.EmbeddedModule
	image    *shared.EmbeddedFirmwareImage
	client   *qemu32.Client
}

type pico2QEMUModule struct {
	arm *pico2QEMUTargetModule
	rv  *pico2QEMUTargetModule
}

type pico2QEMUScriptRuntime struct {
	modules   map[int]*pico2QEMUModule
	armClient *qemu32.Client
	rvClient  *qemu32.Client
}

func (r *pico2QEMUScriptRuntime) close() {
	if r == nil {
		return
	}
	if r.armClient != nil {
		_ = r.armClient.Close()
	}
	if r.rvClient != nil {
		_ = r.rvClient.Close()
	}
}

func TestPico2Release2Execution(t *testing.T) {
	checkout := os.Getenv("WAGO_PICO2_SPECTEST_DIR")
	if checkout == "" {
		t.Skip("WAGO_PICO2_SPECTEST_DIR is not set")
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	qemuArm, err := exec.LookPath("qemu-arm")
	if err != nil {
		t.Skip("qemu-arm not on PATH")
	}
	qemuRV, err := exec.LookPath("qemu-riscv32")
	if err != nil {
		t.Skip("qemu-riscv32 not on PATH")
	}
	suite, err := spectest.DiscoverRelease2(checkout)
	if err != nil {
		t.Fatal(err)
	}
	filter := os.Getenv("WAGO_PICO2_SPECTEST_FILE")
	tmp := t.TempDir()
	modules, assertions, resources := 0, 0, 0
	for _, base := range suite.Files {
		if filter != "" && base != filter {
			continue
		}
		base := base
		t.Run(strings.ReplaceAll(base, string(filepath.Separator), "_"), func(t *testing.T) {
			jsonPath := filepath.Join(tmp, strings.ReplaceAll(base, string(filepath.Separator), "_")+".json")
			wast := filepath.Join(suite.CoreDir, base+".wast")
			if output, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
				t.Fatalf("wast2json: %v: %s", err, output)
			}
			raw, err := os.ReadFile(jsonPath)
			if err != nil {
				t.Fatal(err)
			}
			var script pico2ExecutionScript
			if err := json.Unmarshal(raw, &script); err != nil {
				t.Fatal(err)
			}
			runtime, resource, err := startPico2QEMUScript(t, tmp, &script, qemuArm, qemuRV)
			if resource {
				resources++
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.close()
			modules += len(runtime.modules)
			byName := make(map[string]*pico2QEMUModule)
			var current *pico2QEMUModule
			for commandIndex := range script.Commands {
				command := &script.Commands[commandIndex]
				switch command.Type {
				case "module":
					current = runtime.modules[commandIndex]
					if current == nil {
						t.Fatalf("line %d module image is unavailable", command.Line)
					}
					if err := instantiatePico2Module(current); err != nil {
						t.Fatalf("line %d module start: %v", command.Line, err)
					}
					if command.Name != "" {
						byName[command.Name] = current
					}
				case "register":
					module := current
					if command.Name != "" {
						module = byName[command.Name]
					}
					if module == nil {
						t.Fatalf("line %d register references unavailable module %q", command.Line, command.Name)
					}
					byName[command.As] = module
				case "action", "assert_return", "assert_trap", "assert_exhaustion":
					if current == nil {
						continue
					}
					module := current
					if command.Action != nil && command.Action.Module != "" {
						module = byName[command.Action.Module]
					}
					if module == nil {
						t.Fatalf("line %d action references unavailable module", command.Line)
					}
					armResults, armTrap, rvResults, rvTrap, err := invokePico2QEMU(module, command.Action)
					if err != nil {
						t.Fatalf("line %d invoke: %v", command.Line, err)
					}
					assertions++
					if armTrap != rvTrap {
						t.Errorf("line %d target trap mismatch: arm=%d rv=%d", command.Line, armTrap, rvTrap)
						continue
					}
					switch command.Type {
					case "assert_trap", "assert_exhaustion":
						want, ok := pico2ExpectedTrap(command.Text)
						if !ok {
							t.Fatalf("line %d unsupported trap text %q", command.Line, command.Text)
						}
						if armTrap != want {
							t.Errorf("line %d trap=%d want=%d (%s)", command.Line, armTrap, want, command.Text)
						}
					default:
						if armTrap != embedded32.TrapNone {
							t.Errorf("line %d unexpected trap=%d", command.Line, armTrap)
							continue
						}
						if err := comparePico2Expected(armResults, command.Expected); err != nil {
							t.Errorf("line %d arm result: %v", command.Line, err)
						}
						if err := comparePico2Expected(rvResults, command.Expected); err != nil {
							t.Errorf("line %d rv result: %v", command.Line, err)
						}
					}
				case "assert_uninstantiable":
					module := runtime.modules[commandIndex]
					if module == nil {
						t.Fatalf("line %d uninstantiable module image is unavailable", command.Line)
					}
					armTrap, rvTrap, err := instantiatePico2FailingModule(module)
					if err != nil {
						t.Fatalf("line %d uninstantiable module: %v", command.Line, err)
					}
					assertions++
					want, ok := pico2ExpectedTrap(command.Text)
					if !ok {
						t.Fatalf("line %d unsupported instantiation trap %q", command.Line, command.Text)
					}
					if armTrap != rvTrap || armTrap != want {
						t.Errorf("line %d instantiation traps arm=%d rv=%d want=%d", command.Line, armTrap, rvTrap, want)
					}
				case "assert_malformed", "assert_invalid", "assert_unlinkable":
					// Strict rejection and unlinkable admission are covered by the compile gate.
				default:
					t.Fatalf("line %d unsupported command %q", command.Line, command.Type)
				}
			}
		})
	}
	if filter != "" && modules == 0 && resources == 0 {
		t.Fatalf("execution filter %q did not select a module", filter)
	}
	t.Logf("Pico 2 Release 2 execution: modules=%d assertions=%d bounded-resource=%d", modules, assertions, resources)
}

func pico2PassiveInstantiationClone(module *shared.EmbeddedModule) *shared.EmbeddedModule {
	clone := *module
	clone.Data = append([]shared.EmbeddedDataSegment(nil), module.Data...)
	for i := range clone.Data {
		if !clone.Data[i].Passive {
			clone.Data[i].Passive = true
		}
	}
	clone.Elements = append([]shared.EmbeddedElementSegment(nil), module.Elements...)
	if module.Elements == nil && module.Table != nil {
		clone.Elements = append([]shared.EmbeddedElementSegment(nil), module.Table.Elements...)
	}
	for i := range clone.Elements {
		if clone.Elements[i].Mode == shared.EmbeddedElementActive {
			clone.Elements[i].Mode = shared.EmbeddedElementPassive
		}
	}
	clone.Tables = append([]shared.EmbeddedTable(nil), module.Tables...)
	for i := range clone.Tables {
		clone.Tables[i].Elements = nil
	}
	if len(clone.Tables) == 1 {
		clone.Tables[0].Elements = clone.Elements
		clone.Table = &clone.Tables[0]
	} else if len(clone.Tables) > 1 {
		compat := clone.Tables[0]
		compat.Elements = clone.Elements
		clone.Table = &compat
	} else if len(clone.Elements) != 0 {
		clone.Table = &shared.EmbeddedTable{Reference: wasm.FuncRef.Ref, Elements: clone.Elements}
	} else {
		clone.Table = nil
	}
	return &clone
}

func startPico2QEMUScript(t *testing.T, directory string, script *pico2ExecutionScript, qemuArm, qemuRV string) (*pico2QEMUScriptRuntime, bool, error) {
	t.Helper()
	type moduleSpec struct {
		commandIndex   int
		commandName    string
		linkName       string
		uninstantiable bool
		module         *wasm.Module
		arm            *shared.EmbeddedModule
		rv             *shared.EmbeddedModule
		armImage       *shared.EmbeddedModule
		rvImage        *shared.EmbeddedModule
	}
	var specs []*moduleSpec
	byCommandName := make(map[string]*moduleSpec)
	var current *moduleSpec
	needsSpectest := false
	for commandIndex := range script.Commands {
		command := &script.Commands[commandIndex]
		switch command.Type {
		case "module", "assert_uninstantiable":
			moduleBytes, err := os.ReadFile(filepath.Join(directory, command.Filename))
			if err != nil {
				return nil, false, err
			}
			module, err := wasm.DecodeModule(moduleBytes)
			if err != nil {
				return nil, false, err
			}
			if pico2OutsideCompletionGate(module) {
				return nil, true, nil
			}
			armCompiled, armErr := arm32.CompileModule(module)
			rvCompiled, rvErr := riscv32.CompileModule(module)
			if (armErr == nil) != (rvErr == nil) {
				return nil, false, fmt.Errorf("line %d target admission differs: arm=%v rv=%v", command.Line, armErr, rvErr)
			}
			if armErr != nil {
				if pico2BoundedResourceRejection(armErr) && pico2BoundedResourceRejection(rvErr) {
					return nil, true, nil
				}
				return nil, false, fmt.Errorf("line %d: %w", command.Line, armErr)
			}
			uninstantiable := command.Type == "assert_uninstantiable"
			spec := &moduleSpec{commandIndex: commandIndex, commandName: command.Name, linkName: fmt.Sprintf("pico2.module.%d", len(specs)), uninstantiable: uninstantiable, module: module, arm: armCompiled, rv: rvCompiled, armImage: armCompiled, rvImage: rvCompiled}
			if uninstantiable {
				spec.armImage = pico2PassiveInstantiationClone(armCompiled)
				spec.rvImage = pico2PassiveInstantiationClone(rvCompiled)
			}
			if command.Name != "" {
				byCommandName[command.Name] = spec
			}
			for i := range module.Imports {
				if module.Imports[i].Module == "spectest" {
					needsSpectest = true
				}
			}
			specs = append(specs, spec)
			if !uninstantiable {
				current = spec
			}
		case "register":
			provider := current
			if command.Name != "" {
				provider = byCommandName[command.Name]
			}
			if provider == nil {
				return nil, false, fmt.Errorf("line %d register provider %q is unavailable", command.Line, command.Name)
			}
			provider.linkName = command.As
		}
	}
	if len(specs) == 0 {
		return &pico2QEMUScriptRuntime{modules: make(map[int]*pico2QEMUModule)}, false, nil
	}
	known := make(map[string]bool, len(specs)+1)
	if needsSpectest {
		known["spectest"] = true
	}
	for _, spec := range specs {
		if known[spec.linkName] {
			return nil, false, fmt.Errorf("duplicate script link name %q", spec.linkName)
		}
		known[spec.linkName] = true
	}
	for _, spec := range specs {
		for i := range spec.module.Imports {
			if !known[spec.module.Imports[i].Module] {
				return nil, false, fmt.Errorf("module %q imports unavailable provider %q", spec.linkName, spec.module.Imports[i].Module)
			}
		}
	}
	var armNamed, rvNamed []shared.EmbeddedNamedModule
	var armOptions, rvOptions []shared.EmbeddedFirmwareOptions
	providerCount := 0
	if needsSpectest {
		provider := pico2SpectestProviderModule(t)
		armProvider, err := arm32.CompileModule(provider)
		if err != nil {
			return nil, false, err
		}
		rvProvider, err := riscv32.CompileModule(provider)
		if err != nil {
			return nil, false, err
		}
		armNamed = append(armNamed, shared.EmbeddedNamedModule{Name: "spectest", Module: armProvider})
		rvNamed = append(rvNamed, shared.EmbeddedNamedModule{Name: "spectest", Module: rvProvider})
		armOptions = append(armOptions, pico2ExecutionFirmwareOptions(armProvider, qemu32.ArmHelpers()))
		rvOptions = append(rvOptions, pico2ExecutionFirmwareOptions(rvProvider, qemu32.RVHelpers()))
		providerCount = 1
	}
	for _, spec := range specs {
		armNamed = append(armNamed, shared.EmbeddedNamedModule{Name: spec.linkName, Module: spec.armImage})
		rvNamed = append(rvNamed, shared.EmbeddedNamedModule{Name: spec.linkName, Module: spec.rvImage})
		armOptions = append(armOptions, pico2ExecutionFirmwareOptions(spec.armImage, qemu32.ArmHelpers()))
		rvOptions = append(rvOptions, pico2ExecutionFirmwareOptions(spec.rvImage, qemu32.RVHelpers()))
	}
	armPlan, err := shared.ResolveEmbeddedLinksWithOptions(armNamed, shared.EmbeddedLinkOptions{AllowRuntimeGrownLimits: true})
	if err != nil {
		return nil, false, fmt.Errorf("arm script links: %w", err)
	}
	rvPlan, err := shared.ResolveEmbeddedLinksWithOptions(rvNamed, shared.EmbeddedLinkOptions{AllowRuntimeGrownLimits: true})
	if err != nil {
		return nil, false, fmt.Errorf("rv script links: %w", err)
	}
	armLinkedOptions := arm32.LinkedFirmwareOptions{BaseAddress: qemu32.ImageBase, Modules: armOptions, DeferImportedActiveSegments: true}
	rvLinkedOptions := riscv32.LinkedFirmwareOptions{BaseAddress: qemu32.ImageBase, Modules: rvOptions, DeferImportedActiveSegments: true}
	armSize, armErr := arm32.LinkedFirmwareImageSize(armPlan, armLinkedOptions)
	rvSize, rvErr := riscv32.LinkedFirmwareImageSize(rvPlan, rvLinkedOptions)
	if armErr != nil || rvErr != nil {
		if pico2ExecutionResourceError(armErr) && pico2ExecutionResourceError(rvErr) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("script firmware size: arm=%v rv=%v", armErr, rvErr)
	}
	armBundle, err := arm32.BuildLinkedFirmwareImage(make([]byte, armSize), armPlan, armLinkedOptions)
	if err != nil {
		return nil, false, fmt.Errorf("arm script firmware: %w", err)
	}
	rvBundle, err := riscv32.BuildLinkedFirmwareImage(make([]byte, rvSize), rvPlan, rvLinkedOptions)
	if err != nil {
		return nil, false, fmt.Errorf("rv script firmware: %w", err)
	}
	armELF, _, err := qemu32.ArmELF(armBundle.Bytes)
	if err != nil {
		return nil, false, err
	}
	rvELF, _, err := qemu32.RVELF(rvBundle.Bytes)
	if err != nil {
		return nil, false, err
	}
	dir := t.TempDir()
	armPath, rvPath := filepath.Join(dir, "arm-script.elf"), filepath.Join(dir, "rv-script.elf")
	if err := os.WriteFile(armPath, armELF, 0o755); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(rvPath, rvELF, 0o755); err != nil {
		return nil, false, err
	}
	armClient, err := qemu32.Start(qemuArm, armPath)
	if err != nil {
		return nil, false, err
	}
	rvClient, err := qemu32.Start(qemuRV, rvPath)
	if err != nil {
		_ = armClient.Close()
		return nil, false, err
	}
	runtime := &pico2QEMUScriptRuntime{modules: make(map[int]*pico2QEMUModule, len(specs)), armClient: armClient, rvClient: rvClient}
	for i, spec := range specs {
		armImage := armBundle.Modules[providerCount+i].Image
		rvImage := rvBundle.Modules[providerCount+i].Image
		runtime.modules[spec.commandIndex] = &pico2QEMUModule{
			arm: &pico2QEMUTargetModule{name: "arm32", compiled: spec.arm, image: armImage, client: armClient},
			rv:  &pico2QEMUTargetModule{name: "riscv32", compiled: spec.rv, image: rvImage, client: rvClient},
		}
	}
	return runtime, false, nil
}

func instantiatePico2FailingModule(module *pico2QEMUModule) (embedded32.Trap, embedded32.Trap, error) {
	armTrap, err := instantiatePico2Target(module.arm)
	if err != nil {
		return 0, 0, err
	}
	rvTrap, err := instantiatePico2Target(module.rv)
	return armTrap, rvTrap, err
}

func instantiatePico2Target(module *pico2QEMUTargetModule) (embedded32.Trap, error) {
	readWord := func(address uint32) (uint32, error) {
		words, err := module.client.Read(address, 1)
		if err != nil {
			return 0, err
		}
		return words[0], nil
	}
	resolveOffset := func(offset uint32, hasGlobal bool, global uint32) (uint32, error) {
		if !hasGlobal {
			return offset, nil
		}
		directory, err := readWord(module.image.ContextAddress + embedded32.ContextImportedGlobalsBaseOffset)
		if err != nil {
			return 0, err
		}
		cell, err := readWord(directory + global*4)
		if err != nil {
			return 0, err
		}
		return readWord(cell)
	}
	tables := module.compiled.Tables
	if len(tables) == 0 && module.compiled.Table != nil {
		tables = []shared.EmbeddedTable{*module.compiled.Table}
	}
	elements := module.compiled.Elements
	if elements == nil && module.compiled.Table != nil {
		elements = module.compiled.Table.Elements
	}
	if len(elements) != 0 {
		directory, err := readWord(module.image.ContextAddress + embedded32.ContextTablesBaseOffset)
		if err != nil {
			return 0, err
		}
		descriptors, err := readWord(module.image.ContextAddress + embedded32.ContextElementSegmentsBaseOffset)
		if err != nil {
			return 0, err
		}
		for i := range elements {
			segment := &elements[i]
			if segment.Mode != shared.EmbeddedElementActive {
				continue
			}
			if uint64(segment.Table) >= uint64(len(tables)) {
				return embedded32.TrapTableOutOfBounds, nil
			}
			table, err := readWord(directory + segment.Table*4)
			if err != nil {
				return 0, err
			}
			entries, err := readWord(table + embedded32.TableABIEntriesBaseOffset)
			if err != nil {
				return 0, err
			}
			length, err := readWord(table + embedded32.TableABILengthOffset)
			if err != nil {
				return 0, err
			}
			offset, err := resolveOffset(segment.Offset, segment.HasOffsetGlobal, segment.OffsetGlobal)
			if err != nil {
				return 0, err
			}
			if uint64(offset)+uint64(len(segment.Values)) > uint64(length) {
				return embedded32.TrapTableOutOfBounds, nil
			}
			payload, err := readWord(descriptors + uint32(i)*embedded32.DataSegmentABIBytes + embedded32.DataSegmentBaseOffset)
			if err != nil {
				return 0, err
			}
			values, err := module.client.Read(payload, uint32(len(segment.Values)))
			if err != nil {
				return 0, err
			}
			encoded := make([]byte, len(values)*4)
			for j := range values {
				binary.LittleEndian.PutUint32(encoded[j*4:], values[j])
			}
			if err := module.client.Write(entries+offset*4, encoded); err != nil {
				return 0, err
			}
			if err := module.client.Write(descriptors+uint32(i)*embedded32.DataSegmentABIBytes+embedded32.DataSegmentDroppedOffset, []byte{1, 0, 0, 0}); err != nil {
				return 0, err
			}
		}
	}
	if len(module.compiled.Data) != 0 {
		owner := module.image.ContextAddress
		if module.compiled.MemoryImported {
			var err error
			owner, err = readWord(module.image.ContextAddress + embedded32.ContextLinearMemoryContextOffset)
			if err != nil {
				return 0, err
			}
		}
		base, err := readWord(owner + embedded32.ContextLinearMemoryBaseOffset)
		if err != nil {
			return 0, err
		}
		length, err := readWord(owner + embedded32.ContextLinearMemoryLengthOffset)
		if err != nil {
			return 0, err
		}
		descriptors, err := readWord(module.image.ContextAddress + embedded32.ContextDataSegmentsBaseOffset)
		if err != nil {
			return 0, err
		}
		for i := range module.compiled.Data {
			segment := &module.compiled.Data[i]
			if segment.Passive {
				continue
			}
			offset, err := resolveOffset(segment.Offset, segment.HasOffsetGlobal, segment.OffsetGlobal)
			if err != nil {
				return 0, err
			}
			if uint64(offset)+uint64(len(segment.Bytes)) > uint64(length) {
				return embedded32.TrapMemoryOutOfBounds, nil
			}
			if err := module.client.Write(base+offset, segment.Bytes); err != nil {
				return 0, err
			}
			if err := module.client.Write(descriptors+uint32(i)*embedded32.DataSegmentABIBytes+embedded32.DataSegmentDroppedOffset, []byte{1, 0, 0, 0}); err != nil {
				return 0, err
			}
		}
	}
	if module.image.StartAddress != 0 {
		return module.client.StartFunction(module.image.StartAddress, module.image.ContextAddress)
	}
	return embedded32.TrapNone, nil
}

func instantiatePico2Module(module *pico2QEMUModule) error {
	if err := applyPico2ImportedSegments(module.arm); err != nil {
		return fmt.Errorf("arm instantiation: %w", err)
	}
	if err := applyPico2ImportedSegments(module.rv); err != nil {
		return fmt.Errorf("rv instantiation: %w", err)
	}
	return startPico2ModuleEntries(module)
}

type pico2TargetWrite struct {
	address uint32
	data    []byte
}

func applyPico2ImportedSegments(module *pico2QEMUTargetModule) error {
	var writes []pico2TargetWrite
	readWord := func(address uint32) (uint32, error) {
		words, err := module.client.Read(address, 1)
		if err != nil {
			return 0, err
		}
		return words[0], nil
	}
	resolveOffset := func(offset uint32, hasGlobal bool, global uint32) (uint32, error) {
		if !hasGlobal {
			return offset, nil
		}
		directory, err := readWord(module.image.ContextAddress + embedded32.ContextImportedGlobalsBaseOffset)
		if err != nil {
			return 0, err
		}
		cell, err := readWord(directory + global*4)
		if err != nil {
			return 0, err
		}
		return readWord(cell)
	}
	if module.compiled.MemoryImported {
		owner, err := readWord(module.image.ContextAddress + embedded32.ContextLinearMemoryContextOffset)
		if err != nil {
			return err
		}
		base, err := readWord(owner + embedded32.ContextLinearMemoryBaseOffset)
		if err != nil {
			return err
		}
		length, err := readWord(owner + embedded32.ContextLinearMemoryLengthOffset)
		if err != nil {
			return err
		}
		if module.compiled.Memory != nil && length/embedded32.WasmPageSize < module.compiled.Memory.Minimum {
			return fmt.Errorf("imported memory has %d pages, needs %d", length/embedded32.WasmPageSize, module.compiled.Memory.Minimum)
		}
		descriptors, err := readWord(module.image.ContextAddress + embedded32.ContextDataSegmentsBaseOffset)
		if err != nil {
			return err
		}
		for i := range module.compiled.Data {
			segment := &module.compiled.Data[i]
			if segment.Passive {
				continue
			}
			offset, err := resolveOffset(segment.Offset, segment.HasOffsetGlobal, segment.OffsetGlobal)
			if err != nil {
				return err
			}
			if uint64(offset)+uint64(len(segment.Bytes)) > uint64(length) {
				return fmt.Errorf("active data segment %d exceeds imported memory", i)
			}
			writes = append(writes,
				pico2TargetWrite{address: base + offset, data: append([]byte(nil), segment.Bytes...)},
				pico2TargetWrite{address: descriptors + uint32(i)*embedded32.DataSegmentABIBytes + embedded32.DataSegmentDroppedOffset, data: []byte{1, 0, 0, 0}},
			)
		}
	}
	tables := module.compiled.Tables
	if len(tables) == 0 && module.compiled.Table != nil {
		tables = []shared.EmbeddedTable{*module.compiled.Table}
	}
	if len(tables) != 0 {
		directory, err := readWord(module.image.ContextAddress + embedded32.ContextTablesBaseOffset)
		if err != nil {
			return err
		}
		for i := range tables {
			if !tables[i].Imported {
				continue
			}
			table, err := readWord(directory + uint32(i)*4)
			if err != nil {
				return err
			}
			length, err := readWord(table + embedded32.TableABILengthOffset)
			if err != nil {
				return err
			}
			if length < tables[i].Minimum {
				return fmt.Errorf("imported table %d has %d entries, needs %d", i, length, tables[i].Minimum)
			}
		}
		descriptors, err := readWord(module.image.ContextAddress + embedded32.ContextElementSegmentsBaseOffset)
		if err != nil {
			return err
		}
		elements := module.compiled.Elements
		if elements == nil && module.compiled.Table != nil {
			elements = module.compiled.Table.Elements
		}
		for i := range elements {
			segment := &elements[i]
			if segment.Mode != shared.EmbeddedElementActive || uint64(segment.Table) >= uint64(len(tables)) || !tables[segment.Table].Imported {
				continue
			}
			table, err := readWord(directory + segment.Table*4)
			if err != nil {
				return err
			}
			entries, err := readWord(table + embedded32.TableABIEntriesBaseOffset)
			if err != nil {
				return err
			}
			length, err := readWord(table + embedded32.TableABILengthOffset)
			if err != nil {
				return err
			}
			offset, err := resolveOffset(segment.Offset, segment.HasOffsetGlobal, segment.OffsetGlobal)
			if err != nil {
				return err
			}
			if uint64(offset)+uint64(len(segment.Values)) > uint64(length) {
				return fmt.Errorf("active element segment %d exceeds imported table", i)
			}
			payload, err := readWord(descriptors + uint32(i)*embedded32.DataSegmentABIBytes + embedded32.DataSegmentBaseOffset)
			if err != nil {
				return err
			}
			values, err := module.client.Read(payload, uint32(len(segment.Values)))
			if err != nil {
				return err
			}
			encoded := make([]byte, len(values)*4)
			for j := range values {
				binary.LittleEndian.PutUint32(encoded[j*4:], values[j])
			}
			writes = append(writes,
				pico2TargetWrite{address: entries + offset*4, data: encoded},
				pico2TargetWrite{address: descriptors + uint32(i)*embedded32.DataSegmentABIBytes + embedded32.DataSegmentDroppedOffset, data: []byte{1, 0, 0, 0}},
			)
		}
	}
	for _, write := range writes {
		if err := module.client.Write(write.address, write.data); err != nil {
			return err
		}
	}
	return nil
}

func startPico2ModuleEntries(module *pico2QEMUModule) error {
	if module.arm.image.StartAddress == 0 && module.rv.image.StartAddress == 0 {
		return nil
	}
	if module.arm.image.StartAddress == 0 || module.rv.image.StartAddress == 0 {
		return fmt.Errorf("target start metadata differs")
	}
	armTrap, err := module.arm.client.StartFunction(module.arm.image.StartAddress, module.arm.image.ContextAddress)
	if err != nil {
		return err
	}
	rvTrap, err := module.rv.client.StartFunction(module.rv.image.StartAddress, module.rv.image.ContextAddress)
	if err != nil {
		return err
	}
	if armTrap != rvTrap || armTrap != embedded32.TrapNone {
		return fmt.Errorf("start traps: arm=%d rv=%d", armTrap, rvTrap)
	}
	return nil
}

func pico2SpectestProviderModule(t *testing.T) *wasm.Module {
	t.Helper()
	types := [][]byte{
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.F32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.F64}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.F64, wasm.F64}, nil),
	}
	functions := make([][]byte, len(types))
	codes := make([][]byte, len(types))
	for i := range types {
		functions[i] = wasmtest.ULEB(uint32(i))
		codes[i] = wasmtest.Code([]byte{0x0b})
	}
	i32Init := append([]byte{0x41}, wasmtest.SLEB32(666)...)
	i32Init = append(i32Init, 0x0b)
	i64Init := append([]byte{0x42}, wasmtest.SLEB64(666)...)
	i64Init = append(i64Init, 0x0b)
	f32Init := make([]byte, 6)
	f32Init[0], f32Init[5] = 0x43, 0x0b
	binary.LittleEndian.PutUint32(f32Init[1:5], math.Float32bits(666))
	f64Init := make([]byte, 10)
	f64Init[0], f64Init[9] = 0x44, 0x0b
	binary.LittleEndian.PutUint64(f64Init[1:9], math.Float64bits(666))
	exports := [][]byte{
		wasmtest.ExportEntry("print", byte(wasm.ExternFunc), 0),
		wasmtest.ExportEntry("print_i32", byte(wasm.ExternFunc), 1),
		wasmtest.ExportEntry("print_i64", byte(wasm.ExternFunc), 2),
		wasmtest.ExportEntry("print_f32", byte(wasm.ExternFunc), 3),
		wasmtest.ExportEntry("print_f64", byte(wasm.ExternFunc), 4),
		wasmtest.ExportEntry("print_i32_f32", byte(wasm.ExternFunc), 5),
		wasmtest.ExportEntry("print_f64_f64", byte(wasm.ExternFunc), 6),
		wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
		wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0),
		wasmtest.ExportEntry("global_i32", byte(wasm.ExternGlobal), 0),
		wasmtest.ExportEntry("global_i64", byte(wasm.ExternGlobal), 1),
		wasmtest.ExportEntry("global_f32", byte(wasm.ExternGlobal), 2),
		wasmtest.ExportEntry("global_f64", byte(wasm.ExternGlobal), 3),
	}
	module, err := wasm.DecodeModule(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 1, 10, 20})),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, i32Init),
			wasmtest.GlobalEntry(wasm.I64, false, i64Init),
			wasmtest.GlobalEntry(wasm.F32, false, f32Init),
			wasmtest.GlobalEntry(wasm.F64, false, f64Init),
		)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	))
	if err != nil {
		t.Fatal(err)
	}
	return module
}

func pico2ExecutionFirmwareOptions(module *shared.EmbeddedModule, helpers [4]uint32) shared.EmbeddedFirmwareOptions {
	opts := shared.EmbeddedFirmwareOptions{BaseAddress: qemu32.ImageBase, NativeStackLimit: 1, HelperEntries: helpers}
	if module.Memory != nil {
		pages := uint32(1024)
		if pages < module.Memory.Minimum {
			pages = module.Memory.Minimum
		}
		if pages < module.Memory.Minimum || module.Memory.HasMaximum && pages > module.Memory.Maximum {
			pages = module.Memory.Maximum
		}
		if pages < module.Memory.Minimum {
			pages = module.Memory.Minimum
		}
		opts.MemoryCapacity = pages * embedded32.WasmPageSize
	}
	tables := module.Tables
	if len(tables) == 0 && module.Table != nil {
		tables = []shared.EmbeddedTable{*module.Table}
	}
	if len(tables) != 0 {
		opts.TableCapacities = make([]uint32, len(tables))
		for i := range tables {
			capacity := tables[i].Minimum + 1024
			if capacity < tables[i].Minimum || tables[i].HasMaximum && capacity > tables[i].Maximum {
				capacity = tables[i].Maximum
			}
			if capacity < tables[i].Minimum {
				capacity = tables[i].Minimum
			}
			opts.TableCapacities[i] = capacity
		}
	}
	return opts
}

func pico2ExecutionResourceError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "arena capacity") || strings.Contains(err.Error(), "capacity"))
}

func invokePico2QEMU(module *pico2QEMUModule, action *pico2ExecutionAction) ([]uint32, embedded32.Trap, []uint32, embedded32.Trap, error) {
	if action == nil {
		return nil, 0, nil, 0, fmt.Errorf("missing action")
	}
	if action.Type == "get" {
		armResults, err := pico2ReadExportedGlobal(module.arm, action.Field)
		if err != nil {
			return nil, 0, nil, 0, err
		}
		rvResults, err := pico2ReadExportedGlobal(module.rv, action.Field)
		return armResults, embedded32.TrapNone, rvResults, embedded32.TrapNone, err
	}
	if action.Type != "invoke" {
		return nil, 0, nil, 0, fmt.Errorf("unsupported action %q", action.Type)
	}
	parameters, err := pico2ValueSlots(action.Args)
	if err != nil {
		return nil, 0, nil, 0, err
	}
	armFunction, err := pico2ExportedFunction(module.arm, action.Field)
	if err != nil {
		return nil, 0, nil, 0, err
	}
	rvFunction, err := pico2ExportedFunction(module.rv, action.Field)
	if err != nil {
		return nil, 0, nil, 0, err
	}
	if armFunction.ParamSlots != uint16(len(parameters)) || rvFunction.ParamSlots != uint16(len(parameters)) || armFunction.ResultSlots != rvFunction.ResultSlots {
		return nil, 0, nil, 0, fmt.Errorf("export %q slot shape differs", action.Field)
	}
	armResults, armTrap, err := module.arm.client.Call(armFunction.Address, armFunction.Context, parameters, uint32(armFunction.ResultSlots))
	if err != nil {
		return nil, 0, nil, 0, err
	}
	rvResults, rvTrap, err := module.rv.client.Call(rvFunction.Address, rvFunction.Context, parameters, uint32(rvFunction.ResultSlots))
	return armResults, armTrap, rvResults, rvTrap, err
}

func pico2ReadExportedGlobal(module *pico2QEMUTargetModule, name string) ([]uint32, error) {
	for i := range module.compiled.Exports {
		export := module.compiled.Exports[i]
		if export.Kind != wasm.ExternGlobal || export.Name != name {
			continue
		}
		var address uint32
		var valueType wasm.ValType
		if uint64(export.Index) < uint64(len(module.compiled.ImportedGlobals)) {
			directory, err := module.client.Read(module.image.ContextAddress+embedded32.ContextImportedGlobalsBaseOffset, 1)
			if err != nil {
				return nil, err
			}
			cell, err := module.client.Read(directory[0]+export.Index*4, 1)
			if err != nil {
				return nil, err
			}
			address = cell[0]
			valueType = module.compiled.ImportedGlobals[export.Index].Type
		} else {
			local := uint64(export.Index) - uint64(len(module.compiled.ImportedGlobals))
			if local >= uint64(len(module.compiled.Globals)) {
				return nil, fmt.Errorf("%s global export %q is unavailable", module.name, name)
			}
			global := module.compiled.Globals[local]
			address = module.image.GlobalsAddress + uint32(global.Slot)*4
			valueType = global.Type
		}
		width := uint32(1)
		if valueType.Kind == wasm.ValNum && (valueType.Num == wasm.NumI64 || valueType.Num == wasm.NumF64) {
			width = 2
		} else if valueType.Kind == wasm.ValVec {
			width = 4
		}
		return module.client.Read(address, width)
	}
	return nil, fmt.Errorf("%s global export %q not found", module.name, name)
}

func pico2ExportedFunction(module *pico2QEMUTargetModule, name string) (embedded32.FirmwareTransportFunction, error) {
	ordinal := 0
	for i := range module.image.Exports {
		export := module.image.Exports[i]
		if export.Kind != wasm.ExternFunc {
			continue
		}
		if export.Name == name {
			return module.image.TransportFunctions[ordinal], nil
		}
		ordinal++
	}
	return embedded32.FirmwareTransportFunction{}, fmt.Errorf("%s export %q not found", module.name, name)
}

func pico2ValueSlots(values []pico2ExecutionValue) ([]uint32, error) {
	var slots []uint32
	for i := range values {
		valueSlots, err := pico2OneValueSlots(values[i])
		if err != nil {
			return nil, err
		}
		slots = append(slots, valueSlots...)
	}
	return slots, nil
}

func pico2OneValueSlots(value pico2ExecutionValue) ([]uint32, error) {
	text, err := pico2RawString(value.Value)
	if err != nil && value.Type != "v128" {
		return nil, err
	}
	switch value.Type {
	case "i32", "f32":
		bits, err := strconv.ParseUint(text, 10, 32)
		return []uint32{uint32(bits)}, err
	case "i64", "f64":
		bits, err := strconv.ParseUint(text, 10, 64)
		return []uint32{uint32(bits), uint32(bits >> 32)}, err
	case "externref":
		if text == "null" {
			return []uint32{0}, nil
		}
		ref, err := strconv.ParseUint(text, 10, 32)
		return []uint32{uint32(ref) + 1}, err
	case "funcref":
		if text != "null" {
			return nil, fmt.Errorf("non-null funcref argument %q", text)
		}
		return []uint32{0}, nil
	case "v128":
		var lanes []string
		if err := json.Unmarshal(value.Value, &lanes); err != nil {
			return nil, err
		}
		var raw [16]byte
		width := map[string]int{"i8": 1, "i16": 2, "i32": 4, "i64": 8, "f32": 4, "f64": 8}[value.LaneType]
		if width == 0 || len(lanes)*width != len(raw) {
			return nil, fmt.Errorf("invalid v128 lane shape %s/%d", value.LaneType, len(lanes))
		}
		for i, lane := range lanes {
			bits, err := strconv.ParseUint(lane, 10, width*8)
			if err != nil {
				return nil, err
			}
			for b := 0; b < width; b++ {
				raw[i*width+b] = byte(bits >> (8 * b))
			}
		}
		return []uint32{binary.LittleEndian.Uint32(raw[0:4]), binary.LittleEndian.Uint32(raw[4:8]), binary.LittleEndian.Uint32(raw[8:12]), binary.LittleEndian.Uint32(raw[12:16])}, nil
	default:
		return nil, fmt.Errorf("unsupported value type %q", value.Type)
	}
}

func pico2RawString(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", err
	}
	return text, nil
}

func comparePico2Expected(results []uint32, expected []pico2ExecutionValue) error {
	offset := 0
	for i := range expected {
		value := expected[i]
		width := map[string]int{"i32": 1, "f32": 1, "externref": 1, "funcref": 1, "i64": 2, "f64": 2, "v128": 4}[value.Type]
		if width == 0 || offset+width > len(results) {
			return fmt.Errorf("result %d type=%s exceeds %d slots", i, value.Type, len(results))
		}
		got := results[offset : offset+width]
		offset += width
		if value.Type == "f32" || value.Type == "f64" || value.Type == "v128" {
			if err := comparePico2FloatValue(got, value); err != nil {
				return fmt.Errorf("result %d: %w", i, err)
			}
			continue
		}
		want, err := pico2OneValueSlots(value)
		if err != nil {
			return err
		}
		for j := range want {
			if got[j] != want[j] {
				return fmt.Errorf("result %d slot %d=%#x want=%#x", i, j, got[j], want[j])
			}
		}
	}
	if offset != len(results) {
		return fmt.Errorf("result has %d trailing slots", len(results)-offset)
	}
	return nil
}

func comparePico2FloatValue(got []uint32, value pico2ExecutionValue) error {
	if value.Type == "v128" {
		var lanes []string
		if err := json.Unmarshal(value.Value, &lanes); err != nil {
			return err
		}
		width := map[string]int{"i8": 1, "i16": 2, "i32": 4, "i64": 8, "f32": 4, "f64": 8}[value.LaneType]
		if width == 0 || len(lanes)*width != 16 {
			return fmt.Errorf("invalid v128 lane shape %s/%d", value.LaneType, len(lanes))
		}
		var raw [16]byte
		for i := range got {
			binary.LittleEndian.PutUint32(raw[i*4:], got[i])
		}
		for i, lane := range lanes {
			offset := i * width
			if lane == "nan:canonical" || lane == "nan:arithmetic" {
				if value.LaneType == "f32" {
					bits := binary.LittleEndian.Uint32(raw[offset : offset+4])
					if lane == "nan:canonical" && bits&0x7fffffff != 0x7fc00000 {
						return fmt.Errorf("v128 lane %d bits=%#x are not canonical f32 NaN", i, bits)
					}
					if lane == "nan:arithmetic" && (bits&0x7f800000 != 0x7f800000 || bits&0x00400000 == 0) {
						return fmt.Errorf("v128 lane %d bits=%#x are not arithmetic f32 NaN", i, bits)
					}
					continue
				}
				if value.LaneType == "f64" {
					bits := binary.LittleEndian.Uint64(raw[offset : offset+8])
					if lane == "nan:canonical" && bits&0x7fffffffffffffff != 0x7ff8000000000000 {
						return fmt.Errorf("v128 lane %d bits=%#x are not canonical f64 NaN", i, bits)
					}
					if lane == "nan:arithmetic" && (bits&0x7ff0000000000000 != 0x7ff0000000000000 || bits&0x0008000000000000 == 0) {
						return fmt.Errorf("v128 lane %d bits=%#x are not arithmetic f64 NaN", i, bits)
					}
					continue
				}
				return fmt.Errorf("NaN lane token for %s", value.LaneType)
			}
			want, err := strconv.ParseUint(lane, 10, width*8)
			if err != nil {
				return err
			}
			var actual uint64
			for b := 0; b < width; b++ {
				actual |= uint64(raw[offset+b]) << (8 * b)
			}
			if actual != want {
				return fmt.Errorf("v128 lane %d=%#x want=%#x", i, actual, want)
			}
		}
		return nil
	}
	text, err := pico2RawString(value.Value)
	if err != nil {
		return err
	}
	if text != "nan:canonical" && text != "nan:arithmetic" {
		want, err := pico2OneValueSlots(value)
		if err != nil {
			return err
		}
		for i := range want {
			if got[i] != want[i] {
				return fmt.Errorf("slot %d=%#x want=%#x", i, got[i], want[i])
			}
		}
		return nil
	}
	if value.Type == "f32" {
		bits := got[0]
		if text == "nan:canonical" && bits&0x7fffffff != 0x7fc00000 {
			return fmt.Errorf("f32 bits=%#x are not canonical NaN", bits)
		}
		if text == "nan:arithmetic" && (bits&0x7f800000 != 0x7f800000 || bits&0x00400000 == 0) {
			return fmt.Errorf("f32 bits=%#x are not arithmetic NaN", bits)
		}
		return nil
	}
	bits := uint64(got[0]) | uint64(got[1])<<32
	if text == "nan:canonical" && bits&0x7fffffffffffffff != 0x7ff8000000000000 {
		return fmt.Errorf("f64 bits=%#x are not canonical NaN", bits)
	}
	if text == "nan:arithmetic" && (bits&0x7ff0000000000000 != 0x7ff0000000000000 || bits&0x0008000000000000 == 0) {
		return fmt.Errorf("f64 bits=%#x are not arithmetic NaN", bits)
	}
	return nil
}

func pico2ExpectedTrap(text string) (embedded32.Trap, bool) {
	if strings.HasPrefix(text, "uninitialized element") {
		return embedded32.TrapIndirectCallNull, true
	}
	switch text {
	case "unreachable":
		return embedded32.TrapUnreachable, true
	case "out of bounds memory access":
		return embedded32.TrapMemoryOutOfBounds, true
	case "integer divide by zero":
		return embedded32.TrapIntegerDivideByZero, true
	case "integer overflow":
		return embedded32.TrapIntegerOverflow, true
	case "invalid conversion to integer":
		return embedded32.TrapInvalidConversion, true
	case "call stack exhausted":
		return embedded32.TrapStackOverflow, true
	case "out of bounds table access", "undefined element":
		return embedded32.TrapTableOutOfBounds, true
	case "uninitialized element":
		return embedded32.TrapIndirectCallNull, true
	case "indirect call type mismatch":
		return embedded32.TrapIndirectCallTypeMismatch, true
	default:
		return 0, false
	}
}
