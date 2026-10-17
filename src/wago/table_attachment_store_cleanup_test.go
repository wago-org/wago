//go:build (linux && (amd64 || arm64) && !tinygo && !wago_target_tinygo) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package wago

import (
	"context"
	"fmt"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// A real Runtime.NewExternRefTable owns one store-object unit. Three issued
// funcref tokens retain actual mapped producers until that table's notification
// retires the token batch; no descriptor roots are fabricated on an extern table.
func TestTableAttachmentCleanupActualStoreNotification(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			table, err := rt.NewExternRefTable(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			address := uintptr(unsafe.Pointer(&table.desc[0]))
			store := table.owner.store
			if store == nil || store != rt.refStore || len(table.retained) != 0 || tableCleanupMappingStatus(address) != 0 {
				t.Fatal("actual store-owned table fixture")
			}
			code := globalCleanupCompile(t, funcrefImportedProducerModule(), artifact)
			mod, err := rt.AdoptModule(code)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			var owners [3]*Instance
			var visited [3]int
			var first any
			for i := range owners {
				owner, err := rt.Instantiate(context.Background(), mod)
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				owners[i] = owner
				got, err := owner.InvokeValues(context.Background(), "get")
				if err != nil || len(got) != 1 || got[0].Type() != ValFuncRef || got[0].FuncRef().IsNull() {
					t.Fatal("actual token issuance failed", got, err)
				}
				if owner.refStore != store || owner.resourceRefs != 1 || owner.jm == nil || owner.ar == nil || owner.eng == nil {
					t.Fatal("actual issued token did not retain exactly one mapped owner")
				}
				index, marker := i, new(int)
				owner.referenceLifetime().afterPhysicalRelease(func() {
					visited[index]++
					if !table.owner.mu.TryLock() {
						panic("store callback under tableOwner.mu")
					}
					claimed := table.owner.closed && table.owner.arena == nil
					table.owner.mu.Unlock()
					if !table.mu.TryLock() {
						panic("store callback under Table.mu")
					}
					claimed = claimed && table.closed && table.desc == nil
					table.mu.Unlock()
					if !store.mu.TryLock() {
						panic("store callback under referenceStore.mu")
					}
					claimed = claimed && store.liveObjects == 0 && len(store.byToken) == 0
					store.mu.Unlock()
					if !claimed {
						panic("store/table obligations remain published during token callback")
					}
					if err := table.Close(); err != nil {
						panic(err)
					}
					if err := rt.Close(); err != nil {
						panic(err)
					}
					if first == nil {
						first = marker
					}
					panic(marker)
				})
			}
			for _, owner := range owners {
				if err := owner.Close(); err != nil || owner.resourcesClosed || owner.resourceRefs != 1 {
					t.Fatal("logical Close overtook issued token ownership", err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := rt.CloseContext(ctx); err != nil {
				t.Fatal(err)
			}
			store.mu.Lock()
			objects, tokens := store.liveObjects, len(store.byToken)
			store.mu.Unlock()
			if objects != 1 || tokens != 3 {
				t.Fatal("actual table/store/token units lost before notification", objects, tokens)
			}
			value, panicked := cleanupTestPanic(func() { err = table.Close() })
			if !panicked || value != first || visited != [3]int{1, 1, 1} {
				t.Fatal("actual store notification skipped a native owner or replaced its first panic", value, panicked, first, visited, err)
			}
			for _, owner := range owners {
				if !owner.resourcesClosed || owner.finalizing || owner.resourceRefs != 0 || owner.jm != nil || owner.ar != nil || owner.eng != nil || owner.nativeContext != 0 {
					t.Fatal("actual store notification stranded native storage")
				}
			}
			store.mu.Lock()
			objects, tokens = store.liveObjects, len(store.byToken)
			store.mu.Unlock()
			if objects != 0 || tokens != 0 || !table.owner.closed || table.owner.arena != nil || table.desc != nil || tableCleanupMappingStatus(address) != syscall.ENOMEM {
				t.Fatal("actual store table cleanup stranded its arena or token bookkeeping", objects, tokens)
			}
			if err := table.Close(); err != nil || visited != [3]int{1, 1, 1} {
				t.Fatal("duplicate table notification repeated native callbacks", err, visited)
			}
		})
	}
}
