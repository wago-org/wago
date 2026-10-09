//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"math"
	"reflect"
	"testing"
)

func TestExperimentalMaps(t *testing.T) {
	for _, name := range []string{"map-f32", "map-i32"} {
		t.Run(name, func(t *testing.T) {
			mode := "vector-i32"
			if name == "map-f32" {
				mode = "vector-f32"
			}
			m := loopExperimentModule(t, name)
			lowered, reason, err := shared.RewriteReplication(m, mode)
			if err != nil || reason != "accepted" || lowered == m {
				t.Fatal(reason, err)
			}
			ref := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true}, 65536)
			got := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode}, 65536)
			fast := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode + "-assert"}, 65536)
			for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 15, 16, 17, 31, 32, 33, 511, 512, 513, 8192} {
				for _, pair := range [][2]uint64{{128, 32768}, {128, 128}, {129, 128}, {128, 129}, {132, 128}, {128, 132}, {1, 32769}, {65520, 128}, {128, 65520}} {
					init := func(mem []byte) {
						for i := range mem {
							mem[i] = 0xa5
						}
						values := []uint32{0, 0x80000000, 1, 0x7f800000, 0xff800000, 0x7fc12345, 0x3f800001, 0x3f7fffff, 0xffffffff, 0x80000001}
						for i := uint64(0); i < n && pair[1]+i*4+4 <= 65536; i++ {
							binary.LittleEndian.PutUint32(mem[pair[1]+i*4:], values[i%uint64(len(values))])
						}
					}
					init(ref.jm.CurrentBytes())
					copy(got.jm.CurrentBytes(), ref.jm.CurrentBytes())
					copy(fast.jm.CurrentBytes(), ref.jm.CurrentBytes())
					scale, bias := uint64(0xffffffff), uint64(0x80000001)
					if name == "map-f32" {
						scale, bias = uint64(math.Float32bits(1.0000001)), uint64(math.Float32bits(-1))
					}
					args := []uint64{pair[0], pair[1], n, scale, bias}
					a, b := ref.call(args...), got.call(args...)
					if (a != nil) != (b != nil) || a == nil && !bytes.Equal(ref.out, got.out) || !bytes.Equal(ref.jm.CurrentBytes(), got.jm.CurrentBytes()) {
						t.Fatalf("n=%d pair=%v errors=%v/%v outputs=%x/%x", n, pair, a, b, ref.out[:32], got.out[:32])
					}
					endD, endS := pair[0]+n*4, pair[1]+n*4
					expectFast := endD <= 65536 && endS <= 65536 && (pair[0] == pair[1] || endD <= pair[1] || endS <= pair[0])
					fe := fast.call(args...)
					if expectFast && fe != nil {
						t.Fatal("guard failed for eligible input", n, pair, fe)
					}
					if !expectFast && fe == nil {
						t.Fatal("guard incorrectly passed", n, pair)
					}
				}
			}
			if diagnosticsEnabled {
				for _, variant := range []string{"", mode} {
					var stats ModuleStats
					cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: variant, Stats: &stats})
					if err != nil {
						t.Fatal(err)
					}
					x := stats.Funcs[0]
					t.Logf("mode=%s module=%d function=%d spills=%d reloads=%d frame=%d", variant, len(cm.Code), x.CodeBytes, x.Spills, x.Reloads, x.FrameBytes)
					cm.CodeImage.Close()
				}
			}
		})
	}
}

func TestExperimentalTrapOrigins(t *testing.T) {
	for _, mode := range []string{"count2", "count4", "guard2", "vector-i32"} {
		for _, name := range []string{"map-i32", "pointer"} {
			m := loopExperimentModule(t, name)
			ref := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true}, 65536)
			got := newLoopExperimentRun(t, m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode}, 65536)
			for _, args := range [][]uint64{{128, 65532, 4, 3, 7}, {65532, 128, 4, 3, 7}} {
				if name == "pointer" {
					args = []uint64{65536, 4, 0}
				}
				a, b := ref.call(args...), got.call(args...)
				ta, oka := a.(*coreruntime.TrapError)
				tb, okb := b.(*coreruntime.TrapError)
				if !oka || !okb || ta.Code != tb.Code || !reflect.DeepEqual(ta.Frames, tb.Frames) {
					t.Fatalf("%s trap mismatch: %v / %v", mode, a, b)
				}
			}
		}
	}
}

func TestExperimentalMapOperationsAndImmediateOffsets(t *testing.T) {
	for _, fp := range []bool{false, true} {
		name, mode, operations := "map-i32", "vector-i32", []byte{0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73}
		if fp {
			name, mode, operations = "map-f32", "vector-f32", []byte{0x92, 0x93, 0x94}
		}
		base := loopExperimentModule(t, name)
		var plan shared.MapPlan
		if why := shared.InspectMap(base, 0, fp, &plan); why != "" {
			t.Fatal(why)
		}
		for _, first := range operations {
			for _, second := range operations {
				m := *base
				m.Code = append([]wasm.Func(nil), base.Code...)
				body := append([]byte(nil), base.Code[0].BodyBytes...)
				body[plan.Loop.BodyStart+int(plan.Loop.Operations[4].Start)] = first
				body[plan.Loop.BodyStart+int(plan.Loop.Operations[6].Start)] = second
				m.Code[0].BodyBytes = body
				ref := newLoopExperimentRun(t, &m, CompileOptions{AMD64FeaturesSet: true}, 65536)
				got := newLoopExperimentRun(t, &m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode}, 65536)
				scales, biases := []uint32{0xffffffff, 0x80000000, 0, 1}, []uint32{0xffffffff, 0x80000001, 0}
				if fp {
					scales = []uint32{0, 0x80000000, 0x3f800000, 0xbf800000, 1, 0x7f800000, 0x7fc12345}
					biases = []uint32{0, 0x80000000, 0xbf800000}
				}
				for _, scale := range scales {
					for _, bias := range biases {
						for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 17} {
							values := []uint32{0, 0x80000000, 1, 0x3f800001, 0x3f7fffff, 0x7fc54321, 0x7f800000, 0xff800000}
							clear(ref.jm.CurrentBytes())
							for i := uint64(0); i < n; i++ {
								binary.LittleEndian.PutUint32(ref.jm.CurrentBytes()[256+i*4:], values[i%uint64(len(values))])
							}
							copy(got.jm.CurrentBytes(), ref.jm.CurrentBytes())
							args := []uint64{128, 256, n, uint64(scale), uint64(bias)}
							a, b := ref.call(args...), got.call(args...)
							same := bytes.Equal(ref.out, got.out) && bytes.Equal(ref.jm.CurrentBytes(), got.jm.CurrentBytes())
							if fp {
								same = experimentFPMapEqual(ref, got, n, values, scale, bias, first)
							}
							if a != nil || b != nil || !same {
								t.Fatalf("fp=%v ops=%x/%x scale=%x bias=%x n=%d errors=%v/%v", fp, first, second, scale, bias, n, a, b)
							}
						}
					}
				}
			}
		}
		for _, which := range []int{2, 8} {
			m := *base
			m.Code = append([]wasm.Func(nil), base.Code...)
			m.Code[0].BodyBytes = append([]byte(nil), base.Code[0].BodyBytes...)
			x := plan.Loop.Operations[which]
			m.Code[0].BodyBytes[plan.Loop.BodyStart+int(x.End)-1] = 4
			if out, reason, err := shared.RewriteReplication(&m, mode); err != nil || out != &m || reason != "map-immediate-offset" {
				t.Fatal("offset was folded into wrapping address", reason, err)
			}
			m.Memories = append([]wasm.MemType(nil), base.Memories...)
			m.Memories[0].Limits.Max = 65536
			ref := newLoopExperimentRun(t, &m, CompileOptions{AMD64FeaturesSet: true}, 1<<32)
			got := newLoopExperimentRun(t, &m, CompileOptions{AMD64FeaturesSet: true, ExperimentalLoopMode: mode}, 1<<32)
			args := []uint64{256, 0xfffffffc, 1, 3, 7}
			if which == 8 {
				args[0], args[1] = 0xfffffffc, 256
			}
			a, b := ref.call(args...), got.call(args...)
			if a == nil || b == nil {
				t.Fatal("widened immediate did not trap", which, a, b)
			}
		}
	}
}

func TestExperimentalSuffixOrigins(t *testing.T) {
	m := loopExperimentModule(t, "map-i32")
	old := append([]byte(nil), m.Code[0].BodyBytes...)
	suffix := []byte{0x41, 0, 0xfd, 0, 0, 0, 0x1a, 0x41, 0, 0xfd, 12}
	suffix = append(suffix, make([]byte, 16)...)
	suffix = append(suffix, 0xfd, 11, 0, 0, 0x0b)
	m.Code[0].BodyBytes = append(append([]byte(nil), old[:len(old)-1]...), suffix...)
	out, why, err := shared.RewriteReplication(m, "vector-i32")
	if err != nil || why != "accepted" {
		t.Fatal(why, err)
	}
	newStart := len(out.Code[0].BodyBytes) - len(suffix)
	oldStart := len(old) - 1
	for i := range suffix {
		if out.ExperimentalInstructionOrigins[0][newStart+i] != uint32(oldStart+i) {
			t.Fatalf("suffix byte %d origin=%d want=%d", i, out.ExperimentalInstructionOrigins[0][newStart+i], oldStart+i)
		}
	}
}

func experimentFloatBinary(op byte, a, b float32) float32 {
	switch op {
	case 0x92:
		return a + b
	case 0x93:
		return a - b
	case 0x94:
		return a * b
	}
	panic("unsupported float operation")
}
func experimentIsNaN(v uint32) bool           { return v&0x7f800000 == 0x7f800000 && v&0x7fffff != 0 }
func experimentNoncanonicalNaN(v uint32) bool { return experimentIsNaN(v) && v&0x7fffff != 0x400000 }
func experimentPermittedFloat(actual, expected, input, scale, bias uint32, first byte) bool {
	if !experimentIsNaN(expected) {
		return actual == expected
	}
	if actual&0x7f800000 != 0x7f800000 || actual&0x400000 == 0 {
		return false
	}
	intermediate := math.Float32bits(experimentFloatBinary(first, math.Float32frombits(input), math.Float32frombits(scale)))
	arithmetic := (experimentIsNaN(intermediate) && (experimentNoncanonicalNaN(input) || experimentNoncanonicalNaN(scale))) || experimentNoncanonicalNaN(bias)
	return arithmetic || actual&0x7fffff == 0x400000
}
func experimentFPMapEqual(ref, got *loopExperimentRun, n uint64, inputs []uint32, scale, bias uint32, first byte) bool {
	a, b := ref.jm.CurrentBytes(), got.jm.CurrentBytes()
	end := 128 + int(n)*4
	if !bytes.Equal(a[:128], b[:128]) || !bytes.Equal(a[end:], b[end:]) || !bytes.Equal(ref.out[:24], got.out[:24]) || !bytes.Equal(ref.out[28:], got.out[28:]) {
		return false
	}
	for i := uint64(0); i < n; i++ {
		want := binary.LittleEndian.Uint32(a[128+i*4:])
		actual := binary.LittleEndian.Uint32(b[128+i*4:])
		if !experimentPermittedFloat(actual, want, inputs[i%uint64(len(inputs))], scale, bias, first) {
			return false
		}
	}
	if n == 0 {
		return bytes.Equal(ref.out, got.out)
	}
	return experimentPermittedFloat(binary.LittleEndian.Uint32(got.out[24:]), binary.LittleEndian.Uint32(ref.out[24:]), inputs[(n-1)%uint64(len(inputs))], scale, bias, first)
}
