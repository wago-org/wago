//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func rv64TableTargetRefresh(targets *rv64NativeTableTargets, root *Instance) error {
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	return targets.refresh(root)
}

func rv64TableTargetFixture(t *testing.T, vector, artifact, shared bool) (*Instance, *Instance, *Table, *Compiled) {
	t.Helper()
	var imports *Imports
	if shared {
		_, memory := importedMemoryProvider(t)
		t.Cleanup(func() { _ = memory.Close() })
		imports = NewImports().Memory("env", "memory", memory)
	}
	c := rv64PublicStartCompile(t, rv64PublicTableExportModule(vector, shared), artifact)
	root, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	producer, err := Instantiate(c, imports)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close() })
	table, err := root.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	descriptor, ok := producer.localFuncrefDescriptor(1)
	if !ok {
		t.Fatal("retained local canonical descriptor")
	}
	offset := descriptor - uint64(offHeapSlicePtr(producer.funcRefDescs))
	nativeExecutionMu.Lock()
	copy(table.desc[8:8+coreruntime.TableEntryBytes], producer.funcRefDescs[int(offset):int(offset)+coreruntime.TableEntryBytes])
	nativeExecutionMu.Unlock()
	return root, producer, table, c
}

func TestRV64PrivateTableTargetsRetainReplacedRetiredProducer(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-shared%t", vector, artifact, shared), func(t *testing.T) {
					root, producer, table, c := rv64TableTargetFixture(t, vector, artifact, shared)
					if !table.retainProducerInstance(producer) {
						t.Fatal("table physical producer root")
					}
					var targets rv64NativeTableTargets
					defer targets.release()
					if err := rv64TableTargetRefresh(&targets, root); err != nil {
						t.Fatal(err)
					}
					if targets.count != 2 || targets.owner(producer.nativeContext) != producer || targets.owner(root.nativeContext) != root || targets.owner(1) != nil {
						t.Fatal("exact context owner identities")
					}
					if shared && (root.jm != producer.jm || root.nativeContext == producer.nativeContext) {
						t.Fatal("shared home is not owner identity")
					}
					if n := testing.AllocsPerRun(100, func() {
						if err := rv64TableTargetRefresh(&targets, root); err != nil {
							panic(err)
						}
					}); n != 0 {
						t.Fatal("warm target authentication allocations", n)
					}
					replacement, err := Instantiate(c, root.Imports())
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = replacement.Close() })
					identity := producer.nativeContext
					_ = c.Close()
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if _, err := root.Invoke("clear", 0); err != nil {
						t.Fatal(err)
					}
					table.mu.Lock()
					remaining := len(table.retained)
					table.mu.Unlock()
					runtime.GC()
					if remaining != 0 || producer.jm == nil || producer.funcRefDescs == nil || targets.owner(identity) != producer {
						t.Fatal("row replacement revoked suspended target storage")
					}
					if err := rv64TableTargetRefresh(&targets, root); err != nil {
						t.Fatal("refresh preserves prior activation owners", err)
					}
					// A resume must merge newly installed targets while keeping the
					// first suspended producer, whose table root already disappeared.
					nativeExecutionMu.Lock()
					copy(table.desc[8:40], replacement.funcRefDescs[80:112])
					nativeExecutionMu.Unlock()
					if !table.retainProducerInstance(replacement) {
						t.Fatal("replacement physical table root")
					}
					if err := rv64TableTargetRefresh(&targets, root); err != nil {
						t.Fatal("merge replacement", err)
					}
					if targets.count != 3 || targets.owner(replacement.nativeContext) != replacement || targets.owner(identity) != producer {
						t.Fatal("refresh lost an old or new activation owner")
					}
					if err := replacement.Close(); err != nil {
						t.Fatal(err)
					}
					if _, err := root.Invoke("clear", 0); err != nil {
						t.Fatal(err)
					}
					if err := root.Close(); err != nil {
						t.Fatal(err)
					}
					if root.jm == nil {
						t.Fatal("activation root released early")
					}
					targets.release()
					runtime.GC()
					if producer.jm != nil || replacement.jm != nil || root.jm != nil || targets.count != 0 {
						t.Fatal("final table target ownership cleanup")
					}
				})
			}
		}
	}
}

func TestRV64PrivateTableTargetsAuthenticateBeforeUnknownPointerReads(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	var targets rv64NativeTableTargets
	defer targets.release()
	// A valid foreign row without its physical table attachment is insufficient.
	root.lifeMu.Lock()
	beforeRoots := root.resourceRefs
	root.lifeMu.Unlock()
	if err := rv64TableTargetRefresh(&targets, root); err == nil {
		t.Fatal("unretained foreign row admitted")
	}
	targets.release()
	root.lifeMu.Lock()
	afterRoots := root.resourceRefs
	root.lifeMu.Unlock()
	if afterRoots != beforeRoots || targets.count != 0 {
		t.Fatal("failed partial refresh leaked physical roots", beforeRoots, afterRoots)
	}
	if !table.retainProducerInstance(producer) {
		t.Fatal("table root")
	}
	if err := rv64TableTargetRefresh(&targets, root); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(table.desc)
	canonical := producer.funcRefDescs[2*coreruntime.FuncRefDescBytes : 3*coreruntime.FuncRefDescBytes]
	savedCanonical := bytes.Clone(canonical)
	for _, fault := range []string{"unknown-slot", "interior-slot", "row-entry", "row-home", "canonical-key", "canonical-entry", "canonical-home", "canonical-self", "canonical-context", "null-identity", "size", "capacity", "extent"} {
		t.Run(fault, func(t *testing.T) {
			copy(table.desc, before)
			copy(canonical, savedCanonical)
			switch fault {
			case "unknown-slot":
				binary.LittleEndian.PutUint64(table.desc[8+coreruntime.TableEntryRefSlotOffset:], 0xfffffffffffffff8)
			case "interior-slot":
				binary.LittleEndian.PutUint64(table.desc[8+coreruntime.TableEntryRefSlotOffset:], binary.LittleEndian.Uint64(canonical[24:])+8)
			case "row-entry":
				binary.LittleEndian.PutUint64(table.desc[8:], uint64(root.base))
			case "row-home":
				binary.LittleEndian.PutUint64(table.desc[24:], binary.LittleEndian.Uint64(canonical[16:])^8)
			case "null-identity":
				binary.LittleEndian.PutUint64(table.desc[8:], 0)
			case "size":
				binary.LittleEndian.PutUint32(table.desc, 5)
			case "capacity":
				binary.LittleEndian.PutUint32(table.desc[4:], 1025)
			case "extent":
				binary.LittleEndian.PutUint32(table.desc[4:], 1024)
			default:
				offset := map[string]int{"canonical-key": 8, "canonical-entry": 0, "canonical-home": 16, "canonical-self": 24, "canonical-context": 32}[fault]
				binary.LittleEndian.PutUint64(canonical[offset:], binary.LittleEndian.Uint64(canonical[offset:])^8)
				copy(table.desc[8:40], canonical[:32])
			}
			if err := rv64TableTargetRefresh(&targets, root); err == nil {
				t.Fatal("forged table identity accepted", fault)
			}
		})
	}
	copy(table.desc, before)
	copy(canonical, savedCanonical)
	if err := rv64TableTargetRefresh(&targets, root); err != nil {
		t.Fatal("reuse after rejected identity", err)
	}
}

func TestRV64PrivateTableTargetOwnerLimit(t *testing.T) {
	var targets rv64NativeTableTargets
	defer targets.release()
	for i := 0; i < len(targets.owners); i++ {
		if err := targets.add(&Instance{}); err != nil {
			t.Fatal("bounded owner slot", i, err)
		}
	}
	extra := new(Instance)
	if err := targets.add(extra); err == nil || extra.resourceRefs != 0 {
		t.Fatal("overflow acquired an unauthenticated physical root")
	}
	if err := targets.add(targets.owners[0]); err != nil || targets.count != len(targets.owners) {
		t.Fatal("duplicate consumes owner budget", err)
	}
	targets.release()
	if targets.count != 0 || targets.owners[0] != nil {
		t.Fatal("bounded roots not cleared")
	}
}

func TestRV64PrivateTableTargetAttachmentEdgeLimit(t *testing.T) {
	root, producer, table, _ := rv64TableTargetFixture(t, false, false, false)
	if !table.retainProducerInstance(producer) {
		t.Fatal("table root")
	}
	for _, initial := range []int{4095, 4096} {
		var targets rv64NativeTableTargets
		edges := initial
		nativeExecutionMu.Lock()
		err := targets.tableRoots(table, &edges)
		nativeExecutionMu.Unlock()
		if (err != nil) != (initial == 4096) || edges != initial+1 {
			targets.release()
			t.Fatal("attachment edge bound", initial, edges, err)
		}
		if err == nil && targets.owner(root.nativeContext) != root {
			targets.release()
			t.Fatal("table owner omitted")
		}
		targets.release()
	}
}
