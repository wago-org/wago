//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64NativeTailConfig() *RuntimeConfig {
	cfg := compatibilityDefaultConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureTailCall | CoreFeatureTypedFunctionReferences)
	cfg.railshotState.tailProducts = true
	return cfg
}
func rv64NativeTailCompile(t *testing.T, name string, source []byte, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_TAIL_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE")
	if mode == "load" {
		if !artifact {
			t.Fatal("compiler-free runner selected source case")
		}
		blob, err := os.ReadFile(filepath.Join(dir, name+".wago"))
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	c, err := rv64NativeTailConfig().Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if artifact || mode == "prepare" {
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if mode == "prepare" {
			if err = os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, name+".wago"), blob, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if artifact {
			c, err = LoadTrustedArtifact(blob)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}
	return c
}
func rv64PublicTailModule(types []wasm.ValType, ordinary bool) []byte {
	im := append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), 0, 0)
	body := []byte{}
	for i := range types {
		body = append(body, 0x20, byte(i))
	}
	body = append(body, 0x12, 0, 0x0b)
	indices := []byte{1, 0}
	codes := [][]byte{wasmtest.Code(body)}
	export := uint32(1)
	if ordinary {
		indices = []byte{2, 0, 0}
		export = 2
		codes = append(codes, wasmtest.Code([]byte{0x20, 0, 0x10, 1, 0x41, 7, 0x6a, 0x0b}))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, indices), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, export))), wasmtest.Section(10, wasmtest.Vec(codes...)))
}
func TestRV64PublicNativeTailSourceArtifactTransportLifetime(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shape := range []string{"zero", "mixed", "pair", "continuation"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, shape), func(t *testing.T) {
				types := []wasm.ValType{}
				var input, want []uint64
				switch shape {
				case "mixed":
					types = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
					input = []uint64{0xcafe123480000001, 0xfedcba9876543210, 0xcafe12347f800123, 0x7ff0000000000001}
					want = []uint64{0x80000001, input[1], 0x7f800123, input[3]}
				case "pair":
					types = []wasm.ValType{wasm.I32, wasm.V128, wasm.F32}
					input = []uint64{0xcafe123480000001, 0xfedcba9876543210, 0xff00aabbccdd1234, 0xcafe12347f800123}
					want = []uint64{0x80000001, input[1], input[2], 0x7f800123}
				case "continuation":
					types = []wasm.ValType{wasm.I32}
					input = []uint64{0xcafe123400000023}
					want = []uint64{42}
				}
				key := "native-tail-" + shape
				pc := rv64NativeTailCompile(t, key+"-producer", rv64PublicNativeIdentityModule(types), artifact)
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				target, err := producer.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				c := rv64NativeTailCompile(t, key+"-caller", rv64PublicTailModule(types, shape == "continuation"), artifact)
				m := rv64NativeTailModule(c)
				if m == nil || len(m.Plans) != 1 {
					t.Fatal("missing persisted source plan")
				}
				roots := producer.referenceLifetime().snapshot().ResourceRoots
				caller, err := Instantiate(c, testImports("env.tail", target))
				if err != nil {
					t.Fatal(err)
				}
				defer caller.Close()
				state := caller.pluginState.Load()
				if state == nil || state.railshotInvocation.tailDispatcher == nil || producer.referenceLifetime().snapshot().ResourceRoots != roots+1 || binary.LittleEndian.Uint64(rv64NativeInvocationContext(caller)[120:]) == 0 {
					t.Fatal("physical tail binding")
				}
				for i := 0; i < 3; i++ {
					got, err := caller.Invoke("call", input...)
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("public native tail", got, want, err)
					}
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if producer.jm == nil {
					t.Fatal("logical close retired attached producer")
				}
				if got, err := caller.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("attached closed tail target", got, err)
				}
				if allocs := testing.AllocsPerRun(10, func() {
					got, err := caller.Invoke("call", input...)
					if err != nil || !slices.Equal(got, want) {
						panic("warm public native tail")
					}
				}); allocs != 0 {
					t.Fatalf("warm allocations %g", allocs)
				}
				if err := caller.Close(); err != nil {
					t.Fatal(err)
				}
				if producer.jm != nil {
					t.Fatal("final caller failed to retire producer")
				}
			})
		}
	}
}

func TestRV64PublicNativeTailAdmissionAndCodec(t *testing.T) {
	source := rv64PublicTailModule([]wasm.ValType{wasm.I32}, false)
	if c, err := compatibilityDefaultConfig().Compile(source); c != nil || err == nil {
		t.Fatal("public default tail gate opened", err)
	}
	c := rv64NativeTailCompile(t, "native-tail-admission", source, false)
	vector := rv64NativeTailCompile(t, "native-tail-vector-host-reject", rv64PublicTailModule([]wasm.ValType{wasm.V128}, false), false)
	if in, err := Instantiate(vector, testImports("env.tail", HostCallFunc(func(HostCall) {}))); in != nil || !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("vector host target published", err)
	}
	// The isolated appendix permits exact negative controls without publishing
	// any executable bytes or relying on a corrupt-artifact checksum failure.
	w := compiledWriter{}
	if err := railshotWriteNativeTail(&w, c); err != nil {
		t.Fatal(err)
	}
	if len(w.buf) != 29 {
		t.Fatal("symbolic tail appendix size", len(w.buf))
	}
	for _, kind := range []string{"version", "function", "import", "frame", "source", "stride", "count", "truncated", "result"} {
		t.Run(kind, func(t *testing.T) {
			blob := bytes.Clone(w.buf)
			v := *c
			v.validateMemo = &validateMemo{}
			v.codeCache = nil
			switch kind {
			case "version":
				binary.LittleEndian.PutUint32(blob, 2)
			case "function":
				binary.LittleEndian.PutUint32(blob[5:], 0)
			case "import":
				binary.LittleEndian.PutUint32(blob[9:], uint32(c.NumImports))
			case "frame":
				binary.LittleEndian.PutUint32(blob[17:], 0xfffffff0)
			case "source":
				binary.LittleEndian.PutUint32(blob[21:], 0xfffffff0)
			case "stride":
				binary.LittleEndian.PutUint32(blob[25:], 4)
			case "count":
				blob[4] = 2
			case "truncated":
				blob = blob[:len(blob)-1]
			case "result":
				v.Funcs = slices.Clone(v.Funcs)
				v.Funcs[0].Results = []ValType{ValI64}
			}
			r := compiledReader{data: blob, budget: newArtifactDecodeBudget(0)}
			if err := railshotReadNativeTail(&r, &v); err == nil {
				t.Fatal("bad plan accepted")
			}
		})
	}
	if err := checkArtifactAdmissionLimits(c, uint64(len(c.code)), 100); err == nil {
		t.Fatal("helper code bypassed code quota")
	}
	if _, err := rv64NativeTailConfig().WithMaxNativeCodeBytes(uint64(len(c.code))).Compile(source); err == nil {
		t.Fatal("source factory code bypassed code quota")
	}
	wide := *c
	wide.validateMemo = &validateMemo{}
	wide.codeCache = nil
	values := make([]ValType, 64)
	for i := range values {
		values[i] = ValI32
	}
	wide.Funcs = []FuncSig{{Params: values, Results: values}}
	wide.importFuncSigs = []FuncSig{{Params: values, Results: values}}
	plan := rv64NativeTailModule(c).Plans[0]
	plan.FrameBytes, plan.SourceBase, plan.SourceStride = 576, 48, 8
	module := &encoder.NativeTailModule{Plans: make([]encoder.NativeTailPlan, 512)}
	for i := range module.Plans {
		module.Plans[i] = plan
	}
	rv64StoreNativeTailModule(&wide, module)
	if err := rv64ValidateNativeTail(&wide); err == nil {
		t.Fatal("wide repeated source signatures passed aggregate metadata budget")
	}
	pc := rv64NativeTailCompile(t, "native-tail-quota-leaf", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	producer, err := Instantiate(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	bindings := testImports("env.tail", export)
	anchor := producer.funcRefDescs
	producer.funcRefDescs = nil
	beforeBase, beforeSealed, beforeRefs := c.codeCache.base, c.codeCache.sealed, c.codeCache.refs
	bad, linkErr := Instantiate(c, bindings)
	producer.funcRefDescs = anchor
	if bad != nil || !errors.Is(linkErr, railshot.ErrUnsupported) || c.codeCache.base != beforeBase || c.codeCache.sealed != beforeSealed || c.codeCache.refs != beforeRefs {
		t.Fatal("anchorless target reached caller publication", linkErr)
	}
	view := c.executionView()
	if err := view.validateCached(); err != nil {
		t.Fatal(err)
	}
	need := view.arenaNeedForImports(bindings.bindings, true) + railshotNativeImportMetadataBytes(view, bindings.bindings)
	roots := producer.referenceLifetime().snapshot().ResourceRoots
	if err := c.prepareCodeMapping(); err != nil {
		t.Fatal(err)
	}
	base, sealed, refs := c.codeCache.base, c.codeCache.sealed, c.codeCache.refs
	if in, err := Instantiate(c, InstantiateOptions{Imports: bindings, maxInstanceMetadataBytes: uint64(need - 1)}); in != nil || !errors.Is(err, ErrResourceLimit) || c.codeCache.base != base || c.codeCache.sealed != sealed || c.codeCache.refs != refs || producer.referenceLifetime().snapshot().ResourceRoots != roots {
		t.Fatalf("capability quota failed after publication/attachment: err=%v limit=%t base=%x/%x sealed=%t/%t refs=%d/%d roots=%d/%d", err, errors.Is(err, ErrResourceLimit), c.codeCache.base, base, c.codeCache.sealed, sealed, c.codeCache.refs, refs, producer.referenceLifetime().snapshot().ResourceRoots, roots)
	}
	if in, err := Instantiate(c, InstantiateOptions{Imports: bindings, maxInstanceMetadataBytes: uint64(need)}); err != nil {
		t.Fatal("exact capability footprint", err)
	} else {
		_ = in.Close()
	}
}

func TestRV64PublicNativeTailOlderArchitecture4Anchor(t *testing.T) {
	pc := rv64NativeTailCompile(t, "native-tail-old4-leaf", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	// Older architecture4 numeric images omitted the redundant context-header
	// requirement. Keep their real native invocation ABI and code unchanged.
	old := cloneCompiledMetadata(pc)
	old.needsFuncRefContextHeader = false
	old.validateMemo = &validateMemo{}
	blob, err := marshalCompiled(old)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if !loaded.needsFuncRefContextHeader {
		t.Fatal("old4 loader failed to derive canonical header")
	}
	producer, err := Instantiate(loaded)
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	c := rv64NativeTailCompile(t, "native-tail-old4-caller", rv64PublicTailModule([]wasm.ValType{wasm.I32}, false), false)
	in, err := Instantiate(c, testImports("env.tail", export))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("call", 0xcafe123480000001); err != nil || !slices.Equal(got, []uint64{0x80000001}) {
		t.Fatal("old4 retained tail target", got, err)
	}
}

func rv64TailHostConsumer(shared bool) []byte {
	imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), 0, 0)}
	if shared {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, 1, 1, 4))
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("safe", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x12, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
}

func TestRV64PublicNativeTailHostPauseReentryColdReuse(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-shared%t", artifact, shared), func(t *testing.T) {
				var memory *Memory
				if shared {
					var err error
					memory, err = NewSharedMemory(1, 4)
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
				}
				var producer, consumer *Instance
				depth, calls := 0, 0
				fail, nested := false, false
				stopped := errors.New("native tail paused host")
				host := CallerHostCallFunc(func(caller Caller, call HostCall) {
					calls++
					if depth == 0 {
						pointer := railshotParkedNativeInvocation(producer)
						view := rv64InvocationViewForTest(producer)
						if pointer < consumer.eng.StackLimit() || pointer+64 > consumer.eng.RV64InvocationViewPointer() || binary.LittleEndian.Uint64(view[24:]) != uint64(pointer-4096) || binary.LittleEndian.Uint64(view[40:]) != uint64(pointer-2048) || binary.LittleEndian.Uint64(view[32:]) != 2048 || binary.LittleEndian.Uint64(view[48:]) != 2048 || binary.LittleEndian.Uint64(view[56:]) == 0 {
							panic("public tail activation lost authority")
						}
						before := bytes.Clone(view)
						if !nested {
							nested = true
							depth++
							got, err := producer.InvokeFromHost(context.Background(), caller, "producer", 8)
							depth--
							if err != nil || !slices.Equal(got, []uint64{9}) || !bytes.Equal(view, before) {
								panic(fmt.Sprintf("tail producer reentry %v %v", got, err))
							}
							if got, err := consumer.InvokeFromHost(context.Background(), caller, "safe", 51); err != nil || !slices.Equal(got, []uint64{51}) || !bytes.Equal(view, before) {
								panic("tail root scratch reentry")
							}
							runtime.GC()
						}
						if fail {
							panic(HostTrap{Err: stopped})
						}
					}
					call.SetI32(0, call.I32(0)+1)
				})
				key := fmt.Sprintf("native-tail-host-shared%t", shared)
				source := rv64PublicNativeHostModule(shared, "producer", []byte{0x20, 0, 0x10, 0, 0x1a})
				imports := testImports("env.host", host)
				if shared {
					imports.Memory("env", "memory", memory)
				}
				var err error
				producer, err = Instantiate(rv64NativeTailCompile(t, key+"-producer", source, artifact), imports)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("producer")
				if err != nil {
					t.Fatal(err)
				}
				imports = testImports("env.tail", export)
				if shared {
					imports.Memory("env", "memory", memory)
				}
				consumer, err = Instantiate(rv64NativeTailCompile(t, key+"-caller", rv64TailHostConsumer(shared), artifact), imports)
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				for i := 0; i < 3; i++ {
					fail = false
					nested = false
					got, err := consumer.Invoke("call", 0xcafe123400000007)
					if err != nil || !slices.Equal(got, []uint64{8}) {
						t.Fatal("paused tail resume", got, err)
					}
					fail = true
					nested = false
					if _, err := consumer.Invoke("call", 7); !errors.Is(err, stopped) {
						t.Fatal("paused tail host trap", err)
					}
					if got, err := consumer.Invoke("safe", 52); err != nil || !slices.Equal(got, []uint64{52}) {
						t.Fatal("tail cold root reuse", got, err)
					}
					if got, err := producer.Invoke("safe", 53); err != nil || !slices.Equal(got, []uint64{53}) {
						t.Fatal("tail cold producer reuse", got, err)
					}
				}
				if calls != 21 {
					t.Fatal("tail two host pauses plus reentry", calls)
				}
			})
		}
	}
}

func TestRV64PublicNativeTailDeepLocalAndForeignChain(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			cfg := rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)
			source := rv64PublicTailModule([]wasm.ValType{wasm.I32}, false)
			c := rv64NativeTailCompile(t, "native-tail-chain", source, artifact)
			c.codeCache.setNativeStackBytes(cfg.NativeStackBytes())
			pc := rv64NativeTailCompile(t, "native-tail-chain-leaf", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
			current, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer current.Close()
			leaf := current
			for i := 0; i < 100; i++ {
				name := "call"
				if i == 0 {
					name = "identity"
				}
				export, err := current.ExportedFunc(name)
				if err != nil {
					t.Fatal(err)
				}
				next, err := Instantiate(c, testImports("env.tail", export))
				if err != nil {
					t.Fatal(err)
				}
				defer next.Close()
				current = next
			}
			// 100 distinct factories would need 556800 bytes if each grew a record.
			if current.eng.RV64InvocationViewPointer()-current.eng.StackLimit() > uintptr(MinNativeStackBytes) {
				t.Fatal("bounded chain stack fixture")
			}
			for i := 0; i < 3; i++ {
				got, err := current.Invoke("call", 0xcafe123480000001)
				if err != nil || !slices.Equal(got, []uint64{0x80000001}) {
					t.Fatal("shared proper-tail exit chain", got, err)
				}
			}
			if leaf.jm == nil {
				t.Fatal("attached leaf lost ownership")
			}
			// Deep local tails followed by a genuine foreign hop use the same public
			// framed source compiler and transported root result destination.
			im := append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), 0, 0)
			body := []byte{0x20, 0, 0x45, 0x04, 0x7f, 0x41, 47, 0x12, 0, 0x05, 0x20, 0, 0x41, 1, 0x6b, 0x12, 1, 0x0b, 0x0b}
			source = wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			local := rv64NativeTailCompile(t, "native-tail-deep-local", source, artifact)
			local.codeCache.setNativeStackBytes(cfg.NativeStackBytes())
			export, err := leaf.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(local, testImports("env.tail", export))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("call", 20000); err != nil || !slices.Equal(got, []uint64{47}) {
				t.Fatal("deep local to native tail", got, err)
			}
			if binary.LittleEndian.Uint64(rv64InvocationViewForTest(in)) != abi.RV64InvocationMagic {
				t.Fatal("root view restored after tail chain")
			}
		})
	}
}

// Header-only contexts provide canonical producer identity without eagerly
// allocating indexed ref.func payloads or granting a reference-call target.
func rv64HeaderOnlyContext(in *Instance) bool {
	if in.c.needsFuncRefDescs() {
		return false
	}
	if in.c.NumImports+len(in.c.Funcs) == 0 {
		return len(in.funcRefDescs) == 0
	}
	return len(in.funcRefDescs) == coreruntime.FuncRefDescBytes && binary.LittleEndian.Uint64(in.funcRefDescs[coreruntime.FuncRefContextOffset:]) == uint64(in.nativeContext) && binary.LittleEndian.Uint64(in.funcRefDescs[coreruntime.TableEntryHomeLinMemOffset:]) == uint64(in.jm.LinMemBase())
}

func TestRV64PublicNativeTailCancellationAndCloseWhileProducerParked(t *testing.T) {
	rv64TailCancellationAndCloseWhileProducerParked(t, false, false)
}

func rv64TailCancellationAndCloseWhileProducerParked(t *testing.T, dynamic, reference bool) {
	for _, closeRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifacttrue-close%t", closeRoot), func(t *testing.T) {
			// An independently retained cell witnesses every incorrect guest resume,
			// including after the producer and consumer have physically closed.
			flag := NewGlobalI32(0, true)
			defer flag.Close()
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "flag", wasm.I32, true), append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))),
				wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("producer", 0, 1))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x24, 0, 0x23, 0, 0x0b}))),
			)
			producerCode := rv64NativeTailCompile(t, "native-tail-close-producer", source, true)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			producerImports := testImports("env.host", func(v int32) int32 { close(entered); <-release; return v + 1 }).Global("env", "flag", flag)
			producer, err := Instantiate(producerCode, producerImports)
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			export, err := producer.ExportedFunc("producer")
			if err != nil {
				t.Fatal(err)
			}
			callerSource, callerImports, args := rv64PublicTailModule([]wasm.ValType{wasm.I32}, false), testImports("env.tail", export), []uint64{7}
			if dynamic || reference {
				callerSource = rv64DynamicNativeImportModule([]wasm.ValType{wasm.I32}, false)
				if reference {
					callerSource = rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true)
				}
				callerImports = NewImports().Function("env", "target", export)
				args = append(args, 0)
			}
			consumer, err := Instantiate(rv64NativeTailCompile(t, fmt.Sprintf("native-tail-close-caller-dynamic%t-reference%t", dynamic, reference), callerSource, true), callerImports)
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() { defer close(finished); _, err := consumer.InvokeContext(ctx, "call", args...); done <- err }()
			defer func() {
				cancel()
				unblock()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("foreign activation cleanup did not finish")
					<-finished
				}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("foreign host did not park")
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := producerCode.Close(); err != nil {
				t.Fatal(err)
			}
			if producer.jm == nil || len(producer.ctrl) == 0 || rv64EntryCapability(consumer) == 0 {
				t.Fatal("parked retained producer released")
			}
			if closeRoot {
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			if err := consumer.c.Close(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			if atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
				t.Fatal("foreign watcher did not interrupt")
			}
			unblock()
			select {
			case err := <-done:
				var trap *coreruntime.TrapError
				if closeRoot {
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
						t.Fatal("foreign Close unwind", err)
					}
				} else if !errors.Is(err, context.Canceled) {
					t.Fatal("foreign cancellation", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("foreign unwind did not finish")
			}
			if closeRoot && (producer.jm != nil || consumer.pluginState.Load().railshotInvocation.tailDispatcher != nil) {
				t.Fatal("foreign physical ownership leaked after unwind")
			}
			if flag.Get() != 0 {
				t.Fatal("interrupted foreign guest resumed its post-host store")
			}
		})
	}
}
