//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRV64PublicMutableGlobalDescriptorPendingReceipt(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = rt.CloseContext(ctx)
			})
			compiled := rv64TableStageCompile(t, "mutable-global-descriptor", rv64ReferenceGlobalExportModule(t, false, false, false, 1), artifact)
			module, err := rt.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			producer, err := rt.Instantiate(context.Background(), module)
			module.Close()
			compiled.Close()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = producer.Close() })
			value, err := producer.GlobalValue("reference")
			if err != nil || value.Type() != ValFuncRef || value.FuncRef().IsNull() {
				t.Fatal("producer token", value, err)
			}
			global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = global.Close() })
			baseline := dependencyTestRefs(producer)
			if err := global.SetValue(value); err != nil {
				t.Fatal(err)
			}
			global.owner.mu.Lock()
			state := global.owner.retained[producer]
			row := global.owner.descriptorDependencies.rows[state]
			global.owner.mu.Unlock()
			if state == nil || row == nil || row.phase != rv64GlobalDescriptorPending || dependencyTestRefs(producer) != baseline+1 {
				t.Fatal("public SetValue did not publish exactly one descriptor receipt")
			}
			receipt := row.receipt
			receipt.graph.mu.Lock()
			record, _ := receipt.graph.recordLocked(receipt.identity)
			pending := record != nil && record.state == dependencyPending && record.target.owner == producer && record.consumer == nil && record.kind == 0
			receipt.graph.mu.Unlock()
			if !pending {
				t.Fatal("public mutable container receipt was not Pending/external")
			}
			if err := global.SetValue(value); err != nil {
				t.Fatal(err)
			}
			global.owner.mu.Lock()
			duplicateState := global.owner.retained[producer]
			duplicateRow := global.owner.descriptorDependencies.rows[duplicateState]
			global.owner.mu.Unlock()
			if duplicateState != state || duplicateRow != row || duplicateRow.receipt.identity != receipt.identity || dependencyTestRefs(producer) != baseline+1 {
				t.Fatal("duplicate SetValue changed the physical acquisition identity")
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() {
				t.Fatal("logical producer Close retired a descriptor-backed mapping")
			}
			if err := global.SetValue(ValueFuncRef(NullFuncRef())); err != nil {
				t.Fatal(err)
			}
			global.owner.mu.Lock()
			retained, sidecar := len(global.owner.retained), len(global.owner.descriptorDependencies.rows)
			global.owner.mu.Unlock()
			if retained != 0 || sidecar != 0 {
				t.Fatal("null overwrite retained a descriptor map row", retained, sidecar)
			}
			receipt.graph.mu.Lock()
			record, _ = receipt.graph.recordLocked(receipt.identity)
			released := record != nil && record.state == dependencyReleased
			receipt.graph.mu.Unlock()
			if !released {
				t.Fatal("null overwrite did not retire the exact receipt generation")
			}
			if err := global.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rt.Close(); err != nil {
				t.Fatal(err)
			}
			waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := rt.WaitClosed(waitCtx); err != nil {
				t.Fatal(err)
			}
			if producer.hasPhysicalResources() {
				t.Fatal("final store close retained producer mappings")
			}
		})
	}
}

func TestRV64PublicMutableGlobalDeniedOverwriteFirstCloseRetries(t *testing.T) {
	rt := NewRuntime()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = rt.CloseContext(ctx)
	})
	compiled := rv64TableStageCompile(t, "mutable-global-denied-overwrite", rv64ReferenceGlobalExportModule(t, false, false, false, 1), false)
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := rt.Instantiate(context.Background(), module)
	module.Close()
	compiled.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = producer.Close() })
	value, err := producer.GlobalValue("reference")
	if err != nil {
		t.Fatal(err)
	}
	global, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = cleanupTestPanic(func() { _ = global.Close() })
	})
	if err := global.SetValue(value); err != nil {
		t.Fatal(err)
	}
	global.owner.mu.Lock()
	state := global.owner.retained[producer]
	row := global.owner.descriptorDependencies.rows[state]
	global.owner.mu.Unlock()
	receipt := row.receipt
	receipt.graph.mu.Lock()
	record, _ := receipt.graph.recordLocked(receipt.identity)
	record.state = dependencyReserved
	receipt.graph.mu.Unlock()
	faultOwned := true
	defer func() {
		if !faultOwned {
			return
		}
		receipt.graph.mu.Lock()
		if current, _ := receipt.graph.recordLocked(receipt.identity); current != nil && current.state == dependencyReserved {
			current.state = dependencyPending
		}
		receipt.graph.mu.Unlock()
	}()
	_, panicked := cleanupTestPanic(func() {
		_ = global.SetValue(ValueFuncRef(NullFuncRef()))
	})
	global.owner.mu.Lock()
	retained, sidecar := len(global.owner.retained), len(global.owner.descriptorDependencies.rows)
	global.owner.mu.Unlock()
	if !panicked || retained != 0 || sidecar != 1 || row.phase != rv64GlobalDescriptorPending {
		t.Fatal("denied public overwrite lost its retryable retired row", panicked, retained, sidecar, row.phase)
	}
	receipt.graph.mu.Lock()
	record, _ = receipt.graph.recordLocked(receipt.identity)
	record.state = dependencyPending
	receipt.graph.mu.Unlock()
	faultOwned = false
	if err := global.Close(); err != nil {
		t.Fatal(err)
	}
	global.owner.mu.Lock()
	sidecar = len(global.owner.descriptorDependencies.rows)
	global.owner.mu.Unlock()
	if sidecar != 0 || row.phase != rv64GlobalDescriptorReleased {
		t.Fatal("first terminal Close did not drain repaired retired row", sidecar, row.phase)
	}
	receipt.graph.mu.Lock()
	record, _ = receipt.graph.recordLocked(receipt.identity)
	released := record != nil && record.state == dependencyReleased
	receipt.graph.mu.Unlock()
	if !released {
		t.Fatal("first terminal Close did not retire exact receipt")
	}
}
