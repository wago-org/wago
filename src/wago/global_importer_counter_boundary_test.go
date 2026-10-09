package wago

import (
	"fmt"
	"strings"
	"testing"
)

func counterGlobal(typ ValType, producer *Instance, count int, store *referenceStore) *Global {
	return &Global{Type: typ, Mutable: true, cell: make([]byte, globalCellSize(typ)), owner: &globalOwner{
		typ: typ, mutable: true, instance: producer, importers: count, store: store,
	}}
}

func attachCounterGlobal(g *Global, store *referenceStore) error {
	if isReferenceValType(g.Type) {
		return g.attachReferenceImporter(store)
	}
	return g.attachNumericImporter()
}

// Background counts and cells are synthetic. Each successful attach still
// acquires a real lifetime root, measured separately from the background count.
func TestGlobalImporterCounterBoundaryAndRootBalance(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	for _, typ := range []ValType{ValI32, ValV128, ValFuncRef, ValExternRef} {
		for _, instanceOwned := range []bool{false, true} {
			for _, count := range []int{-1, 0, 254, 255, 256, maximum - 1, maximum} {
				t.Run(fmt.Sprintf("%s-instance%t-count%d", typ, instanceOwned, count), func(t *testing.T) {
					var producer *Instance
					if instanceOwned {
						producer = &Instance{}
					}
					store := newReferenceStore(true)
					g := counterGlobal(typ, producer, count, store)
					err := attachCounterGlobal(g, store)
					if count < 0 || count == maximum {
						message := "overflows int"
						if count < 0 {
							message = "negative"
						}
						if err == nil || !strings.Contains(err.Error(), message) || g.owner.importers != count || producer != nil && producer.resourceRefs != 0 {
							t.Fatal("denied counter acquired an importer or producer root", err)
						}
						return
					}
					if err != nil || g.owner.importers != count+1 || producer != nil && producer.resourceRefs != 1 {
						t.Fatal("successful attach did not acquire exactly one count/root", err)
					}
					if count == maximum-1 {
						if err := attachCounterGlobal(g, store); err == nil || g.owner.importers != maximum || producer != nil && producer.resourceRefs != 1 {
							t.Fatal("maximum/+1 changed the existing acquisition", err)
						}
					}
					owner, consumed := g.detachImporterCounter()
					if !consumed || owner != producer || g.owner.importers != count || producer != nil && producer.resourceRefs != 1 {
						t.Fatal("counter claim released a producer or lost a host count")
					}
					if owner != nil {
						owner.releaseResourceRoot()
					}
					if err := attachCounterGlobal(g, store); err != nil {
						t.Fatal(err)
					}
					g.detachReferenceImporter()
					if g.owner.importers != count || producer != nil && producer.resourceRefs != 0 {
						t.Fatal("ordinary detach no longer balances its exact count/root")
					}
					g.owner.importers = 0 // discard only the synthetic background count
					if owner, consumed := g.detachImporterCounter(); owner != nil || consumed {
						t.Fatal("empty count fabricated an obligation")
					}
				})
			}
		}
	}
}

func TestGlobalImporterCounterValidationAndAliases(t *testing.T) {
	for _, typ := range []ValType{ValI32, ValV128, ValFuncRef, ValExternRef} {
		for _, denial := range []string{"closed-global", "closed-producer", "metadata", "short-cell", "store"} {
			if denial == "store" && !isReferenceValType(typ) {
				continue
			}
			t.Run(fmt.Sprintf("%s-%s", typ, denial), func(t *testing.T) {
				producer := &Instance{}
				store := newReferenceStore(true)
				g := counterGlobal(typ, producer, 0, store)
				switch denial {
				case "closed-global":
					g.owner.closed = true
				case "closed-producer":
					producer.closeInvocationEntry()
				case "metadata":
					g.Mutable = false
				case "short-cell":
					g.cell = nil
				case "store":
					store = newReferenceStore(true)
				}
				if err := attachCounterGlobal(g, store); err == nil || g.owner.importers != 0 || producer.resourceRefs != 0 {
					t.Fatal("invalid attach had acquisition side effects", err)
				}
			})
		}
		t.Run(fmt.Sprintf("%s-aliases", typ), func(t *testing.T) {
			producer := &Instance{}
			store := newReferenceStore(true)
			g := counterGlobal(typ, producer, 0, store)
			other := counterGlobal(typ, producer, 0, store)
			var batch globalImportAttachments
			failed := false
			allocs := testing.AllocsPerRun(100, func() {
				if batch.attach(g, store, nil) != nil || batch.attach(g, store, nil) != nil || batch.attach(other, store, nil) != nil || g.owner.importers != 1 || other.owner.importers != 1 || producer.resourceRefs != 2 {
					failed = true
				}
				batch.detachAll()
				batch.detachAll()
			})
			if failed || allocs != 0 || g.owner.importers != 0 || other.owner.importers != 0 || producer.resourceRefs != 0 {
				t.Fatal("exact alias/independent-handle acquisition changed", failed, allocs)
			}
		})
	}
	for _, g := range []*Global{nil, {}} {
		if owner, consumed := g.detachImporterCounter(); owner != nil || consumed {
			t.Fatal("invalid handle fabricated an obligation")
		}
	}
}
