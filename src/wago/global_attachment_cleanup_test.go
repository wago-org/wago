//go:build linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"syscall"
	"testing"
	"unsafe"
)

func globalCleanupCompile(t *testing.T, module []byte, artifact bool) *Compiled {
	t.Helper()
	c, err := Compile(artifactTestConfig(artifact), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	return c

}

func globalCleanupMappingStatus(address uintptr) syscall.Errno {
	var resident [1]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE, address, uintptr(syscall.Getpagesize()), uintptr(unsafe.Pointer(&resident[0])))
	return errno
}

// All retained owners really reach one published descriptor: its mapped
// producer and two mapped writers using the canonical imported function refSlot.
// No issued token adds another lifetime root. This common cleanup fixture does
// not expand RV64 admission for mutable reference globals or reference returns.
func TestGlobalAttachmentCleanupMappedOwnersAndHostStorage(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, action := range []string{"close", "overwrite", "overwrite-close"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, action), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				producer, err := Instantiate(globalCleanupCompile(t, funcrefImportedProducerModule(), artifact), InstantiateOptions{runtime: rt})
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("target")
				if err != nil {
					t.Fatal(err)
				}
				writerCode := globalCleanupCompile(t, funcrefImportedRefFuncModule(), artifact)
				writer, err := Instantiate(writerCode, InstantiateOptions{runtime: rt, Imports: NewImports().Function("env", "target", export)})
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
				otherWriter, err := Instantiate(writerCode, InstantiateOptions{runtime: rt, Imports: NewImports().Function("env", "target", export)})
				if err != nil {
					t.Fatal(err)
				}
				defer otherWriter.Close()
				instances := []*Instance{producer, writer, otherWriter}
				descriptor, ok := producer.localFuncrefDescriptor(0)
				if !ok || !producer.reachesFuncrefDescriptor(descriptor) || !writer.reachesFuncrefDescriptor(descriptor) || !otherWriter.reachesFuncrefDescriptor(descriptor) {
					t.Fatal("fixture lost its actual canonical native descriptor")
				}
				g, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
				if err != nil {
					t.Fatal(err)
				}
				defer g.Close()
				address := uintptr(unsafe.Pointer(&g.cell[0]))
				if errno := globalCleanupMappingStatus(address); errno != 0 {
					t.Fatal("host global arena is not mapped", errno)
				}
				// Seed genuine native bits without issuing a token; retain through
				// the production descriptor-reachability and real lifetime paths.
				g.owner.mu.Lock()
				writeGlobalObject(g, ValFuncRef, descriptor)
				g.owner.mu.Unlock()
				if !g.retainProducerInstance(producer) || !g.retainProducerInstance(writer) || !g.retainProducerInstance(otherWriter) || len(g.owner.retained) != 3 || producer.resourceRefs != 3 || writer.resourceRefs != 1 || otherWriter.resourceRefs != 1 {
					t.Fatal("fixture did not acquire its exact three actual writer roots")
				}
				var visited [3]int
				var first any
				for i, in := range instances {
					if in.jm == nil || in.ar == nil || in.eng == nil {
						t.Fatal("fixture lacks actual native mappings")
					}
					index, marker := i, new(int)
					in.referenceLifetime().afterPhysicalRelease(func() {
						visited[index]++
						if !g.owner.mu.TryLock() {
							panic("global cleanup callback under globalOwner.mu")
						}
						claimed := len(g.owner.retained) == 0
						if action == "close" {
							claimed = claimed && g.owner.closed && g.owner.arena == nil && g.cell == nil
						} else if !g.owner.closed {
							claimed = claimed && readGlobalObject(g, ValFuncRef) == 0
						}
						g.owner.mu.Unlock()
						if !claimed {
							panic("old global ownership still published during callback")
						}
						if action != "overwrite" {
							if err := g.Close(); err != nil {
								panic(err)
							}
						}
						// Close has no outer native lease. SetValue's callback
						// already holds one and must not recursively acquire it.
						if action == "close" && g.SetValue(ValueFuncRef(NullFuncRef())) == nil {
							panic("closed global accepted a reentrant update")
						}
						if first == nil {
							first = marker // Actual map traversal order is unspecified.
						}
						panic(marker)
					})
				}
				for _, in := range instances {
					if err := in.Close(); err != nil || in.resourcesClosed {
						t.Fatal("logical Close lost a retained native owner", err)
					}
				}
				if err := rt.Close(); err != nil {
					t.Fatal(err)
				}
				store := g.owner.store
				store.mu.Lock()
				objects := store.liveObjects
				store.mu.Unlock()
				if objects != 1 {
					t.Fatal("fixture host store-object ownership", objects)
				}
				value, panicked := cleanupTestPanic(func() {
					if action == "close" {
						err = g.Close()
					} else {
						err = g.SetValue(ValueFuncRef(NullFuncRef()))
					}
				})
				if !panicked || value != first || visited != [3]int{1, 1, 1} {
					t.Fatal("global cleanup replaced its first panic or skipped a real owner", value, panicked, first, visited, err)
				}
				for _, in := range instances {
					if !in.resourcesClosed || in.finalizing || in.resourceRefs != 0 || in.jm != nil || in.ar != nil || in.eng != nil {
						t.Fatal("global cleanup stranded an actual native ownership unit")
					}
				}
				store.mu.Lock()
				objects = store.liveObjects
				store.mu.Unlock()
				if action == "overwrite" {
					if g.owner.closed || g.owner.arena == nil || readGlobalObject(g, ValFuncRef) != 0 || objects != 1 || globalCleanupMappingStatus(address) != 0 {
						t.Fatal("null overwrite consumed unrelated host arena/store ownership")
					}
					if err := g.Close(); err != nil {
						t.Fatal(err)
					}
				} else if objects != 0 {
					t.Fatal("panicking callback skipped actual host store-object release", objects)
				}
				store.mu.Lock()
				objects = store.liveObjects
				store.mu.Unlock()
				if !g.owner.closed || g.owner.arena != nil || g.cell != nil || len(g.owner.retained) != 0 || objects != 0 || globalCleanupMappingStatus(address) != syscall.ENOMEM {
					t.Fatal("global cleanup failed to unmap its actual host arena or close its store")
				}
				if err := g.Close(); err != nil || visited != [3]int{1, 1, 1} {
					t.Fatal("repeated global Close repeated a consumed native callback", err, visited)
				}
			})
		}
	}
}
