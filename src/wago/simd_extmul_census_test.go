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

func extmulKind(k wasm.InstrKind) (width int, extension, explicit bool) {
	switch {
	case k >= wasm.InstrI16x8ExtendLowI8x16S && k <= wasm.InstrI16x8ExtendHighI8x16U:
		return 16, true, false
	case k >= wasm.InstrI32x4ExtendLowI16x8S && k <= wasm.InstrI32x4ExtendHighI16x8U:
		return 32, true, false
	case k >= wasm.InstrI64x2ExtendLowI32x4S && k <= wasm.InstrI64x2ExtendHighI32x4U:
		return 64, true, false
	case k >= wasm.InstrI16x8ExtmulLowI8x16S && k <= wasm.InstrI16x8ExtmulHighI8x16U:
		return 16, false, true
	case k >= wasm.InstrI32x4ExtmulLowI16x8S && k <= wasm.InstrI32x4ExtmulHighI16x8U:
		return 32, false, true
	case k >= wasm.InstrI64x2ExtmulLowI32x4S && k <= wasm.InstrI64x2ExtmulHighI32x4U:
		return 64, false, true
	case k == wasm.InstrI16x8Mul:
		return 16, false, false
	case k == wasm.InstrI32x4Mul:
		return 32, false, false
	case k == wasm.InstrI64x2Mul:
		return 64, false, false
	}
	return
}

type extmulCensus struct{ functions, extensions, multiplies, explicit, hits, near int }

// Only the exact five-instruction local-source window is in scope. Every
// instruction is decoded; constants and other immediates cannot create hits.
// This is a linear, constant-scratch observer, not a production rewrite.
func scanExtmul(raw []byte, report func(string)) (out extmulCensus, err error) {
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		return out, err
	}
	if err = wasm.ValidateModule(m); err != nil {
		return out, err
	}
	classifier := wasm.NewModuleInstructionClassifier(m, true)
	type token struct {
		kind   wasm.InstrKind
		index  uint32
		offset int
	}
	for fn, f := range m.Code {
		out.functions++
		r := wasm.NewReader(f.BodyBytes)
		var window [5]token
		var bodyHash [sha256.Size]byte
		hashed := false
		for r.HasNext() {
			off := r.Offset()
			op, e := r.Byte()
			if e != nil {
				return out, e
			}
			var imm wasm.InstructionImmediate
			if e = classifier.ClassifyInto(r, op, &imm); e != nil {
				return out, fmt.Errorf("function %d offset %d: %w", fn, off, e)
			}
			copy(window[:4], window[1:])
			window[4] = token{imm.Kind, imm.Index, off}
			width, ext, explicit := extmulKind(imm.Kind)
			if ext {
				out.extensions++
				continue
			}
			if explicit {
				out.explicit++
				continue
			}
			if width == 0 {
				continue
			}
			out.multiplies++
			a, b := window[1].kind, window[3].kind
			aw, ae, _ := extmulKind(a)
			bw, be, _ := extmulKind(b)
			if window[0].kind != wasm.InstrLocalGet || window[2].kind != wasm.InstrLocalGet || !ae || !be {
				continue
			}
			verdict := "hit"
			if aw != width || bw != width {
				verdict = "width mismatch"
			} else if a != b {
				verdict = "half/sign mismatch"
			}
			if verdict == "hit" {
				out.hits++
			} else {
				out.near++
			}
			if !hashed {
				bodyHash = sha256.Sum256(f.BodyBytes)
				hashed = true
			}
			report(fmt.Sprintf("local-function=%d body-offset=%d body-sha256=%x source-locals=%d,%d extension-kinds=%d,%d multiply-width=%d verdict=%s", fn, window[0].offset, bodyHash, window[0].index, window[2].index, a, b, width, verdict))
		}
	}
	return out, nil
}
func TestSIMDExtmulCensusControls(t *testing.T) {
	for _, s := range extmulShapes() {
		for _, fused := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/extmul=%v", s, fused), func(t *testing.T) {
				got, err := scanExtmul(extmulModule(t, s, fused, "plain"), func(string) {})
				if err != nil {
					t.Fatal(err)
				}
				if fused {
					if got.explicit != 1 || got.hits != 0 {
						t.Fatalf("%+v", got)
					}
				} else if got.hits != 1 || got.extensions != 2 || got.multiplies != 1 {
					t.Fatalf("%+v", got)
				}
			})
		}
	}
	s := extmulShape{32, false, false}
	expr := s.expression(false, false)
	controls := map[string]string{
		"half":                  strings.Replace(expr, "extend_low_i32x4_u (local.get $y)", "extend_high_i32x4_u (local.get $y)", 1),
		"sign":                  strings.Replace(expr, "extend_low_i32x4_u (local.get $y)", "extend_low_i32x4_s (local.get $y)", 1),
		"width":                 strings.Replace(expr, "i64x2.extend_low_i32x4_u (local.get $y)", "i32x4.extend_low_i16x8_u (local.get $y)", 1),
		"one-extension":         strings.Replace(expr, "(i64x2.extend_low_i32x4_u (local.get $y))", "(local.get $y)", 1),
		"intervening-write":     strings.Replace(expr, "(local.get $y)", "(local.tee $y (local.get $x))", 1),
		"retained-intermediate": strings.Replace(expr, "(i64x2.extend_low_i32x4_u (local.get $x))", "(local.tee $z (i64x2.extend_low_i32x4_u (local.get $x)))", 1),
	}
	for name, expression := range controls {
		t.Run(name, func(t *testing.T) {
			raw := watToWasm(t, `(module (func (param $x v128) (param $y v128) (result v128) (local $z v128) `+expression+`))`)
			got, err := scanExtmul(raw, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			if got.hits != 0 {
				t.Fatalf("accepted %s: %+v", name, got)
			}
			if (name == "half" || name == "sign" || name == "width") && got.near != 1 {
				t.Fatalf("lost near miss: %+v", got)
			}
		})
	}
}

func TestSIMDExtmulCorpusCensus(t *testing.T) {
	root := os.Getenv("WAGO_EXTMUL_CORPUS")
	if root == "" {
		t.Skip("set WAGO_EXTMUL_CORPUS to a real workload directory")
	}
	var total extmulCensus
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
		got, err := scanExtmul(raw, func(detail string) { t.Logf("path=%s %s", rel, detail) })
		if err != nil {
			failed++
			t.Logf("REJECT path=%s sha256=%x reason=%v", rel, sha256.Sum256(raw), err)
			return nil
		}
		modules++
		total.functions += got.functions
		total.extensions += got.extensions
		total.multiplies += got.multiplies
		total.explicit += got.explicit
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
	// Rejected modules are visible exclusions, not proof of absent patterns.
}
