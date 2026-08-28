//go:build (linux || darwin || windows) && (amd64 || arm64)

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestFuzzRegressionFixtureManifest(t *testing.T) {
	want := []string{
		"1054", "1777", "1792a", "1792b", "1792c", "1793a", "1793b", "1793c", "1793d",
		"1797a", "1797b", "1797c", "1797d", "1802", "1812", "1817", "1820", "1823",
		"1825", "1826", "1846", "1949", "1999", "2000a", "2000b", "2001", "2006",
		"2007", "2008", "2009", "2017", "2031", "2034", "2037", "2040", "2057", "2058",
		"2060", "2070", "2078", "2082", "2084", "2096", "2118", "2131", "2136", "2137",
		"2140", "2201", "2260", "695", "696", "699", "701", "704", "708", "709", "715",
		"716", "717", "718", "719", "720", "721", "722", "725", "730", "733", "873", "874", "888",
	}
	paths, err := filepath.Glob(filepath.Join("..", "..", "tests", "regressions", "fuzzcases", "*.wasm"))
	if err != nil {
		t.Fatalf("glob fuzz fixtures: %v", err)
	}
	got := make([]string, len(paths))
	for i, path := range paths {
		got[i] = strings.TrimSuffix(filepath.Base(path), ".wasm")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fuzz fixture manifest = %v, want %v", got, want)
	}
}

// TestFuzzRegressionCorpus is not a compile-only smoke test: each case preserves
// the result, trap, memory, or global-state oracle that made the original fuzz
// finding useful. Unsupported threads remain an explicit fail-closed test
// instead of being silently skipped.
func TestFuzzRegressionCorpus(t *testing.T) {
	t.Run("695", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "695")
		assertFuzzTrap(t, in, "i8x16s", TrapLinMemOutOfBounds)
		assertFuzzTrap(t, in, "i16x8s", TrapLinMemOutOfBounds)
	})
	t.Run("696", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "696")
		for _, tc := range []struct {
			name string
			in   uint64
			want []uint64
		}{
			{"select", 1 << 5, []uint64{math.MaxUint64, 0xeeeeeeeeeeeeeeee}},
			{"select", 1, []uint64{math.MaxUint64, 0xeeeeeeeeeeeeeeee}},
			{"select", 0, []uint64{0x1111111111111111, 0x2222222222222222}},
			{"select", 0xffffff, []uint64{math.MaxUint64, 0xeeeeeeeeeeeeeeee}},
			{"select", 0xffff00, []uint64{math.MaxUint64, 0xeeeeeeeeeeeeeeee}},
			{"select", 0, []uint64{0x1111111111111111, 0x2222222222222222}},
			{"typed select", 1, []uint64{math.MaxUint64, 0xeeeeeeeeeeeeeeee}},
			{"typed select", 0, []uint64{0x1111111111111111, 0x2222222222222222}},
		} {
			assertFuzzResults(t, in, tc.name, tc.want, tc.in)
		}
	})
	for _, id := range []string{"699", "704"} {
		id := id
		t.Run(id, func(t *testing.T) { instantiateFuzzFixture(t, id) })
	}
	t.Run("701", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "701")
		assertFuzzTrap(t, in, "i32.extend16_s", TrapLinMemOutOfBounds)
		assertFuzzTrap(t, in, "i32.extend8_s", TrapLinMemOutOfBounds)
	})
	t.Run("708", func(t *testing.T) {
		assertFuzzInstantiateError(t, "708", "linear memory access out of bounds")
	})
	t.Run("709", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "709")
		assertFuzzResults(t, in, "f64x2.promote_low_f32x4", []uint64{0xffffffffe0000000, 0xffffffffe0000000})
	})
	t.Run("715", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "715")
		assertFuzzResults(t, in, "select on conditional value after table.size", []uint64{1})
	})
	t.Run("716", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "716")
		assertFuzzResults(t, in, "select on ref.func", []uint64{1})
	})
	t.Run("717", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "717")
		want := make([]uint64, 35)
		for i := range want {
			want[i] = uint64(i)
		}
		assertFuzzResults(t, in, "vectors", want)
	})
	t.Run("718", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "718")
		assertFuzzResults(t, in, "v128.load_zero on the ceil", nil)
	})
	t.Run("719", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "719")
		assertFuzzTrap(t, in, "require unreachable", TrapUnreachable)
	})
	t.Run("720", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "720")
		assertFuzzResults(t, in, "access memory after table.grow", []uint64{math.MaxUint32})
	})
	t.Run("721", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "721")
		assertFuzzResults(t, in, "conditional before elem.drop", []uint64{1})
	})
	t.Run("722", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "722")
		assertFuzzResults(t, in, "conditional before data.drop", []uint64{1})
	})
	t.Run("725", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "725")
		assertFuzzTrap(t, in, "i32.load8_s", TrapLinMemOutOfBounds)
		assertFuzzTrap(t, in, "i32.load16_s", TrapLinMemOutOfBounds)
	})
	t.Run("730", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "730")
		for name, want := range map[string][]uint64{
			"f32x4.max":     {0x80000000 << 32, 0},
			"f32x4.min":     {0x80000000, 0x80000000<<32 | 0x80000000},
			"f64x2.max":     {0, 0},
			"f64x2.min":     {1 << 63, 1 << 63},
			"f64x2.max/mix": {0, 1 << 63},
			"f64x2.min/mix": {1 << 63, 0},
		} {
			assertFuzzResults(t, in, name, want)
		}
	})
	t.Run("733", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "733")
		assertFuzzTrap(t, in, "out of bounds", TrapLinMemOutOfBounds)
		if testing.Short() {
			return
		}
		assertFuzzResults(t, in, "store higher offset", nil)
		got, ok := in.ReadUint64Le(0x80000100)
		if !ok || got != math.MaxUint64 {
			t.Fatalf("memory[0x80000100] = %#x, %v", got, ok)
		}
	})

	// The historical regression only asserted that these malformed active
	// externref segments did not crash. Wago intentionally applies the stricter
	// Core validation/instantiation rule: active segments must fit their table.
	for _, id := range []string{"873", "874"} {
		id := id
		t.Run(id, func(t *testing.T) { assertFuzzInstantiateError(t, id, "active element segment") })
	}
	t.Run("888", testFuzzRegression888)
	t.Run("1054", func(t *testing.T) {
		b := readFuzzFixture(t, "1054")
		var memories [][]byte
		for i := 0; i < 2; i++ {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), b)
			if err != nil {
				t.Fatalf("compile %d: %v", i, err)
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: Imports{}})
			if err != nil {
				_ = c.Close()
				t.Fatalf("instantiate %d: %v", i, err)
			}
			memories = append(memories, append([]byte(nil), in.Memory().Bytes()...))
			_ = in.Close()
			_ = c.Close()
		}
		if !bytes.Equal(memories[0], memories[1]) {
			t.Fatal("independent instances initialized different memory")
		}
	})
	t.Run("1777", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1777")
		assertFuzzResults(t, in, "", []uint64{18446626425965379583, 4607736361554183979})
	})
	for _, id := range []string{"1792a", "1792b"} {
		id := id
		t.Run(id, func(t *testing.T) { instantiateFuzzFixture(t, id) })
	}
	t.Run("1792c", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1792c")
		assertFuzzResults(t, in, "", nil, 0, 0, 0)
		assertFuzzGlobal128(t, in, 0, 5044022786561933312, 9205357640488583168)
	})
	t.Run("1793a", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1793a")
		assertFuzzResults(t, in, "", nil)
		assertFuzzGlobal128(t, in, 2, 2531906066518671488, math.MaxUint64)
	})
	t.Run("1793b", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1793b")
		assertFuzzNoTrap(t, in, "", 0, 0, 0, 0)
		assertFuzzGlobal128(t, in, 1, 18374967954648334335, math.MaxUint64)
	})
	t.Run("1793c", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1793c")
		assertFuzzResults(t, in, "", nil, 0, 0)
		assertFuzzGlobal128(t, in, 0, math.MaxUint64, math.MaxUint64)
	})
	t.Run("1793d", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1793d")
		assertFuzzNoTrap(t, in, "")
		assertFuzzGlobal64(t, in, 1, 0)
	})
	t.Run("1797a", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1797a")
		assertFuzzResults(t, in, "", []uint64{0})
	})
	t.Run("1797b", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1797b")
		assertFuzzResults(t, in, "\x00\x00\x00\x00\x00", nil, make([]uint64, 6)...)
		assertFuzzGlobal128(t, in, 0, 2666130977255796624, 9223142857682330634)
	})
	t.Run("1797c", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1797c")
		assertFuzzTrapArgs(t, in, "~zz\x00E1E\x00EE\x00$", TrapUnreachable, make([]uint64, 20)...)
	})
	t.Run("1797d", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1797d")
		assertFuzzNoTrap(t, in, "p", make([]uint64, 20)...)
		assertFuzzGlobal128(t, in, 2, 15092115255309870764, 9241386435284803069)
	})
	t.Run("1802", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1802")
		assertFuzzTrap(t, in, "", TrapUnreachable)
	})
	t.Run("1812", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1812")
		assertFuzzResults(t, in, "", []uint64{
			0x8301fd00, 0xfd838783, 0x87878383, 0x9b000087, 0x170001fd,
			0xfd8383fd, 0x87838301, 0x878787, 0x83fd9b00, 0x201fd83,
			0x878783, 0x83fd9b00, 0x9b00fd83, 0xfd8383fd, 0x87838301,
			0x87878787, 0xfd9b0000, 0x87878383, 0x1fd8383,
		})
	})
	t.Run("1817", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1817")
		assertFuzzResults(t, in, "", nil)
		got, ok := in.Read(15616, 16)
		want := []byte{0, 0, 0, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		if !ok || !bytes.Equal(got, want) {
			t.Fatalf("memory = %x, %v; want %x", got, ok, want)
		}
		assertFuzzGlobal128(t, in, 0, 0x8000000080000000, 0x8000000080000000)
	})
	t.Run("1820", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1820")
		assertFuzzResults(t, in, "", nil)
		assertFuzzGlobal128(t, in, 1, 0xffffffffffff0000, 0xffff)
	})
	t.Run("1823", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1823")
		assertFuzzResults(t, in, "", nil)
		assertFuzzGlobal128(t, in, 0, 17282609607625994159, 4671060543367625455)
	})
	t.Run("1825", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1825")
		assertFuzzNoTrap(t, in, "")
		assertFuzzGlobal128(t, in, 6, 1099511627775, math.MaxUint64)
	})
	t.Run("1826", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1826")
		assertFuzzResults(t, in, "3", nil, 0, 0)
		assertFuzzGlobal128(t, in, 0, 1608723901141126568, 0)
	})
	t.Run("1846", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1846")
		assertFuzzResults(t, in, "", nil)
		assertFuzzGlobal64(t, in, 0, math.Float64bits(2))
	})
	t.Run("1949", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1949")
		assertFuzzTrap(t, in, "", TrapLinMemOutOfBounds)
		got, ok := in.Read(65526, 8)
		want := []byte{0xfe, 0xca, 0xfe, 0xca, 0, 0, 0, 0}
		if !ok || !bytes.Equal(got, want) {
			t.Fatalf("partial OOB vector store mutated memory: %x, %v", got, ok)
		}
	})
	t.Run("1999", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "1999")
		assertFuzzTrapArgs(t, in, "", TrapUnreachable, 0)
	})
	t.Run("2000a", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2000a")
		assertFuzzNoTrap(t, in, "", 0)
	})
	t.Run("2000b", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2000b")
		assertFuzzTrap(t, in, "", TrapTruncOverflow)
	})
	for _, id := range []string{"2001", "2006"} {
		id := id
		t.Run(id, func(t *testing.T) {
			in := instantiateFuzzFixture(t, id)
			args := []uint64(nil)
			if id == "2006" {
				args = []uint64{0}
			}
			assertFuzzResults(t, in, "", nil, args...)
		})
	}
	t.Run("2007", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2007")
		assertFuzzTrap(t, in, "", TrapTruncOverflow)
	})
	t.Run("2008", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2008")
		assertFuzzTrap(t, in, "", TrapUnreachable)
	})
	t.Run("2009", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2009")
		assertFuzzResults(t, in, "", []uint64{0})
	})
	t.Run("2017", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2017")
		assertFuzzTrap(t, in, "", TrapDivZero)
	})
	t.Run("2031", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2031")
		assertFuzzResults(t, in, "", []uint64{2139095040, 9218868437227405312}, 0)
	})
	t.Run("2034", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2034")
		assertFuzzResults(t, in, "", []uint64{0xf0f0f280f0f280f, 0, 0, 0, 0}, make([]uint64, 20)...)
	})
	t.Run("2037", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2037")
		assertFuzzResults(t, in, "", []uint64{0, 0, 0xbbbbbbbbbbbbbbbb, 0xbbbbbbbbbbbbbbbb, 0xcb6151c8d497b060, 0xbbbbbbbbbbbbbbbb, 0xe71c3971a22b233b, 0xa0a0a0a0a0a0a0a, 0, 0xfffffffb00000030, 0, 0x6c6cbbbbbbbbbbbb, 0xfeb44590ef194fa2, 0x1313131313131313, 0x1898a98e9daf4f22, 0xf8f8f8f80a0a0aa0, 0x6c6c6c6c6c6cf1f8, 0x6c6c6c6c6c6c6c6c, 0x9abbbbbbbbbbbb6c, 0x9a9ad39a9a9a9a9a})
	})
	t.Run("2040", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2040")
		assertFuzzResults(t, in, "", nil, 0)
	})
	t.Run("2057", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2057")
		assertFuzzResults(t, in, "", []uint64{0xe2012900e20129, 0xe2012900e20129}, 0)
	})
	t.Run("2058", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2058")
		assertFuzzResults(t, in, "", nil, 0, 0, 0)
		assertFuzzGlobal128(t, in, 0, math.MaxUint64, 0)
	})
	t.Run("2060", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2060")
		assertFuzzResults(t, in, "", nil, 0)
		assertFuzzGlobal128(t, in, 0, 1, 1)
		assertFuzzGlobal128(t, in, 1, math.MaxUint64, math.MaxUint64)
	})
	t.Run("2070", func(t *testing.T) { instantiateFuzzFixture(t, "2070") })

	// The upstream tests below were differential compiler/interpreter checks.
	// Wago has no interpreter tier, so pin the concrete compiler oracle captured
	// from the pinned source revision instead of weakening these to smoke tests.
	t.Run("2078", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2078")
		assertFuzzResults(t, in, "", nil, 0, 0)
	})
	t.Run("2082", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2082")
		assertFuzzTrapArgs(t, in, "", TrapTruncOverflow, make([]uint64, 8)...)
	})
	t.Run("2084", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2084")
		assertFuzzResults(t, in, "", nil, 0, 0)
		assertFuzzMemorySHA(t, in, 393216, "a6619f482fee91a315f76cdcd8705d39b6ce11077c435ccc696142e130c27762")
		assertFuzzGlobal64(t, in, 0, 0)
	})
	t.Run("2096", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2096")
		assertFuzzTrap(t, in, "", TrapLinMemOutOfBounds)
		assertFuzzMemorySHA(t, in, 458752, "183facbc37f3a18e7fbdd26d4963c44885692139b16d1da7a41da17e5770f9e1")
	})
	t.Run("2097", func(t *testing.T) {
		b := mustDecodeHex(t, "0061736d010000000107016000037f7f7f02010003030200000a49022f0010010004001001027d00024002400240024002400240440000000000000000000b0b0b0b0b0b000b0005000b000b1703017c017c017e00000000000000000000000b43420b0b")
		if _, err := Compile(nil, b); err == nil || (!strings.Contains(err.Error(), "section size mismatch") && !strings.Contains(err.Error(), "unexpected")) {
			t.Fatalf("malformed function error = %v", err)
		}
	})
	t.Run("2112", func(t *testing.T) {
		b := mustDecodeHex(t, "0061736d0100000001050160017e00020100030201000404017000000a06010400fc300b")
		if _, err := Compile(nil, b); err == nil || (!strings.Contains(err.Error(), "invalid instruction") && !strings.Contains(err.Error(), "opcode")) {
			t.Fatalf("unknown misc opcode error = %v", err)
		}
	})
	t.Run("2118", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2118")
		assertFuzzTrapArgs(t, in, "", TrapUnreachable, make([]uint64, 9)...)
		assertFuzzMemorySHA(t, in, 131072, "574af21d29662e8a7c27e13fe485cce02b09912109a3357d074d45b657d5956f")
	})
	t.Run("2131_threads_fail_closed", func(t *testing.T) {
		b := readFuzzFixture(t, "2131")
		if _, err := Compile(nil, b); err == nil || !strings.Contains(err.Error(), "shared") {
			t.Fatalf("threads module error = %v, want explicit shared-memory rejection", err)
		}
	})
	t.Run("2136", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2136")
		assertFuzzResults(t, in, "", []uint64{1333788672})
		assertFuzzMemorySHA(t, in, 196608, "25496ae941838ca34ec9a64331284987e3462bd31509e898cb6aff15342276f2")
	})
	for _, id := range []string{"2137", "2140"} {
		id := id
		t.Run(id+"_extended_const", func(t *testing.T) {
			// FEATURES.md advertises extended constant expressions, so these
			// imported-global element initializers are required to compile and link.
			testExtendedConstElementFixture(t, id)
		})
	}
	t.Run("2201", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2201")
		assertFuzzTrapArgs(t, in, "", TrapLinMemOutOfBounds, 0, 0)
		assertFuzzMemorySHA(t, in, 1245184, "8af6704aa6fc1cc76b5e49190b4b337c9f60c71e4d242f5481077db53457afd2")
	})
	t.Run("2260", func(t *testing.T) {
		in := instantiateFuzzFixture(t, "2260")
		assertFuzzResults(t, in, "", nil, 0)
		assertFuzzGlobal128(t, in, 0, 9223372039002259456, 9223372041137523338)
		assertFuzzGlobal128(t, in, 1, 9223372039002259456, 9223372041137523338)
		assertFuzzGlobal128(t, in, 2, 9223090564025483264, 9223090561878158282)
		assertFuzzGlobal64(t, in, 3, 0)
	})
}

func readFuzzFixture(t *testing.T, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "regressions", "fuzzcases", id+".wasm"))
	if err != nil {
		t.Fatalf("read fuzz fixture %s: %v", id, err)
	}
	return b
}

func instantiateFuzzFixture(t *testing.T, id string) *Instance {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), readFuzzFixture(t, id))
	if err != nil {
		t.Fatalf("compile fuzz fixture %s: %v", id, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{}})
	if err != nil {
		t.Fatalf("instantiate fuzz fixture %s: %v", id, err)
	}
	t.Cleanup(func() { _ = in.Close() })
	return in
}

func assertFuzzInstantiateError(t *testing.T, id, contains string) {
	t.Helper()
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), readFuzzFixture(t, id))
	if err != nil {
		t.Fatalf("compile fuzz fixture %s: %v", id, err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{}})
	if in != nil {
		_ = in.Close()
	}
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("instantiate %s error = %v, want substring %q", id, err, contains)
	}
}

func invokeFuzzFixture(t *testing.T, in *Instance, export string, args ...uint64) []uint64 {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err != nil {
		t.Fatalf("invoke %q%v: %v", export, args, err)
	}
	return got
}

func assertFuzzResults(t *testing.T, in *Instance, export string, want []uint64, args ...uint64) {
	t.Helper()
	got := invokeFuzzFixture(t, in, export, args...)
	if want == nil {
		if len(got) != 0 {
			t.Fatalf("%q%v = %#v, want no results", export, args, got)
		}
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q%v = %#v, want %#v", export, args, got, want)
	}
}

func assertFuzzNoTrap(t *testing.T, in *Instance, export string, args ...uint64) {
	t.Helper()
	if _, err := in.Invoke(export, args...); err != nil {
		t.Fatalf("invoke %q%v: %v", export, args, err)
	}
}

func assertFuzzTrap(t *testing.T, in *Instance, export string, code TrapCode) {
	t.Helper()
	assertFuzzTrapArgs(t, in, export, code)
}

func assertFuzzTrapArgs(t *testing.T, in *Instance, export string, code TrapCode, args ...uint64) {
	t.Helper()
	got, err := in.Invoke(export, args...)
	if err == nil {
		t.Fatalf("%q%v = %#v, want trap %s", export, args, got, code)
	}
	var trap *TrapError
	if !errors.As(err, &trap) || trap.Code != code {
		t.Fatalf("%q%v error = %v, want trap %s", export, args, err, code)
	}
}

func assertFuzzGlobal64(t *testing.T, in *Instance, index int, want uint64) {
	t.Helper()
	if index < 0 || index >= len(in.c.Globals) || index >= len(in.globalCells) {
		t.Fatalf("global index %d out of range", index)
	}
	got := readGlobalObject(in.globalCells[index], in.c.Globals[index].Type)
	if got != want {
		t.Fatalf("global[%d] = %#x, want %#x", index, got, want)
	}
}

func assertFuzzGlobal128(t *testing.T, in *Instance, index int, wantLo, wantHi uint64) {
	t.Helper()
	if index < 0 || index >= len(in.c.Globals) || index >= len(in.globalCells) {
		t.Fatalf("global index %d out of range", index)
	}
	v := readGlobalObjectV128(in.globalCells[index])
	lo, hi := binary.LittleEndian.Uint64(v[:8]), binary.LittleEndian.Uint64(v[8:])
	if lo != wantLo || hi != wantHi {
		t.Fatalf("global[%d] = (%#x,%#x), want (%#x,%#x)", index, lo, hi, wantLo, wantHi)
	}
}

func assertFuzzMemorySHA(t *testing.T, in *Instance, wantLen int, want string) {
	t.Helper()
	if in.Memory() == nil {
		t.Fatal("module has no memory")
	}
	b := in.Memory().Bytes()
	if len(b) != wantLen {
		t.Fatalf("memory length = %d, want %d", len(b), wantLen)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(b))
	if got != want {
		t.Fatalf("memory sha256 = %s, want %s", got, want)
	}
}

func testFuzzRegression888(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	providerBytes := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x00, 0x05})),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("", 3, 0),
			wasmtest.ExportEntry("s", 2, 0),
		)),
	)
	providerModule, err := rt.Compile(providerBytes)
	if err != nil {
		t.Fatalf("compile provider: %v", err)
	}
	provider, err := rt.Instantiate(context.Background(), providerModule)
	if err != nil {
		t.Fatalf("instantiate provider: %v", err)
	}
	defer provider.Close()
	global, err := provider.ExportedGlobalObject("")
	if err != nil {
		t.Fatalf("provider global: %v", err)
	}
	memory, err := provider.ExportedMemory("s")
	if err != nil {
		t.Fatalf("provider memory: %v", err)
	}
	consumerModule, err := rt.Compile(readFuzzFixture(t, "888"))
	if err != nil {
		t.Fatalf("compile 888: %v", err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(Imports{"host.": global, "host.s": memory}))
	if err != nil {
		t.Fatalf("instantiate 888 with imported funcref global: %v", err)
	}
	defer consumer.Close()
}

func testExtendedConstElementFixture(t *testing.T, id string) {
	t.Helper()
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)))
	defer rt.Close()
	mod, err := rt.Compile(readFuzzFixture(t, id))
	if err != nil {
		t.Fatalf("compile extended-const fixture %s: %v", id, err)
	}

	blob, err := mod.c.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal extended-const fixture %s: %v", id, err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatalf("load extended-const fixture %s: %v", id, err)
	}
	defer loaded.Close()
	foundGlobalInit := false
	for _, elems := range [][]ElemInit{loaded.Elems, loaded.passiveElems} {
		for _, elem := range elems {
			for _, value := range elem.Values {
				foundGlobalInit = foundGlobalInit || value.HasGlobal
			}
		}
	}
	if !foundGlobalInit {
		t.Fatalf("codec lost global.get element initializer for %s", id)
	}

	imports := Imports{}
	for _, imp := range mod.Imports() {
		if imp.Kind != ImportGlobal {
			continue
		}
		var global *Global
		switch imp.Type {
		case ValExternRef:
			global, err = rt.NewExternRefGlobal(NullExternRef(), false)
		case ValFuncRef:
			global, err = rt.NewFuncRefGlobal(NullFuncRef(), false)
		default:
			t.Fatalf("fixture %s imported unexpected global type %s", id, imp.Type)
		}
		if err != nil {
			t.Fatalf("create %s global for %s: %v", imp.Type, id, err)
		}
		defer global.Close()
		imports[imp.Key()] = global
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
	if err != nil {
		t.Fatalf("instantiate extended-const fixture %s: %v", id, err)
	}
	defer in.Close()
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b := make([]byte, len(s)/2)
	for i := range b {
		var x byte
		if _, err := fmt.Sscanf(s[i*2:i*2+2], "%02x", &x); err != nil {
			t.Fatalf("decode hex: %v", err)
		}
		b[i] = x
	}
	return b
}
