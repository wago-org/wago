package wago

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Background counts are synthetic; each attach below still performs a real
// producer retain. Verify its own exact unit independently of those counts.
func TestMemoryImporterCounterBoundaryAndRootBalance(t *testing.T) {
	for _, addr64 := range []bool{false, true} {
		for _, count := range []uint32{0, 254, 255, 256, ^uint32(0) - 1, ^uint32(0)} {
			t.Run(fmt.Sprintf("memory64%t-count%d", addr64, count), func(t *testing.T) {
				producer := &Instance{}
				state := &memoryState{owner: producer}
				state.set(memoryStateShared, true)
				state.set(memoryStateAddr64, addr64)
				maximum := uint64(65535)
				if addr64 {
					maximum = 1<<48 - 1
				}
				state.setDeclaredLimits(maximum, true)
				state.setImporterCount(count)
				defer state.setImporterCount(0)
				memory := &Memory{jm: &coreruntime.JobMemory{}}
				memory.state.Store(state)
				if unsafe.Sizeof(*memory) != 2*unsafe.Sizeof(uintptr(0)) {
					t.Fatal("memory handle footprint changed")
				}
				err := memory.attachImporter()
				if count == ^uint32(0) {
					if err == nil || !strings.Contains(err.Error(), "overflows uint32") || state.importerCount() != count || producer.resourceRefs != 0 {
						t.Fatal("overflow acquired an importer or producer root", err)
					}
					return
				}
				if err != nil || state.importerCount() != count+1 || producer.resourceRefs != 1 {
					t.Fatal("exact successful counter/root acquisition", err)
				}
				if count == ^uint32(0)-1 {
					if err := memory.attachImporter(); err == nil || state.importerCount() != count+1 || producer.resourceRefs != 1 {
						t.Fatal("maximum/+1 admission changed the existing acquisition", err)
					}
				}
				owner, consumed := memory.detachImporterCounter()
				if !consumed || owner != producer || state.importerCount() != count || producer.resourceRefs != 1 {
					t.Fatal("counter claim performed a premature producer release")
				}
				owner.releaseResourceRoot()
				if producer.resourceRefs != 0 || state.declaredMaximum() != maximum {
					t.Fatal("root release or packed declared limits changed")
				}
				if err := memory.attachImporter(); err != nil {
					t.Fatal("ordinary detach fixture", err)
				}
				memory.detachImporter()
				if state.importerCount() != count || producer.resourceRefs != 0 {
					t.Fatal("ordinary detach no longer balances its counter and root")
				}
				state.setImporterCount(0)
				if owner, consumed := memory.detachImporterCounter(); owner != nil || consumed {
					t.Fatal("empty counter fabricated ownership")
				}
			})
		}
	}
}

func TestMemoryImporterCounterHostAndDenial(t *testing.T) {
	for _, denial := range []string{"none", "closed-memory", "closed-producer", "unexported", "exclusive"} {
		t.Run(denial, func(t *testing.T) {
			state := &memoryState{}
			state.set(memoryStateShared, true)
			memory := &Memory{jm: &coreruntime.JobMemory{}}
			memory.state.Store(state)
			var producer *Instance
			if denial == "closed-producer" || denial == "unexported" {
				producer = &Instance{}
				if denial == "closed-producer" {
					producer.closeInvocationEntry() // actual real-retain admission gate
				}
				state.owner = producer
			}
			if denial == "closed-memory" {
				state.set(memoryStateClosed, true)
			}
			if denial == "unexported" || denial == "exclusive" {
				state.set(memoryStateShared, false)
			}
			if denial == "exclusive" {
				state.setImporterCount(1)
			}
			before := state.importerCount()
			err := memory.attachImporter()
			if denial == "none" {
				if err != nil || state.importerCount() != 1 {
					t.Fatal("host-only count acquisition", err)
				}
				owner, consumed := memory.detachImporterCounter()
				if owner != nil || !consumed || state.importerCount() != 0 {
					t.Fatal("host-only counter obligation mistaken for empty")
				}
			} else if err == nil || state.importerCount() != before || producer != nil && producer.resourceRefs != 0 {
				t.Fatal("denied attach had acquisition side effects", err)
			}
		})
	}
}
