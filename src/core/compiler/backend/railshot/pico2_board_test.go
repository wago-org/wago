package railshot

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/internal/spectest"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/arm32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

// TestPico2Release2BoardExecution streams one compiled 32-bit module at a
// time to a physical Pico 2. It is deliberately opt-in: both variables identify
// external qualification inputs, and an optional file filter keeps initial
// board runs short and reproducible.
//
//	WAGO_PICO2_SERIAL=/dev/ttyACM0 \
//	WAGO_PICO2_SPECTEST_DIR=/path/to/WebAssembly/spec \
//	WAGO_PICO2_SPECTEST_FILE=i32 \
//	WAGO_PICO2_TARGET=riscv32 \
//	go test ./src/core/compiler/backend/railshot \
//	  -run '^TestPico2Release2BoardExecution$' -count=1 -v -timeout=10m
//
// This one-resident-module path supports closed modules and aliases of the
// current module. A script that later references a replaced registered module,
// imports another module, reads an exported global, or asserts an instantiation
// failure is rejected clearly instead of being partially exercised.
func TestPico2Release2BoardExecution(t *testing.T) {
	checkout := os.Getenv("WAGO_PICO2_SPECTEST_DIR")
	port := os.Getenv("WAGO_PICO2_SERIAL")
	if checkout == "" || port == "" {
		t.Skip("WAGO_PICO2_SPECTEST_DIR and WAGO_PICO2_SERIAL are required")
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	suite, err := spectest.DiscoverRelease2(checkout)
	if err != nil {
		t.Fatal(err)
	}
	target, targetName, err := pico2BoardTarget(os.Getenv("WAGO_PICO2_TARGET"))
	if err != nil {
		t.Fatal(err)
	}
	board, err := openPico2Board(port, target, targetName)
	if err != nil {
		t.Fatal(err)
	}
	defer board.close()
	t.Logf("Pico 2 %s board: port=%s base=%#08x capacity=%d max_chunk=%d", targetName, port, board.status.BaseAddress, board.status.Capacity, board.status.MaximumChunk)

	filter := os.Getenv("WAGO_PICO2_SPECTEST_FILE")
	tmp := t.TempDir()
	modules, assertions, files := 0, 0, 0
	selected := false
	for _, base := range suite.Files {
		if filter != "" && base != filter {
			continue
		}
		selected = true
		files++
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
			gotModules, gotAssertions, err := runPico2BoardScript(t, board, filepath.Dir(jsonPath), &script)
			t.Logf("Pico 2 %s script: modules=%d assertions=%d", targetName, gotModules, gotAssertions)
			if err != nil {
				t.Fatal(err)
			}
			modules += gotModules
			assertions += gotAssertions
		})
	}
	if !selected {
		t.Fatalf("execution filter %q did not select a script", filter)
	}
	t.Logf("Pico 2 %s physical execution: modules=%d assertions=%d files=%d", targetName, modules, assertions, files)
}

func pico2BoardTarget(value string) (uint32, string, error) {
	switch value {
	case "", "arm", "arm32":
		return embedded32.TransportTargetArm32, "arm32", nil
	case "riscv", "riscv32", "rv32":
		return embedded32.TransportTargetRISCV32, "riscv32", nil
	default:
		return 0, "", fmt.Errorf("WAGO_PICO2_TARGET %q is not arm32 or riscv32", value)
	}
}

type pico2BoardModule struct {
	compiled   *shared.EmbeddedModule
	image      *shared.EmbeddedFirmwareImage
	generation uint64
}

func runPico2BoardScript(t *testing.T, board *pico2Board, directory string, script *pico2ExecutionScript) (int, int, error) {
	t.Helper()
	if board == nil || script == nil {
		return 0, 0, fmt.Errorf("nil board or script")
	}
	trace := os.Getenv("WAGO_PICO2_TRACE") != ""
	byName := make(map[string]*pico2BoardModule)
	var current *pico2BoardModule
	modules, assertions := 0, 0
	for commandIndex := range script.Commands {
		command := &script.Commands[commandIndex]
		switch command.Type {
		case "module":
			wasmBytes, err := os.ReadFile(filepath.Join(directory, command.Filename))
			if err != nil {
				return modules, assertions, fmt.Errorf("line %d read module: %w", command.Line, err)
			}
			module, err := wasm.DecodeModule(wasmBytes)
			if err != nil {
				return modules, assertions, fmt.Errorf("line %d decode module: %w", command.Line, err)
			}
			if len(module.Imports) != 0 {
				return modules, assertions, fmt.Errorf("line %d module has %d imports; one-resident board execution does not yet preserve providers", command.Line, len(module.Imports))
			}
			current, err = board.compileUploadStart(module)
			if err != nil {
				return modules, assertions, fmt.Errorf("line %d module upload/start: %w", command.Line, err)
			}
			modules++
			if command.Name != "" {
				byName[command.Name] = current
			}
			if trace {
				t.Logf("line %d module: artifact=%d image=%d exports=%d", command.Line, board.status.ImageBytes, len(current.image.Bytes), len(current.image.TransportFunctions))
			}
		case "register":
			module := current
			if command.Name != "" {
				module = byName[command.Name]
			}
			if module == nil {
				return modules, assertions, fmt.Errorf("line %d register references unavailable module %q", command.Line, command.Name)
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
				return modules, assertions, fmt.Errorf("line %d action references unavailable module %q", command.Line, command.Action.Module)
			}
			if module.generation != board.generation {
				return modules, assertions, fmt.Errorf("line %d action references a replaced module; one-resident board execution cannot preserve it", command.Line)
			}
			results, trap, err := invokePico2Board(board, module, command.Action)
			if err != nil {
				return modules, assertions, fmt.Errorf("line %d invoke: %w", command.Line, err)
			}
			assertions++
			if trace {
				t.Logf("line %d %s %s: trap=%d results=%#v", command.Line, command.Type, command.Action.Field, trap, results)
			}
			switch command.Type {
			case "assert_trap", "assert_exhaustion":
				want, ok := pico2ExpectedTrap(command.Text)
				if !ok {
					return modules, assertions, fmt.Errorf("line %d unsupported trap text %q", command.Line, command.Text)
				}
				if trap != want {
					t.Errorf("line %d trap=%d want=%d (%s)", command.Line, trap, want, command.Text)
				}
			default:
				if trap != embedded32.TrapNone {
					t.Errorf("line %d unexpected trap=%d", command.Line, trap)
					continue
				}
				if command.Type == "assert_return" {
					if err := comparePico2Expected(results, command.Expected); err != nil {
						t.Errorf("line %d result: %v", command.Line, err)
					}
				}
			}
		case "assert_uninstantiable":
			return modules, assertions, fmt.Errorf("line %d assert_uninstantiable needs target memory read/write transport", command.Line)
		case "assert_malformed", "assert_invalid", "assert_unlinkable":
			// These are host decoder/linker admission checks, not executable
			// target actions. The compile-admission gate covers them strictly.
		default:
			return modules, assertions, fmt.Errorf("line %d unsupported command %q", command.Line, command.Type)
		}
	}
	return modules, assertions, nil
}

func invokePico2Board(board *pico2Board, module *pico2BoardModule, action *pico2ExecutionAction) ([]uint32, embedded32.Trap, error) {
	if action == nil {
		return nil, 0, fmt.Errorf("missing action")
	}
	if action.Type == "get" {
		return nil, 0, fmt.Errorf("exported global reads need target memory read transport")
	}
	if action.Type != "invoke" {
		return nil, 0, fmt.Errorf("unsupported action %q", action.Type)
	}
	parameters, err := pico2ValueSlots(action.Args)
	if err != nil {
		return nil, 0, err
	}
	ordinal, function, err := pico2BoardExport(module, action.Field)
	if err != nil {
		return nil, 0, err
	}
	if function.ParamSlots != uint16(len(parameters)) {
		return nil, 0, fmt.Errorf("export %q takes %d slots, got %d", action.Field, function.ParamSlots, len(parameters))
	}
	return board.call(ordinal, parameters, uint32(function.ResultSlots))
}

func pico2BoardExport(module *pico2BoardModule, name string) (uint32, embedded32.FirmwareTransportFunction, error) {
	if module == nil || module.image == nil {
		return 0, embedded32.FirmwareTransportFunction{}, fmt.Errorf("module image is unavailable")
	}
	ordinal := uint32(0)
	for i := range module.image.Exports {
		export := module.image.Exports[i]
		if export.Kind != wasm.ExternFunc {
			continue
		}
		if uint64(ordinal) >= uint64(len(module.image.TransportFunctions)) {
			return 0, embedded32.FirmwareTransportFunction{}, fmt.Errorf("transport export metadata is truncated")
		}
		if export.Name == name {
			return ordinal, module.image.TransportFunctions[ordinal], nil
		}
		ordinal++
	}
	return 0, embedded32.FirmwareTransportFunction{}, fmt.Errorf("embedded32 export %q not found", name)
}

func TestPico2BoardExportUsesFunctionOrdinal(t *testing.T) {
	functions := []embedded32.FirmwareTransportFunction{
		{Address: 0x20001001, ParamSlots: 1, ResultSlots: 1},
		{Address: 0x20001021, ParamSlots: 2, ResultSlots: 2},
	}
	module := &pico2BoardModule{image: &shared.EmbeddedFirmwareImage{
		Exports: []shared.EmbeddedFirmwareExport{
			{Name: "memory", Kind: wasm.ExternMem},
			{Name: "first", Kind: wasm.ExternFunc},
			{Name: "global", Kind: wasm.ExternGlobal},
			{Name: "second", Kind: wasm.ExternFunc},
		},
		TransportFunctions: functions,
	}}
	ordinal, function, err := pico2BoardExport(module, "second")
	if err != nil {
		t.Fatal(err)
	}
	if ordinal != 1 || function != functions[1] {
		t.Fatalf("second export = ordinal %d %#v, want ordinal 1 %#v", ordinal, function, functions[1])
	}
	if _, _, err := pico2BoardExport(module, "missing"); err == nil {
		t.Fatal("missing export was accepted")
	}
}

type pico2Board struct {
	stream         *os.File
	target         uint32
	targetName     string
	sequence       uint32
	maximumPayload uint32
	status         embedded32.TransportUploadStatusInfo
	generation     uint64
}

const pico2BoardExchangeTimeout = 30 * time.Second

func openPico2Board(port string, target uint32, targetName string) (*pico2Board, error) {
	if output, err := exec.Command("stty", "-F", port, "raw", "-echo", "115200").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("configure %s: %w: %s", port, err, output)
	}
	stream, err := os.OpenFile(port, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", port, err)
	}
	board := &pico2Board{stream: stream, target: target, targetName: targetName, maximumPayload: 4096}
	timer := time.AfterFunc(30*time.Second, func() { _ = stream.Close() })
	defer timer.Stop()
	frame, err := board.exchange(embedded32.TransportHello, nil)
	if err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("hello: %w", err)
	}
	hello, err := embedded32.DecodeTransportHello(frame.Payload)
	if err != nil || hello.Target != target {
		_ = stream.Close()
		return nil, fmt.Errorf("%s hello: target=%d decode=%v", targetName, hello.Target, err)
	}
	board.maximumPayload = hello.MaximumPayload
	frame, err = board.exchange(embedded32.TransportUploadStatus, nil)
	if err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("upload status: %w", err)
	}
	board.status, err = embedded32.DecodeTransportUploadStatus(frame.Payload)
	if err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("decode upload status: %w", err)
	}
	return board, nil
}

func (b *pico2Board) close() {
	if b != nil && b.stream != nil {
		_ = b.stream.Close()
	}
}

func (b *pico2Board) compileUploadStart(module *wasm.Module) (*pico2BoardModule, error) {
	var compiled *shared.EmbeddedModule
	var err error
	switch b.target {
	case embedded32.TransportTargetArm32:
		compiled, err = arm32.CompileModule(module)
	case embedded32.TransportTargetRISCV32:
		compiled, err = riscv32.CompileModule(module)
	default:
		err = fmt.Errorf("unsupported Pico 2 target %d", b.target)
	}
	if err != nil {
		return nil, err
	}
	opts := shared.EmbeddedFirmwareOptions{
		BaseAddress:      b.status.BaseAddress,
		NativeStackLimit: 1,
		HelperEntries:    [4]uint32{1, 1, 1, 1},
	}
	if compiled.Memory != nil {
		functionCount := uint32(0)
		for i := range compiled.Exports {
			if compiled.Exports[i].Kind == wasm.ExternFunc {
				functionCount++
			}
		}
		for pages := compiled.Memory.Minimum + 1; pages > compiled.Memory.Minimum; pages++ {
			if compiled.Memory.HasMaximum && pages > compiled.Memory.Maximum || uint64(pages)*uint64(embedded32.WasmPageSize) > uint64(^uint32(0)) {
				break
			}
			candidate := opts
			candidate.MemoryCapacity = pages * embedded32.WasmPageSize
			candidateSize, candidateErr := pico2BoardFirmwareImageSize(b.target, compiled, candidate)
			if candidateErr != nil {
				break
			}
			artifactSize, ok := embedded32.FirmwareArtifactSize(candidateSize, 1, functionCount)
			if !ok || artifactSize > b.status.Capacity {
				break
			}
			opts = candidate
		}
	}
	size, err := pico2BoardFirmwareImageSize(b.target, compiled, opts)
	if err != nil {
		return nil, err
	}
	if size > b.status.Capacity {
		return nil, fmt.Errorf("firmware image needs %d bytes, board capacity is %d", size, b.status.Capacity)
	}
	image, err := pico2BoardBuildFirmwareImage(b.target, make([]byte, size), compiled, opts)
	if err != nil {
		return nil, err
	}
	artifact := embedded32.FirmwareArtifact{
		Target:         b.target,
		ImageAddress:   image.BaseAddress,
		ContextAddress: image.ContextAddress,
		StartAddress:   image.StartAddress,
		Image:          image.Bytes,
		Contexts:       []uint32{image.ContextAddress},
		Functions:      image.TransportFunctions,
	}
	artifactSize, ok := embedded32.FirmwareArtifactSize(uint32(len(artifact.Image)), uint32(len(artifact.Contexts)), uint32(len(artifact.Functions)))
	if !ok {
		return nil, fmt.Errorf("firmware artifact size overflow")
	}
	if artifactSize > b.status.Capacity {
		return nil, fmt.Errorf("firmware artifact needs %d bytes, board capacity is %d", artifactSize, b.status.Capacity)
	}
	encoded := make([]byte, artifactSize)
	if _, err := embedded32.EncodeFirmwareArtifact(encoded, artifact); err != nil {
		return nil, err
	}
	if err := b.upload(encoded); err != nil {
		return nil, err
	}
	if _, err := b.exchange(embedded32.TransportInstantiate, nil); err != nil {
		return nil, fmt.Errorf("instantiate: %w", err)
	}
	if _, err := b.exchange(embedded32.TransportStart, nil); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	b.generation++
	return &pico2BoardModule{compiled: compiled, image: image, generation: b.generation}, nil
}

func pico2BoardFirmwareImageSize(target uint32, compiled *shared.EmbeddedModule, opts shared.EmbeddedFirmwareOptions) (uint32, error) {
	switch target {
	case embedded32.TransportTargetArm32:
		return arm32.FirmwareImageSize(compiled, opts)
	case embedded32.TransportTargetRISCV32:
		return riscv32.FirmwareImageSize(compiled, opts)
	default:
		return 0, fmt.Errorf("unsupported Pico 2 target %d", target)
	}
}

func pico2BoardBuildFirmwareImage(target uint32, dst []byte, compiled *shared.EmbeddedModule, opts shared.EmbeddedFirmwareOptions) (*shared.EmbeddedFirmwareImage, error) {
	switch target {
	case embedded32.TransportTargetArm32:
		return arm32.BuildFirmwareImage(dst, compiled, opts)
	case embedded32.TransportTargetRISCV32:
		return riscv32.BuildFirmwareImage(dst, compiled, opts)
	default:
		return nil, fmt.Errorf("unsupported Pico 2 target %d", target)
	}
}

func (b *pico2Board) upload(image []byte) error {
	checksum := embedded32.TransportChecksum(image)
	begin := make([]byte, embedded32.TransportUploadBeginBytes)
	if err := embedded32.EncodeTransportUploadBegin(begin, embedded32.TransportUploadBeginRequest{ImageBytes: uint32(len(image)), ImageChecksum: checksum}); err != nil {
		return err
	}
	if _, err := b.exchange(embedded32.TransportUploadBegin, begin); err != nil {
		return fmt.Errorf("upload begin: %w", err)
	}
	if b.maximumPayload <= embedded32.TransportUploadChunkHeader {
		return fmt.Errorf("maximum payload %d cannot carry an upload chunk", b.maximumPayload)
	}
	chunkBytes := min(b.status.MaximumChunk, b.maximumPayload-embedded32.TransportUploadChunkHeader)
	chunk := make([]byte, embedded32.TransportUploadChunkHeader+chunkBytes)
	for offset := uint32(0); offset < uint32(len(image)); {
		end := min(offset+chunkBytes, uint32(len(image)))
		n, err := embedded32.EncodeTransportUploadChunk(chunk, embedded32.TransportUploadChunkRequest{Offset: offset, Bytes: image[offset:end]})
		if err != nil {
			return err
		}
		if _, err := b.exchange(embedded32.TransportUploadChunk, chunk[:n]); err != nil {
			return fmt.Errorf("upload chunk at %d: %w", offset, err)
		}
		offset = end
	}
	if _, err := b.exchange(embedded32.TransportUploadCommit, nil); err != nil {
		return fmt.Errorf("upload commit: %w", err)
	}
	frame, err := b.exchange(embedded32.TransportUploadStatus, nil)
	if err != nil {
		return fmt.Errorf("committed upload status: %w", err)
	}
	committed, err := embedded32.DecodeTransportUploadStatus(frame.Payload)
	if err != nil {
		return fmt.Errorf("decode committed upload status: %w", err)
	}
	if committed.State != embedded32.TransportUploadCommitted || committed.ImageBytes != uint32(len(image)) || committed.ImageChecksum != checksum {
		return fmt.Errorf("commit mismatch: state=%d bytes=%d checksum=%#x", committed.State, committed.ImageBytes, committed.ImageChecksum)
	}
	b.status = committed
	return nil
}

func (b *pico2Board) call(export uint32, parameters []uint32, resultSlots uint32) ([]uint32, embedded32.Trap, error) {
	payloadBytes, ok := embedded32.TransportCallRequestBytes(uint32(len(parameters)))
	if !ok || payloadBytes > b.maximumPayload || uint64(resultSlots)*4 > uint64(b.maximumPayload) {
		return nil, 0, embedded32.ErrTransportCapacity
	}
	payload := make([]byte, payloadBytes)
	n, err := embedded32.EncodeTransportCallRequest(payload, embedded32.TransportCallRequest{ExportIndex: export, ParameterSlots: parameters, ResultSlots: resultSlots})
	if err != nil {
		return nil, 0, err
	}
	frame, err := b.exchangeAny(embedded32.TransportCall, payload[:n])
	if err != nil {
		return nil, 0, err
	}
	if frame.Code != embedded32.TransportCodeOK {
		if trap, ok := frame.Code.Trap(); ok {
			return nil, trap, nil
		}
		return nil, 0, fmt.Errorf("call response code=%#x", frame.Code)
	}
	results := make([]uint32, resultSlots)
	if _, err := embedded32.DecodeTransportSlots(frame.Payload, results, resultSlots); err != nil {
		return nil, 0, err
	}
	return results, embedded32.TrapNone, nil
}

func (b *pico2Board) exchange(kind embedded32.TransportKind, payload []byte) (embedded32.TransportFrame, error) {
	frame, err := b.exchangeAny(kind, payload)
	if err != nil {
		return embedded32.TransportFrame{}, err
	}
	if frame.Code != embedded32.TransportCodeOK {
		return embedded32.TransportFrame{}, fmt.Errorf("response code=%#x", frame.Code)
	}
	return frame, nil
}

func (b *pico2Board) exchangeAny(kind embedded32.TransportKind, payload []byte) (embedded32.TransportFrame, error) {
	if b == nil || b.stream == nil || uint64(len(payload)) > uint64(b.maximumPayload) {
		return embedded32.TransportFrame{}, embedded32.ErrTransportCapacity
	}
	b.sequence++
	request := make([]byte, int(embedded32.TransportHeaderBytes)+len(payload))
	n, err := embedded32.EncodeTransportFrame(request, embedded32.TransportFrame{Kind: kind, Sequence: b.sequence, Payload: payload})
	if err != nil {
		return embedded32.TransportFrame{}, err
	}
	if err := pico2BoardWriteFull(b.stream, request[:n]); err != nil {
		return embedded32.TransportFrame{}, err
	}
	timer := time.AfterFunc(pico2BoardExchangeTimeout, func() { _ = b.stream.Close() })
	defer timer.Stop()
	header := make([]byte, embedded32.TransportHeaderBytes)
	if _, err := io.ReadFull(b.stream, header); err != nil {
		return embedded32.TransportFrame{}, err
	}
	payloadBytes := binary.LittleEndian.Uint32(header[12:16])
	if payloadBytes > b.maximumPayload {
		return embedded32.TransportFrame{}, embedded32.ErrTransportCapacity
	}
	response := make([]byte, embedded32.TransportHeaderBytes+payloadBytes)
	copy(response, header)
	if _, err := io.ReadFull(b.stream, response[embedded32.TransportHeaderBytes:]); err != nil {
		return embedded32.TransportFrame{}, err
	}
	frame, consumed, err := embedded32.DecodeTransportFrame(response, b.maximumPayload)
	if err != nil {
		return embedded32.TransportFrame{}, err
	}
	if consumed != uint32(len(response)) || frame.Kind != kind.Response() || frame.Sequence != b.sequence {
		return embedded32.TransportFrame{}, fmt.Errorf("unexpected response kind=%d sequence=%d", frame.Kind, frame.Sequence)
	}
	return frame, nil
}

func pico2BoardWriteFull(dst io.Writer, src []byte) error {
	for len(src) != 0 {
		written, err := dst.Write(src)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(src) {
			return io.ErrShortWrite
		}
		src = src[written:]
	}
	return nil
}
