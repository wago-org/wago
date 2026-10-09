//go:build linux && (amd64 || arm64 || (riscv64 && wago_rv64_experimental)) && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"testing"
)

func TestGlobalImporterCounterMappedRelease(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, callbackPanic := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-panic%t", artifact, callbackPanic), func(t *testing.T) {
				c := importedGlobalCompile(t, false, 0, true)
				if artifact {
					blob, err := c.MarshalBinary()
					if err != nil {
						t.Fatal(err)
					}
					c, err = LoadTrustedArtifact(blob)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = c.Close() })
				}
				producer, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				g, err := producer.ExportedGlobalObject("g0")
				if err != nil {
					t.Fatal(err)
				}
				if producer.jm == nil || producer.ar == nil || producer.eng == nil || g.instanceOwner() != producer {
					t.Fatal("fixture lacks actual native ownership")
				}
				callbacks, unlocked := 0, false
				first := new(int)
				producer.referenceLifetime().afterPhysicalRelease(func() {
					callbacks++
					unlocked = g.owner.mu.TryLock()
					if unlocked {
						g.owner.mu.Unlock()
					}
					if callbackPanic {
						panic(first)
					}
				})
				if err := g.attachNumericImporter(); err != nil {
					t.Fatal(err)
				}
				if err := producer.Close(); err != nil || producer.resourcesClosed || producer.resourceRefs != 1 || g.owner.importers != 1 {
					t.Fatal("logical Close lost the actual counted root", err)
				}
				owner, consumed := g.detachImporterCounter()
				if !consumed || owner != producer || g.owner.importers != 0 || producer.resourceRefs != 1 || producer.resourcesClosed || len(g.cell) == 0 || callbacks != 0 {
					t.Fatal("counter consumption prematurely tore down native resources")
				}
				value, panicked := cleanupTestPanic(owner.releaseResourceRoot)
				if panicked != callbackPanic || callbackPanic && value != first || callbacks != 1 || !unlocked {
					t.Fatal("root release changed callback identity or held the global lock", value, panicked, callbacks, unlocked)
				}
				if !producer.resourcesClosed || producer.finalizing || producer.resourceRefs != 0 || producer.jm != nil || producer.ar != nil || producer.eng != nil || len(g.cell) != 0 {
					t.Fatal("counter/root release stranded actual mappings")
				}
				g.detachReferenceImporter()
				if owner, consumed := g.detachImporterCounter(); owner != nil || consumed || callbacks != 1 {
					t.Fatal("repeated detach fabricated a counter/root release")
				}
			})
		}
	}
}
