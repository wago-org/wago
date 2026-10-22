//go:build linux && riscv64 && !wago_precompiled && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ForeignFunctionSubtypeSources(vector bool, includeTails ...bool) (producer, consumer []byte) {
	names := []string{"indirect", "reference", "test", "cast", "tail-indirect", "tail-reference"}
	if len(includeTails) != 0 && !includeTails[0] {
		names = names[:4]
	}
	return rv64ForeignFunctionSubtypeSourcesFor(vector, names)
}

func rv64ForeignFunctionSubtypeSourcesFor(vector bool, names []string, exact ...bool) (producer, consumer []byte) {
	signature := []byte{0x60, 0, 1, 0x7f}
	if vector {
		signature = []byte{0x60, 1, 0x7b, 2, 0x7f, 0x7b}
	}
	base := append([]byte{0x50, 0}, signature...)
	subtype := append([]byte{0x4f, 1, 0}, signature...)
	target := func(value byte) []byte {
		b := []byte{0x41, value}
		if vector {
			b = append(b, 0x20, 0)
		}
		return wasmtest.Code(append(b, 0x0b))
	}
	targetType := byte(1)
	if len(exact) != 0 && exact[0] {
		targetType = 0
	}
	producer = wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(base, subtype, signature)), wasmtest.Section(3, []byte{2, targetType, 2}),
		wasmtest.Section(4, []byte{1, 0x70, 0, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 2, 0, 1}), wasmtest.Section(10, wasmtest.Vec(target(42), target(7))))
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 0, 2)
	var functions, exports, bodies [][]byte
	for _, name := range names {
		for _, unrelated := range []bool{false, true} {
			index := byte(0)
			export := name
			if unrelated {
				index = 1
				export += "-unrelated"
			}
			tail := strings.HasPrefix(name, "tail-")
			body := []byte{}
			if vector && (name == "indirect" || name == "reference" || tail) {
				if tail {
					body = append(body, 0x20, 0)
				} else {
					body = append(body, 0xfd, 12)
					body = append(body, make([]byte, 16)...)
				}
			}
			body = append(body, 0x41, index)
			switch name {
			case "indirect":
				body = append(body, 0x11, 0, 0)
			case "reference":
				body = append(body, 0x25, 0, 0xfb, 22, 0, 0x14, 0)
			case "test":
				body = append(body, 0x25, 0, 0xfb, 20, 0)
			case "cast":
				body = append(body, 0x25, 0, 0xfb, 22, 0, 0x1a, 0x41, 1)
			case "tail-indirect":
				body = append(body, 0x13, 0, 0)
			case "tail-reference":
				body = append(body, 0x25, 0, 0xfb, 22, 0, 0x15, 0)
			}
			if vector && (name == "indirect" || name == "reference") {
				body = append(body, 0x1a)
			}
			typeIndex := byte(1)
			if tail {
				typeIndex = 0
			}
			functions = append(functions, []byte{typeIndex})
			exports = append(exports, wasmtest.ExportEntry(export, 0, uint32(len(bodies))))
			bodies = append(bodies, wasmtest.Code(append(body, 0x0b)))
		}
	}
	consumer = wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(base, []byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(tableImport)), wasmtest.Section(3, wasmtest.Vec(functions...)), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	return
}

func TestRV64ForeignFunctionSubtypeWarmAllocations(t *testing.T) {
	for _, vector := range []bool{false, true} {
		var exactAllocations [4]float64
		for _, exact := range []bool{true, false} {
			t.Run(fmt.Sprintf("vector%t/exact%t", vector, exact), func(t *testing.T) {
				ps, cs := rv64ForeignFunctionSubtypeSourcesFor(vector, []string{"indirect", "reference", "test", "cast"}, exact)
				cfg := NewRuntimeConfig()
				pc, err := cfg.Compile(ps)
				if err != nil {
					t.Fatal(err)
				}
				defer pc.Close()
				cc, err := cfg.Compile(cs)
				if err != nil {
					t.Fatal(err)
				}
				defer cc.Close()
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				table, err := producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				consumer, err := Instantiate(cc, NewImports().Table("env", "table", table))
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				for index, name := range []string{"test", "cast", "indirect", "reference"} {
					fn, e := consumer.WasmFunc(name)
					if e != nil {
						t.Fatal(e)
					}
					session, e := fn.OpenSession()
					if e != nil {
						t.Fatal(e)
					}
					want := uint64(1)
					if name == "indirect" || name == "reference" {
						want = 42
					}
					invoke := func() {
						got, e := session.Invoke()
						if e != nil || len(got) != 1 || got[0] != want {
							panic(fmt.Sprintf("%s=%v,%v", name, got, e))
						}
					}
					invoke()
					allocations := testing.AllocsPerRun(20, invoke)
					t.Logf("%s public entry Go allocations=%g native image=%dB", name, allocations, len(cc.code))
					if exact {
						exactAllocations[index] = allocations
					} else if allocations != exactAllocations[index] {
						t.Errorf("%s subtype allocations=%g; exact-key control=%g", name, allocations, exactAllocations[index])
					}
					session.Close()
				}
				gate := consumer.lockInvocation(0)
				defer gate.unlockInvocation()
				entry, err := consumer.beginNativeEntry()
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.unlockNativeEntry(entry)
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("producer descriptor is unavailable")
				}
				args, results := []uint64{descriptor, 0, 0}, []uint64{0}
				allocations := testing.AllocsPerRun(100, func() {
					consumer.dispatchGCStructHelperParked(0, gcFuncRefTest, 0, args, results, nil)
					if results[0] != 1 {
						panic("foreign subtype helper rejected retained target")
					}
				})
				t.Logf("retained owner subtype helper Go allocations=%g", allocations)
				if allocations != 0 {
					t.Fatalf("retained helper allocates %g times", allocations)
				}
			})
		}
	}
}

func TestRV64ForeignFunctionSubtypeIsolatedCalls(t *testing.T) {
	for _, kind := range []string{"indirect", "reference"} {
		for _, vector := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/vector%t/artifact%t", kind, vector, artifact), func(t *testing.T) {
					cfg := NewRuntimeConfig()
					compile := func(source []byte) *Compiled {
						t.Helper()
						c, err := cfg.Compile(source)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { c.Close() })
						if artifact {
							blob, e := c.MarshalBinary()
							if e != nil {
								t.Fatal(e)
							}
							c, e = LoadTrustedArtifact(blob)
							if e != nil {
								t.Fatal(e)
							}
							t.Cleanup(func() { c.Close() })
						}
						return c
					}
					ps, cs := rv64ForeignFunctionSubtypeSourcesFor(vector, []string{kind})
					producer, err := Instantiate(compile(ps))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					table, err := producer.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					code := compile(cs)
					if !code.usesDynamicFuncRefTest() {
						t.Fatal("call-only image lacks exact subtype helper metadata")
					}
					consumer, err := Instantiate(code, NewImports().Table("env", "table", table))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					producer.Close()
					got, err := consumer.Invoke(kind)
					if err != nil || !slices.Equal(got, []uint64{42}) {
						t.Fatalf("related call=%v,%v", got, err)
					}
					if got, err = consumer.Invoke(kind + "-unrelated"); err == nil {
						t.Fatalf("unrelated call returned %v", got)
					}
				})
			}
		}
	}
}

func TestRV64ForeignFunctionSubtypeNestedOwnerScope(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			testRV64ForeignFunctionSubtypeNestedOwnerScope(t, false, artifact)
		})
	}
}

func TestRV64ForeignFunctionSubtypeClosedNestedOwnerScope(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			testRV64ForeignFunctionSubtypeNestedOwnerScope(t, true, artifact)
		})
	}
}

func testRV64ForeignFunctionSubtypeNestedOwnerScope(t *testing.T, closeConsumer, artifact bool) {
	cfg := NewRuntimeConfig()
	compile := func(source []byte) *Compiled {
		t.Helper()
		c, err := cfg.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		if artifact {
			blob, e := c.MarshalBinary()
			if e != nil {
				t.Fatal(e)
			}
			c, e = LoadTrustedArtifact(blob)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { c.Close() })
		}
		return c
	}
	ps, cs := rv64ForeignFunctionSubtypeSourcesFor(false, []string{"test"})
	producer, err := Instantiate(compile(ps))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	table, err := producer.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	consumer, err := Instantiate(compile(cs), NewImports().Table("env", "table", table))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	target, err := consumer.ExportedFunc("test")
	if err != nil {
		t.Fatal(err)
	}
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("test")...), 0, 0)
	outerSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f})), wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
	outer, err := Instantiate(compile(outerSource), NewImports().Function("env", "test", target))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		outer.Close()
		consumer.Close()
		producer.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, in := range []*Instance{outer, consumer, producer} {
			if err := in.WaitClosed(ctx); err != nil {
				t.Errorf("nested owner physical close: %v", err)
			}
			in.lifeMu.Lock()
			physical := in.resourcesClosed
			in.lifeMu.Unlock()
			if !physical {
				t.Error("nested owner retains physical resources after final importer close")
			}
		}
		g := rv64FunctionDependencyGraph()
		g.mu.Lock()
		for _, record := range g.records {
			if record.generation != 0 && record.state != dependencyReleased {
				for _, in := range []*Instance{outer, consumer, producer} {
					if record.target != nil && record.target.owner == in || record.consumer != nil && record.consumer.owner == in {
						t.Errorf("nested owner graph receipt remains: %+v", record)
					}
				}
			}
		}
		g.mu.Unlock()
	}()
	producer.Close()
	if closeConsumer {
		consumer.Close()
	}
	got, err := outer.Invoke("run")
	if err != nil || !slices.Equal(got, []uint64{1}) {
		t.Fatalf("nested foreign subtype=%v,%v", got, err)
	}
}

func TestRV64ForeignDeclaredFunctionSubtypeOrdinaryCalls(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t/artifact%t", vector, artifact), func(t *testing.T) {
				cfg := NewRuntimeConfig()
				compile := func(source []byte) *Compiled {
					t.Helper()
					c, e := cfg.Compile(source)
					if e != nil {
						t.Fatal(e)
					}
					t.Cleanup(func() { c.Close() })
					if artifact {
						blob, e := c.MarshalBinary()
						if e != nil {
							t.Fatal(e)
						}
						c, e = LoadTrustedArtifact(blob)
						if e != nil {
							t.Fatal(e)
						}
						t.Cleanup(func() { c.Close() })
					}
					return c
				}
				ps, cs := rv64ForeignFunctionSubtypeSources(vector)
				producer, e := Instantiate(compile(ps))
				if e != nil {
					t.Fatal(e)
				}
				defer producer.Close()
				table, e := producer.ExportedTable("table")
				if e != nil {
					t.Fatal(e)
				}
				defer table.Close()
				consumer, e := Instantiate(compile(cs), NewImports().Table("env", "table", table))
				if e != nil {
					t.Fatal(e)
				}
				defer consumer.Close()
				if e := producer.Close(); e != nil {
					t.Fatal(e)
				}
				for _, name := range []string{"indirect", "reference", "test", "cast", "tail-indirect", "tail-reference"} {
					for _, unrelated := range []bool{false, true} {
						export := name
						if unrelated {
							export += "-unrelated"
						}
						t.Run(export, func(t *testing.T) {
							var args []uint64
							want := []uint64{42}
							if name == "test" || name == "cast" {
								want[0] = 1
							}
							if vector && strings.HasPrefix(name, "tail-") {
								args = []uint64{0x1122334455667788, 0x8877665544332211}
								want = append(want, args...)
							}
							for round := 0; round < 3; round++ {
								got, e := consumer.Invoke(export, args...)
								if unrelated && name != "test" {
									if e == nil {
										t.Fatalf("unrelated target returned %v", got)
									}
									continue
								}
								if unrelated {
									want[0] = 0
								}
								if e != nil || !slices.Equal(got, want) {
									t.Fatalf("%s=%v,%v; want %v", export, got, e, want)
								}
							}
						})
					}
				}
			})
		}
	}
}
