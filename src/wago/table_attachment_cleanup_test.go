//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"syscall"
	"testing"
	"unsafe"
)

func tableCleanupMappingStatus(address uintptr) syscall.Errno {
	var resident [1]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE, address, uintptr(syscall.Getpagesize()), uintptr(unsafe.Pointer(&resident[0])))
	return errno
}

// Three independently mapped Instances publish distinct real native descriptor
// rows. Production descriptor reachability acquires their table roots without
// issuing public tokens or fabricating importer/store/native-lease units.
func TestTableAttachmentCleanupMappedOwnersAndHostStorage(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, action := range []string{"close", "prune", "retained"} {
			for _, reentrantClose := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-%s-reentry%t", artifact, action, reentrantClose), func(t *testing.T) {
					table, err := NewTable(3, 3)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					if table.owner.store != nil {
						t.Fatal("NewTable must have no fabricated store-object unit")
					}
					address := uintptr(unsafe.Pointer(&table.desc[0]))
					if e := tableCleanupMappingStatus(address); e != 0 {
						t.Fatal("host table arena is not mapped", e)
					}
					code := tableCounterCompile(t, false, artifact)
					var owners [3]*Instance
					for i := range owners {
						owner, err := Instantiate(code)
						if err != nil {
							t.Fatal(err)
						}
						defer owner.Close()
						owners[i] = owner
						if got, err := owner.Invoke("run"); err != nil || len(got) != 1 || got[0] != 17 {
							t.Fatal("actual descriptor producer failed execution", got, err)
						}
						descriptor, ok := owner.localFuncrefDescriptor(0)
						if !ok || !owner.reachesFuncrefDescriptor(descriptor) || owner.jm == nil || owner.ar == nil || owner.eng == nil || owner.nativeContext == 0 {
							t.Fatal("fixture lacks actual published native descriptor ownership")
						}
						offset := descriptor - uint64(offHeapSlicePtr(owner.funcRefDescs))
						if offset > uint64(len(owner.funcRefDescs)-coreruntime.TableEntryBytes) {
							t.Fatal("actual descriptor row is out of bounds")
						}
						nativeExecutionMu.Lock()
						table.mu.Lock()
						copy(table.desc[8+i*coreruntime.TableEntryBytes:8+(i+1)*coreruntime.TableEntryBytes], owner.funcRefDescs[int(offset):int(offset)+coreruntime.TableEntryBytes])
						table.mu.Unlock()
						nativeExecutionMu.Unlock()
						if !table.retainProducerInstance(owner) {
							t.Fatal("actual table descriptor did not acquire its real owner root")
						}
					}
					if len(table.retained) != 3 || table.owner.importers != 0 || table.owner.nativeLeases != 0 {
						t.Fatal("fixture did not acquire exactly its three descriptor roots")
					}
					var visited [3]int
					var first any
					for i, owner := range owners {
						if owner.resourceRefs != 1 {
							t.Fatal("actual descriptor owner acquired an unrelated root", owner.resourceRefs)
						}
						index, marker := i, new(int)
						owner.referenceLifetime().afterPhysicalRelease(func() {
							visited[index]++
							if !table.owner.mu.TryLock() {
								panic("table cleanup callback under tableOwner.mu")
							}
							if !table.mu.TryLock() {
								table.owner.mu.Unlock()
								panic("table cleanup callback under Table.mu")
							}
							claimed := len(table.retained) == 0
							if action == "close" || table.owner.closed {
								claimed = claimed && table.closed && table.desc == nil && table.owner.arena == nil
							}
							if action == "prune" && !table.closed {
								for j := 0; j < 3; j++ {
									claimed = claimed && binary.LittleEndian.Uint64(table.desc[8+j*coreruntime.TableEntryBytes+coreruntime.TableEntryRefSlotOffset:]) == 0
								}
							}
							if action == "retained" {
								claimed = claimed && table.closed
							}
							table.mu.Unlock()
							table.owner.mu.Unlock()
							if !claimed {
								panic("claimed table roots remain published during physical callback")
							}
							if reentrantClose {
								if err := table.Close(); err != nil {
									panic(err)
								}
							}
							if first == nil {
								first = marker
							} // Actual map traversal order is unspecified.
							panic(marker)
						})
					}
					for _, owner := range owners {
						if err := owner.Close(); err != nil || owner.resourcesClosed || owner.resourceRefs != 1 {
							t.Fatal("logical Close lost a table-owned native producer", err)
						}
					}
					if action == "prune" {
						nativeExecutionMu.Lock()
						table.mu.Lock()
						clear(table.desc[8:])
						table.mu.Unlock()
						nativeExecutionMu.Unlock()
					}
					value, panicked := cleanupTestPanic(func() {
						switch action {
						case "close":
							err = table.Close()
						case "prune":
							table.pruneRetainedInstances()
						case "retained":
							table.releaseRetainedInstances()
						}
					})
					if !panicked || value != first || visited != [3]int{1, 1, 1} {
						t.Fatal("table cleanup skipped a real owner or replaced the first actual panic", value, panicked, first, visited, err)
					}
					for _, owner := range owners {
						if !owner.resourcesClosed || owner.finalizing || owner.resourceRefs != 0 || owner.jm != nil || owner.ar != nil || owner.eng != nil || owner.nativeContext != 0 {
							t.Fatal("table cleanup stranded actual producer storage")
						}
					}
					if action != "close" && !reentrantClose {
						if table.owner.closed || table.owner.arena == nil || tableCleanupMappingStatus(address) != 0 {
							t.Fatal("root-only drain consumed independent host arena ownership")
						}
						if err := table.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if !table.owner.closed || table.owner.arena != nil || table.desc != nil || len(table.retained) != 0 || tableCleanupMappingStatus(address) != syscall.ENOMEM {
						t.Fatal("table cleanup failed to unmap the actual host arena")
					}
					if err := table.Close(); err != nil || visited != [3]int{1, 1, 1} {
						t.Fatal("repeated Close repeated a consumed native callback", err, visited)
					}
				})
			}
		}
	}
}
