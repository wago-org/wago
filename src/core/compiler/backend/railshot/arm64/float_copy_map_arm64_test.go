//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type floatCopyCase struct{ src, dst, byteStart, counterStart, limit, loadOffset, storeOffset uint32 }

func floatCopyFixture(t testing.TB, wide bool, packets int, base byte, cfg floatCopyCase) *wasm.Module {
	typ, load, store, size := byte(0x7d), byte(0x2a), byte(0x38), uint32(4)
	if wide {
		typ, load, store, size = 0x7c, 0x2b, 0x39, 8
	}
	body := []byte{2, 7 + base, 0x7f, 1, typ}
	c := func(v uint32) { body = append(body, 0x41); body = append(body, wasmtest.SLEB32(int32(v))...) }
	get := func(v byte) { body = append(body, 0x20, v+base) }
	c(0)
	c(77)
	body = append(body, 0x36, 2, 0)
	for i, v := range []uint32{cfg.src, cfg.dst, cfg.byteStart, cfg.counterStart} {
		c(v)
		body = append(body, 0x21, byte(i)+base)
	}
	c(4)
	body = append(body, 0x28, 2, 0, 0x21, 4+base)
	c(8)
	get(2)
	c(12)
	get(3) // pending old local reads survive the optimized prefix
	body = append(body, 0x02, 0x40)
	get(3)
	get(4)
	body = append(body, 0x46, 0x0d, 0, 0x03, 0x40)
	for i := 0; i < packets; i++ {
		get(1)
		get(2)
		body = append(body, 0x6a)
		c(uint32(i) * size)
		body = append(body, 0x6a, 0x22, 6+base)
		get(0)
		get(2)
		body = append(body, 0x6a)
		c(uint32(i) * size)
		body = append(body, 0x6a, 0x22, 5+base, load, 0)
		body = append(body, wasmtest.ULEB(cfg.loadOffset)...)
		body = append(body, 0x22, 7+base, store, 0)
		body = append(body, wasmtest.ULEB(cfg.storeOffset)...)
	}
	get(2)
	c(uint32(packets) * size)
	body = append(body, 0x6a, 0x21, 2+base)
	get(3)
	c(uint32(packets))
	body = append(body, 0x6a, 0x22, 3+base)
	get(4)
	body = append(body, 0x47, 0x0d, 0, 0x0b, 0x0b, 0x36, 2, 0, 0x36, 2, 0)
	for i, local := range []byte{3, 2, 5, 6} {
		c(16 + uint32(i)*4)
		get(local)
		body = append(body, 0x36, 2, 0)
	}
	c(32)
	get(7)
	body = append(body, store, 0, 0, 0x0b)
	return modMem(t, 1, nil, nil, body)
}

func floatCopyOracle(initial []byte, wide bool, packets int, cfg floatCopyCase) ([]byte, bool) {
	memory := append([]byte(nil), initial...)
	binary.LittleEndian.PutUint32(memory, 77)
	size := uint32(4)
	if wide {
		size = 8
	}
	counter, index := cfg.counterStart, cfg.byteStart
	var source, destination uint32
	var last uint64
	for trips := 0; counter != cfg.limit; trips++ {
		if trips > 20000 {
			panic("copy oracle trip bound")
		}
		for i := 0; i < packets; i++ {
			destination = cfg.dst + index + uint32(i)*size
			source = cfg.src + index + uint32(i)*size
			a := uint64(source) + uint64(cfg.loadOffset)
			b := uint64(destination) + uint64(cfg.storeOffset)
			if a+uint64(size) > uint64(len(memory)) {
				return memory, true
			}
			if wide {
				last = binary.LittleEndian.Uint64(memory[a:])
			} else {
				last = uint64(binary.LittleEndian.Uint32(memory[a:]))
			}
			if b+uint64(size) > uint64(len(memory)) {
				return memory, true
			}
			if wide {
				binary.LittleEndian.PutUint64(memory[b:], last)
			} else {
				binary.LittleEndian.PutUint32(memory[b:], uint32(last))
			}
		}
		index += uint32(packets) * size
		counter += uint32(packets)
	}
	binary.LittleEndian.PutUint32(memory[8:], cfg.byteStart)
	binary.LittleEndian.PutUint32(memory[12:], cfg.counterStart)
	for i, value := range []uint32{counter, index, source, destination} {
		binary.LittleEndian.PutUint32(memory[16+4*i:], value)
	}
	if wide {
		binary.LittleEndian.PutUint64(memory[32:], last)
	} else {
		binary.LittleEndian.PutUint32(memory[32:], uint32(last))
	}
	return memory, false
}

func TestFloatCopyMapMemoryTrapsAndLiveOuts(t *testing.T) {
	old := floatCopyMapEnabled
	defer func() { floatCopyMapEnabled = old }()
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*139 + i/3)
	}
	// Include non-arithmetic payloads: signed zeros, infinities and NaNs must copy bit-for-bit.
	for i, bits := range []uint64{0, 1 << 63, 0x7ff0000000000000, 0x7ff0000000000001, 0x7ff8000000001234, ^uint64(0)} {
		binary.LittleEndian.PutUint64(initial[128+8*i:], bits)
	}
	for _, wide := range []bool{false, true} {
		for _, packets := range []int{1, 2, 3, 4} {
			for _, base := range []byte{0, 32} {
				var cases []floatCopyCase
				for _, n := range []uint32{0, 1, 2, 3, 15, 16, 17, 33} {
					cases = append(cases, floatCopyCase{src: 128, dst: 8192, limit: n * uint32(packets)})
				}
				for _, distance := range []int32{-64, -32, -16, -8, -4, -1, 0, 1, 4, 8, 16, 32, 64} {
					cases = append(cases, floatCopyCase{src: 256, dst: uint32(256 + distance), limit: 65 * uint32(packets)})
				}
				cases = append(cases,
					floatCopyCase{src: 128, dst: 65500, limit: 33 * uint32(packets)},
					floatCopyCase{src: 65500, dst: 8192, limit: 33 * uint32(packets)},
					floatCopyCase{src: 65535, dst: 8192, limit: 33 * uint32(packets)},
					floatCopyCase{src: 128, dst: 65535, limit: 33 * uint32(packets)},
					floatCopyCase{src: 128, dst: 8192, byteStart: 3, limit: 33 * uint32(packets), loadOffset: 1, storeOffset: 5},
					floatCopyCase{src: 128, dst: 8192, limit: 33 * uint32(packets), loadOffset: 0xffffffff},
					floatCopyCase{src: 128, dst: 8192, limit: 33 * uint32(packets), storeOffset: 0xffffffff},
					floatCopyCase{src: 0xffffff80, dst: 8192, byteStart: 128, limit: 33 * uint32(packets)},
					floatCopyCase{src: 128, dst: 0xffffe000, byteStart: 8192, limit: 33 * uint32(packets)},
					floatCopyCase{src: 128, dst: 8192, counterStart: 0xfffffffe, limit: 2},
					floatCopyCase{src: 128, dst: 8192, byteStart: 0xfffffffc, limit: 33 * uint32(packets)},
				)
				for _, cfg := range cases {
					memory := append([]byte(nil), initial...)
					binary.LittleEndian.PutUint32(memory[4:], cfg.limit)
					want, trapped := floatCopyOracle(memory, wide, packets, cfg)
					m := floatCopyFixture(t, wide, packets, base, cfg)
					for _, enabled := range []bool{false, true} {
						floatCopyMapEnabled = enabled
						for _, compact := range []bool{false, true} {
							got, err := runLinearSpanMemory(t, m, CompileOptions{CompactNative: compact}, memory)
							if (err != nil) != trapped || !bytes.Equal(got, want) {
								first := -1
								for i := range want {
									if got[i] != want[i] {
										first = i
										break
									}
								}
								t.Fatalf("wide%v packets%d base%d enabled%v compact%v cfg%+v error%v trap%v first%d", wide, packets, base, enabled, compact, cfg, err, trapped, first)
							}
						}
					}
				}
			}
		}
	}
}

func TestFloatCopyMapAdmitsWideLocalCopy(t *testing.T) {
	requireCompilerDiagnostics(t)
	old := floatCopyMapEnabled
	floatCopyMapEnabled = true
	defer func() { floatCopyMapEnabled = old }()
	for _, wide := range []bool{false, true} {
		m := floatCopyFixture(t, wide, 4, 32, floatCopyCase{src: 128, dst: 8192, limit: 256})
		stats := compileWithStats(t, m, false)
		if stats.Funcs[0].Peephole["float-copy-map-vector"] != 1 {
			t.Fatalf("wide%v stats%v", wide, stats.Funcs[0].Peephole)
		}
	}
}

func TestFloatCopyMapProbeBudgetAndReader(t *testing.T) {
	f := fn{nLocals: 1, localType: []machineType{mtI32}, floatCopyMapProbe: true, integerLoadMapWork: integerLoadMapWorkLimit - 1}
	reader := wasm.NewReader([]byte{0x41, 0, 0x21, 0, 0x41, 1, 0x1a})
	if p, ok := f.inspectFloatMapLoop(reader); ok || p.work <= p.workLimit {
		t.Fatal("exhausted copy budget accepted")
	}
	if reader.Offset() != 0 {
		t.Fatal("probe changed original reader")
	}
	if n := testing.AllocsPerRun(100, func() { f.inspectFloatMapLoop(reader) }); n != 0 {
		t.Fatalf("rejected probe allocated %g", n)
	}
	f = fn{}
	rejected := wasm.NewReader([]byte{0x1a})
	if n := testing.AllocsPerRun(100, func() { f.integerLoadMapWork = 0; f.emitFloatCopyMapCandidate(rejected) }); n != 0 {
		t.Fatalf("candidate miss allocated %g", n)
	}
	if rejected.Offset() != 0 || f.floatCopyMapProbe {
		t.Fatal("candidate leaked reader or admission state")
	}
	f.integerLoadMapWork = integerLoadMapWorkLimit - 1
	if f.hasFloatCopyMapPrefix(reader) || f.integerLoadMapWork != integerLoadMapWorkLimit || reader.Offset() != 0 {
		t.Fatal("prefix budget not saturated privately")
	}
}

func TestFloatCopyMapCallerKeepsVectorAndFloat(t *testing.T) {
	old := floatCopyMapEnabled
	defer func() { floatCopyMapEnabled = old }()
	cfg := floatCopyCase{src: 128, dst: 8192, limit: 256}
	m := floatCopyFixture(t, true, 4, 32, cfg)
	code := m.Code[0]
	callee := wasmtest.ULEB(uint32(len(code.Locals.Runs)))
	for _, run := range code.Locals.Runs {
		callee = append(callee, wasmtest.ULEB(run.Count)...)
		callee = append(callee, wasm.MustEncodeValType(run.Type))
	}
	callee = append(callee, code.BodyBytes...)
	caller := []byte{1, 1, 0x7c, 0x41, 0xc0, 0, 0xfd, 0x0c}
	payload := make([]byte, 16)
	for i := range payload {
		payload[i] = byte(0xf0 - i*11)
	}
	caller = append(caller, payload...)
	caller = appendF64ConstForCacheTest(caller, 123.25)
	caller = append(caller, 0x21, 0, 0x10, 1, 0xfd, 0x0b, 0, 0, 0x41, 48, 0x20, 0, 0x39, 0, 0, 0x0b)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(caller))), caller...), append(wasmtest.ULEB(uint32(len(callee))), callee...))),
	)
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	initial := make([]byte, 65536)
	for i := range initial {
		initial[i] = byte(i*53 + i/7)
	}
	binary.LittleEndian.PutUint32(initial[4:], cfg.limit)
	want, trapped := floatCopyOracle(initial, true, 4, cfg)
	if trapped {
		t.Fatal("unexpected oracle trap")
	}
	copy(want[64:], payload)
	binary.LittleEndian.PutUint64(want[48:], 0x405ed00000000000)
	for _, enabled := range []bool{false, true} {
		floatCopyMapEnabled = enabled
		for _, compact := range []bool{false, true} {
			got, err := runLinearSpanMemory(t, m, CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"inline": false}}, initial)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("enabled%v compact%v error%v memoryEqual%v", enabled, compact, err, bytes.Equal(got, want))
			}
		}
	}
}

func TestFloatCopyMapDeclinesUnsafeEntry(t *testing.T) {
	old := floatCopyMapEnabled
	floatCopyMapEnabled = true
	defer func() { floatCopyMapEnabled = old }()
	for _, mode := range []string{"shared", "memory64", "active-region", "non-control-region"} {
		m := floatCopyFixture(t, true, 4, 32, floatCopyCase{src: 128, dst: 8192, limit: 256})
		f := gpClassEmitter()
		f.policy = currentCodegenPolicy()
		f.m = m
		if !f.opt(optDotLoopVector) {
			t.Fatal("vector option must be enabled for rejection test")
		}
		switch mode {
		case "shared":
			f.threadedMemory0 = true
		case "memory64":
			m.Memories[0].Limits.Addr64 = true
		case "active-region":
			f.intervalLast = []uint32{1}
			f.intervalControl = true
			f.intervalActive = 1
		case "non-control-region":
			f.intervalLast = []uint32{1}
		}
		r := wasm.NewReader([]byte{0x2b, 0, 0})
		f.emitFloatCopyMapLoop(r)
		if f.integerLoadMapWork != 0 || f.a.Len() != 0 || r.Offset() != 0 || f.floatCopyMapProbe {
			t.Fatalf("entry mode%s admitted or changed state", mode)
		}
	}
}
