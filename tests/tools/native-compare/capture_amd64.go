//go:build amd64 && wago_profile

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func compileCapture(m *wasm.Module) (capturedCode, error) {
	knobs, snapshot := backend.OptKnobSnapshot()
	selection := make(map[string]bool, len(knobs))
	for _, knob := range knobs {
		selection[knob.Name] = knob.On
	}
	encoded, _ := json.Marshal(selection)
	var stats backend.ModuleStats
	cm, err := backend.CompileModuleWith(m, backend.CompileOptions{Stats: &stats, Profile: true, SourceMaps: true, Workers: 1, DeferCodeMapping: true, AMD64FeaturesSet: true, AMD64Features: 0, Optimizations: selection, OptimizationSnapshot: snapshot})
	if err != nil {
		return capturedCode{}, err
	}
	return capturedCode{cm.Code, stats.SourceRanges, stats.ProfileRegions, fmt.Sprintf("configured-mask=00000000;optimizations=%x", sha256.Sum256(encoded)), func() {
		if cm.CodeImage != nil {
			cm.CodeImage.Close()
		}
	}}, nil
}

type boundedListing struct{ bytes.Buffer }

func (b *boundedListing) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxInputBytes {
		return 0, fmt.Errorf("objdump output budget")
	}
	return b.Buffer.Write(p)
}
func captureInstructions(code []byte) ([]Instruction, error) {
	dir, err := os.MkdirTemp("", "wago-native-compare-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "code.bin")
	if err = os.WriteFile(path, code, 0600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "objdump", "-D", "-z", "-b", "binary", "-m", "i386:x86-64", "-Mintel", "--insn-width=16", path)
	var listing boundedListing
	cmd.Stdout = &listing
	cmd.Stderr = &listing
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("bounded objdump: %w", err)
	}
	return parseListing(listing.String(), code)
}
func parseListing(listing string, code []byte) ([]Instruction, error) {
	if len(listing) > maxInputBytes {
		return nil, fmt.Errorf("listing byte budget")
	}
	var instructions []Instruction
	covered := 0
	for _, line := range strings.Split(listing, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		offset, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 16, 64)
		if err != nil {
			continue
		}
		encoded := ""
		for _, field := range strings.Fields(parts[1]) {
			if len(field) != 2 {
				break
			}
			if _, err = hex.DecodeString(field); err != nil {
				break
			}
			encoded += field
		}
		if encoded == "" {
			continue
		}
		if offset != uint64(covered) || len(encoded) > 30 || covered+len(encoded)/2 > len(code) {
			return nil, fmt.Errorf("incomplete/misaligned objdump listing")
		}
		if encoded != hex.EncodeToString(code[covered:covered+len(encoded)/2]) {
			return nil, fmt.Errorf("objdump byte identity mismatch")
		}
		instructions = append(instructions, Instruction{offset, encoded, ""})
		covered += len(encoded) / 2
		if len(instructions) > maxInstructions {
			return nil, fmt.Errorf("disassembly instruction budget")
		}
	}
	if covered != len(code) {
		return nil, fmt.Errorf("objdump omitted bytes")
	}
	return instructions, nil
}
