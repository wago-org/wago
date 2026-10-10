//go:build (linux || darwin) && (amd64 || arm64) && !tinygo && !wago_precompiled

package wago

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type floorToken struct {
	kind     wasm.InstrKind
	index    uint32
	constant int32
	offset   int
}

func floorAddWidth(k wasm.InstrKind) int {
	switch k {
	case wasm.InstrI8x16Add:
		return 8
	case wasm.InstrI16x8Add:
		return 16
	case wasm.InstrI32x4Add:
		return 32
	}
	return 0
}
func floorShift(k wasm.InstrKind) (bits int, signed bool) {
	switch k {
	case wasm.InstrI8x16ShrS:
		return 8, true
	case wasm.InstrI8x16ShrU:
		return 8, false
	case wasm.InstrI16x8ShrS:
		return 16, true
	case wasm.InstrI16x8ShrU:
		return 16, false
	case wasm.InstrI32x4ShrS:
		return 32, true
	case wasm.InstrI32x4ShrU:
		return 32, false
	}
	return 0, false
}

// Exact nine-instruction windows only. Non-local sources, retained uses,
// mutation and effects are deliberately outside this observer's contract.
func floorWindow(w [9]floorToken) string {
	bits := floorAddWidth(w[8].kind)
	if bits == 0 {
		return ""
	}
	a, x, constant, shift := 0, 3, 6, 7
	if w[2].kind == wasm.InstrV128Xor {
		a, x, constant, shift = 5, 0, 3, 4
	}
	if w[a+2].kind != wasm.InstrV128And || w[x+2].kind != wasm.InstrV128Xor {
		return ""
	}
	if w[a].kind != wasm.InstrLocalGet || w[a+1].kind != wasm.InstrLocalGet || w[x].kind != wasm.InstrLocalGet || w[x+1].kind != wasm.InstrLocalGet {
		return "non-local producer"
	}
	same := (w[a].index == w[x].index && w[a+1].index == w[x+1].index) || (w[a].index == w[x+1].index && w[a+1].index == w[x].index)
	if !same {
		return "producer mismatch"
	}
	width, _ := floorShift(w[shift].kind)
	if width != bits {
		return "shift/width mismatch"
	}
	if w[constant].kind != wasm.InstrI32Const {
		return "dynamic count"
	}
	if uint32(w[constant].constant)&uint32(bits-1) != 1 {
		return "count mismatch"
	}
	return "hit"
}

type floorCensus struct{ functions, ands, xors, shifts, adds, hits, near int }

func scanFloor(raw []byte, report func(string)) (out floorCensus, err error) {
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		return out, err
	}
	if err = wasm.ValidateModule(m); err != nil {
		return out, err
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	for fn, f := range m.Code {
		out.functions++
		r := wasm.NewReader(f.BodyBytes)
		var w [9]floorToken
		var hash [sha256.Size]byte
		hashed := false
		for r.HasNext() {
			off := r.Offset()
			op, e := r.Byte()
			if e != nil {
				return out, e
			}
			constant := int32(0)
			if op == 0x41 {
				peek := *r
				constant, e = peek.I32()
				if e != nil {
					return out, e
				}
			}
			var imm wasm.InstructionImmediate
			if e = classifier.ClassifyInto(r, op, &imm); e != nil {
				return out, fmt.Errorf("function %d offset %d: %w", fn, off, e)
			}
			copy(w[:8], w[1:])
			w[8] = floorToken{imm.Kind, imm.Index, constant, off}
			if imm.Kind == wasm.InstrV128And {
				out.ands++
			}
			if imm.Kind == wasm.InstrV128Xor {
				out.xors++
			}
			if bits, _ := floorShift(imm.Kind); bits != 0 {
				out.shifts++
			}
			if floorAddWidth(imm.Kind) == 0 {
				continue
			}
			out.adds++
			verdict := floorWindow(w)
			if verdict == "" {
				continue
			}
			if verdict == "hit" {
				out.hits++
			} else {
				out.near++
			}
			if !hashed {
				hash = sha256.Sum256(f.BodyBytes)
				hashed = true
			}
			a, x, shift := 0, 3, 7
			if w[2].kind == wasm.InstrV128Xor {
				a, x, shift = 5, 0, 4
			}
			_, signed := floorShift(w[shift].kind)
			report(fmt.Sprintf("local-function=%d body-offset=%d body-sha256=%x width=%d signed=%v AND-locals=%d,%d XOR-locals=%d,%d verdict=%s", fn, w[0].offset, hash, floorAddWidth(imm.Kind), signed, w[a].index, w[a+1].index, w[x].index, w[x+1].index, verdict))
		}
	}
	return out, nil
}
func TestSIMDFloorAverageCensusControls(t *testing.T) {
	for _, s := range floorShapes() {
		for commute := 0; commute < 8; commute++ {
			for _, count := range []int{1, 1 + s.bits, 1 - s.bits} {
				raw := floorModule(t, s, commute, count, "plain")
				got, err := scanFloor(raw, func(string) {})
				if err != nil {
					t.Fatal(err)
				}
				if got.hits != 1 {
					t.Fatalf("%s commute=%d count=%d: %+v", s, commute, count, got)
				}
			}
		}
	}
	s := floorShape{16, true}
	expr := s.expression(0, 1, false)
	controls := map[string]struct{ expression, reason string }{
		"non-local":  {strings.Replace(expr, "(local.get $x)", "(v128.const i32x4 0 0 0 0)", 1), "non-local producer"},
		"producer":   {strings.Replace(expr, "(v128.xor (local.get $x) (local.get $y))", "(v128.xor (local.get $x) (local.get $z))", 1), "producer mismatch"},
		"count":      {s.expression(0, 2, false), "count mismatch"},
		"dynamic":    {strings.Replace(expr, "(i32.const 1)", "(local.get $count)", 1), "dynamic count"},
		"width":      {strings.Replace(expr, "i16x8.shr_s", "i32x4.shr_s", 1), "shift/width mismatch"},
		"left-shift": {strings.Replace(expr, "i16x8.shr_s", "i16x8.shl", 1), "shift/width mismatch"},
		"or":         {strings.Replace(expr, "v128.and", "v128.or", 1), ""},
		"sub":        {strings.Replace(expr, "i16x8.add", "i16x8.sub", 1), ""},
		"i64":        {strings.ReplaceAll(expr, "i16x8", "i64x2"), ""},
		"write":      {strings.Replace(expr, "(v128.xor (local.get $x)", "(v128.xor (local.tee $x (local.get $z))", 1), ""},
		"retained":   {strings.Replace(expr, "(v128.and (local.get $x) (local.get $y))", "(local.tee $z (v128.and (local.get $x) (local.get $y)))", 1), ""},
	}
	for name, c := range controls {
		t.Run(name, func(t *testing.T) {
			raw := watToWasm(t, `(module (func (param $x v128) (param $y v128) (param $z v128) (param $count i32) (result v128) `+c.expression+`))`)
			var reports []string
			got, err := scanFloor(raw, func(s string) { reports = append(reports, s) })
			if err != nil {
				t.Fatal(err)
			}
			if got.hits != 0 {
				t.Fatalf("accepted %s: %+v", name, got)
			}
			if c.reason != "" && (len(reports) != 1 || !strings.HasSuffix(reports[0], "verdict="+c.reason)) {
				t.Fatalf("wrong reason: %v", reports)
			}
		})
	}
}
func TestSIMDFloorAverageCorpusCensus(t *testing.T) {
	root := os.Getenv("WAGO_FLOOR_CORPUS")
	if root == "" {
		t.Skip("set WAGO_FLOOR_CORPUS to a real workload directory")
	}
	var total floorCensus
	modules, failed := 0, 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".wasm" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got, err := scanFloor(raw, func(detail string) { t.Logf("path=%s %s", rel, detail) })
		if err != nil {
			failed++
			t.Logf("REJECT path=%s sha256=%x reason=%v", rel, sha256.Sum256(raw), err)
			return nil
		}
		modules++
		total.functions += got.functions
		total.ands += got.ands
		total.xors += got.xors
		total.shifts += got.shifts
		total.adds += got.adds
		total.hits += got.hits
		total.near += got.near
		t.Logf("MODULE path=%s sha256=%x counts=%+v", rel, sha256.Sum256(raw), got)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if modules == 0 {
		t.Fatal("no admitted Wasm modules")
	}
	t.Logf("TOTAL modules=%d rejected=%d counts=%+v", modules, failed, total)
}
