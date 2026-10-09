//go:build wago_profile && amd64

package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const maxCaptureWasm = 32 << 10
const maxCaptureCode = 64 << 10

type capturedCode struct {
	bytes    []byte
	sources  []jitprofile.SourceRange
	owners   []jitprofile.Region
	features string
	close    func()
}

func compilerIdentity() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		revision, dirty := "", ""
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
			}
			if setting.Key == "vcs.modified" && setting.Value == "true" {
				dirty = "+dirty"
			}
		}
		if revision != "" {
			return revision + dirty
		}
	}
	return compiledRevision
}
func captureFile(input, output string) error {
	f, err := os.Open(input)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCaptureWasm+1))
	f.Close()
	if err != nil {
		return err
	}
	if len(data) > maxCaptureWasm {
		return fmt.Errorf("capture Wasm byte budget (%d)", maxCaptureWasm)
	}
	s, err := captureBytes(data)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded) > maxInputBytes {
		return fmt.Errorf("snapshot output byte budget")
	}
	if err = os.WriteFile(output, encoded, 0600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "captured %s: native=%d mapped=%d unmapped=%d bytes; compiler=%s; snapshot=%s\n", s.Architecture, s.Capture.NativeBytes, s.Capture.MappedBytes, s.Capture.UnmappedBytes, s.Provenance.CompilerRevision, output)
	return nil
}
func captureBytes(data []byte) (Snapshot, error) {
	var s Snapshot
	if len(data) > maxCaptureWasm {
		return s, fmt.Errorf("capture Wasm byte budget")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		return s, err
	}
	if len(m.Code) > 64 || len(m.Types) > 128 || len(m.Globals) > 128 || len(m.Imports) > 128 {
		return s, fmt.Errorf("capture module shape budget")
	}
	for _, body := range m.Code {
		var locals uint64
		for _, run := range body.Locals.Runs {
			locals += uint64(run.Count)
		}
		if locals > 256 {
			return s, fmt.Errorf("capture local budget")
		}
	}
	if err = wasm.ValidateModuleWithWorkers(m, 1); err != nil {
		return s, err
	}
	code, err := compileCapture(m)
	if err != nil {
		return s, err
	}
	defer code.close()
	if len(code.bytes) == 0 || len(code.bytes) > maxCaptureCode {
		return s, fmt.Errorf("capture native byte budget")
	}
	instructions, err := captureInstructions(code.bytes)
	if err != nil {
		return s, err
	}
	binaryPath, err := os.Executable()
	if err != nil {
		return s, err
	}
	binaryFile, err := os.Open(binaryPath)
	if err != nil {
		return s, err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, binaryFile)
	binaryFile.Close()
	if err != nil {
		return s, err
	}
	s.Architecture = runtime.GOARCH
	s.Provenance = Provenance{compilerIdentity(), hex.EncodeToString(hash.Sum(nil)), fmt.Sprintf("%x", sha256.Sum256(data)), code.features, "explicit", "wago_profile", "established/direct-backend"}
	s.Capture = &CaptureMetadata{NativeSHA256: fmt.Sprintf("%x", sha256.Sum256(code.bytes)), NativeBytes: uint64(len(code.bytes))}
	if err := populateRegions(&s, code.sources, instructions); err != nil {
		return s, err
	}
	if err := populateRaw(&s, code.owners, code.bytes); err != nil {
		return s, err
	}
	assignTargets(&s)
	if err = validate(s); err != nil {
		return s, err
	}
	return s, nil
}

// Targets are admitted only at captured instruction starts. Gaps, unsupported
// branch families and out-of-image/mid-instruction destinations stay literal.
func assignTargets(s *Snapshot) {
	anchors := make(map[uint64]string)
	for _, r := range s.Regions {
		for i, in := range r.Instructions {
			anchors[in.Offset] = fmt.Sprintf("%s.i%d", r.ID, i)
		}
	}
	for ri := range s.Regions {
		for ii := range s.Regions[ri].Instructions {
			in := &s.Regions[ri].Instructions[ii]
			in.Relocation = ""
			target, ok := branchDestination(s.Architecture, *in)
			if ok {
				in.Relocation = anchors[target]
			}
		}
	}
}
func branchDestination(arch string, in Instruction) (uint64, bool) {
	b, err := hex.DecodeString(in.Hex)
	if err != nil {
		return 0, false
	}
	r, err := decode(arch, in)
	if err != nil || !r.Known || !branchRecord(r) {
		return 0, false
	}
	var displacement int64
	if arch == "amd64" {
		if r.ImmediateWidth == 8 {
			displacement = int64(int8(b[len(b)-1]))
		} else {
			displacement = int64(int32(binary.LittleEndian.Uint32(b[len(b)-4:])))
		}
	} else {
		w := binary.LittleEndian.Uint32(b) & 0x03ffffff
		displacement = int64(int32(w<<6) >> 4)
	}
	base := int64(in.Offset)
	if in.Offset > uint64(^uint64(0)>>1) {
		return 0, false
	}
	if arch == "amd64" {
		if base > int64(^uint64(0)>>1)-int64(len(b)) {
			return 0, false
		}
		base += int64(len(b))
	}
	if displacement < 0 && base < -displacement || displacement > 0 && base > int64(^uint64(0)>>1)-displacement {
		return 0, false
	}
	return uint64(base + displacement), true
}

func populateRegions(s *Snapshot, sources []jitprofile.SourceRange, instructions []Instruction) error {
	occurrences := make(map[string]int)
	cursor := 0
	var previousSourceEnd uint64
	for _, source := range sources {
		if source.Offset > s.Capture.NativeBytes || source.Size > s.Capture.NativeBytes-source.Offset {
			return fmt.Errorf("source map outside image")
		}
		if source.Size == 0 {
			continue
		}
		if source.Offset < previousSourceEnd {
			return fmt.Errorf("overlapping/out-of-order source maps")
		}
		previousSourceEnd = source.Offset + source.Size
		pc := source.WasmOffset
		key := fmt.Sprintf("f%d.pc%d", source.Function, pc)
		id := fmt.Sprintf("%s.%d", key, occurrences[key])
		occurrences[key]++
		r := Region{ID: id, Function: source.Function, WasmOffset: &pc}
		for cursor < len(instructions) && instructions[cursor].Offset < source.Offset {
			cursor++
		}
		if cursor >= len(instructions) || instructions[cursor].Offset != source.Offset {
			return fmt.Errorf("source range starts inside instruction/gap")
		}
		for cursor < len(instructions) && instructions[cursor].Offset < source.Offset+source.Size {
			in := instructions[cursor]
			if in.Offset+uint64(len(in.Hex)/2) > source.Offset+source.Size {
				return fmt.Errorf("source range cuts instruction boundary")
			}
			r.Instructions = append(r.Instructions, in)
			s.Capture.MappedBytes += uint64(len(in.Hex) / 2)
			cursor++
		}
		if len(r.Instructions) > 0 {
			if len(s.Regions) == maxRegions {
				return fmt.Errorf("source region budget")
			}
			s.Regions = append(s.Regions, r)
		}
	}
	if s.Capture.MappedBytes > s.Capture.NativeBytes {
		return fmt.Errorf("overlapping source coverage")
	}
	s.Capture.UnmappedBytes = s.Capture.NativeBytes - s.Capture.MappedBytes
	return nil
}

// Profile ownership can include data/cold code and may cut objdump's decode.
// Gaps therefore retain raw bytes without guessed instruction or Wasm facts.
func populateRaw(s *Snapshot, owners []jitprofile.Region, code []byte) error {
	if len(owners) > maxRawRegions {
		return fmt.Errorf("profile ownership budget")
	}
	if err := jitprofile.ValidateRegions(owners, uint64(len(code))); err != nil {
		return err
	}
	occurrences := make(map[string]int)
	mi := 0
	for _, owner := range owners {
		if owner.Kind == "" || len(owner.Kind) > 64 || owner.Function < -1 || int64(owner.Function) > int64(^uint32(0)) {
			return fmt.Errorf("invalid profile ownership")
		}
		key := fmt.Sprintf("%s.f%d", owner.Kind, owner.Function)
		id := fmt.Sprintf("%s.%d", key, occurrences[key])
		occurrences[key]++
		cursor, end := owner.Offset, owner.Offset+owner.Size
		left := "owner-start"
		gap := 0
		add := func(start, stop uint64, right string) error {
			if start == stop {
				return nil
			}
			if len(s.RawRegions) == maxRawRegions {
				return fmt.Errorf("raw region budget")
			}
			s.RawRegions = append(s.RawRegions, RawRegion{ID: fmt.Sprintf("%s.gap%d", id, gap), Owner: id, Kind: owner.Kind, Function: owner.Function, LeftAnchor: left, RightAnchor: right, Offset: start, Hex: hex.EncodeToString(code[start:stop])})
			gap++
			return nil
		}
		for mi < len(s.Regions) && s.Regions[mi].Instructions[0].Offset < end {
			r := s.Regions[mi]
			start, stop := r.Instructions[0].Offset, regionEnd(r)
			if start < cursor || stop > end || owner.Function < 0 || uint32(owner.Function) != r.Function {
				return fmt.Errorf("source region crosses/mismatches profile owner")
			}
			if err := add(cursor, start, r.ID); err != nil {
				return err
			}
			cursor = stop
			left = r.ID
			mi++
		}
		if err := add(cursor, end, "owner-end"); err != nil {
			return err
		}
	}
	if mi != len(s.Regions) {
		return fmt.Errorf("source regions outside profile ownership")
	}
	s.Capture.RawCoverage = true
	return nil
}
