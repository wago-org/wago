//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// A source owner can first become known while another destination is being
// finalized. Every destination holding its descriptor must retain that owner.
func TestImportedFuncrefRetentionDiscoversRootsBeforeTransfer(t *testing.T) {
	for _, kind := range []string{"tables", "globals", "mixed"} {
		orders := []string{"forward", "reverse"}
		if kind == "mixed" {
			orders = []string{"foreign-table", "foreign-global"}
		}
		for _, order := range orders {
			t.Run(kind+"/"+order, func(t *testing.T) {
				foreignRuntime, rt := NewRuntime(), NewRuntime()
				defer foreignRuntime.Close()
				defer rt.Close()
				foreignModule := mustCompileWat(foreignRuntime, t, `(module
				(func $target (export "target") (result i32) (i32.const 73))
				(elem declare func $target)
				(func (result funcref) (ref.func $target)))`)
				foreign, err := foreignRuntime.Instantiate(context.Background(), foreignModule)
				if err != nil {
					t.Fatal(err)
				}
				defer foreign.Close()
				export, err := foreign.ExportedFunc("target")
				if err != nil {
					t.Fatal(err)
				}
				ownerModule := mustCompileWat(rt, t, `(module
				(import "env" "foreign" (func $foreign (result i32)))
				(func $local (export "local") (result i32) (i32.const 19))
				(elem declare func $foreign $local)
				(func (result funcref) (ref.func $local)))`)
				owner, err := rt.Instantiate(context.Background(), ownerModule, WithImports(testImports("env.foreign", export)))
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				localDescriptor, ok := owner.localFuncrefDescriptor(0)
				if !ok {
					t.Fatal("owner has no local descriptor")
				}
				foreignDescriptor, ok := foreign.localFuncrefDescriptor(0)
				if !ok || !owner.reachesFuncrefDescriptor(foreignDescriptor) {
					t.Fatal("owner does not reach the imported foreign descriptor")
				}
				if err := foreign.Close(); err != nil {
					t.Fatal(err)
				}
				if !foreign.hasPhysicalResources() {
					t.Fatal("foreign producer was released before owner finalization")
				}

				var consumer *Instance
				var destinationRoots []*Instance
				var closeSource, closeDestination func() error
				handoverChecked := false
				retain := func() bool {
					return retainProducerRootsInImportedContainersForFinalizationWithBeforeTransfer(consumer, func() {
						handoverChecked = true
						if err := closeSource(); err == nil || !strings.Contains(err.Error(), "live importer") {
							t.Fatalf("source close before all attachment transfers = %v, want live importer", err)
						}
						if !owner.hasPhysicalResources() || !foreign.hasPhysicalResources() {
							t.Fatal("producer released before attachment handover")
						}
					})
				}
				switch kind {
				case "tables":
					first, _ := NewTable(0, 0)
					second, _ := NewTable(1, 1)
					third, _ := NewTable(1, 1)
					defer first.Close()
					defer second.Close()
					defer third.Close()
					localTable, foreignTable := second, third
					if order == "reverse" {
						localTable, foreignTable = third, second
					}
					closeSource, closeDestination = localTable.Close, foreignTable.Close
					module := mustCompileWat(rt, t, `(module
					(import "env" "first" (table 0 funcref))
					(import "env" "second" (table 1 funcref))
					(import "env" "third" (table 1 funcref))
					(import "env" "duplicate" (table 1 funcref)))`)
					consumer, err = rt.Instantiate(context.Background(), module, WithImports(testImports(
						"env.first", first, "env.second", second, "env.third", third,
						"env.duplicate", foreignTable)))
					if err != nil {
						t.Fatal(err)
					}
					binary.LittleEndian.PutUint64(localTable.desc[8+coreruntime.TableEntryRefSlotOffset:], localDescriptor)
					binary.LittleEndian.PutUint64(foreignTable.desc[8+coreruntime.TableEntryRefSlotOffset:], foreignDescriptor)
					if !retain() {
						t.Fatal("no table roots retained")
					}
					destinationRoots = foreignTable.funcrefProducerRoots()
				case "globals":
					first, _ := rt.NewFuncRefGlobal(NullFuncRef(), true)
					second, _ := rt.NewFuncRefGlobal(NullFuncRef(), true)
					third, _ := rt.NewFuncRefGlobal(NullFuncRef(), true)
					defer first.Close()
					defer second.Close()
					defer third.Close()
					localGlobal, foreignGlobal := second, third
					if order == "reverse" {
						localGlobal, foreignGlobal = third, second
					}
					closeSource, closeDestination = localGlobal.Close, foreignGlobal.Close
					module := mustCompileWat(rt, t, `(module
					(import "env" "first" (global (mut funcref)))
					(import "env" "second" (global (mut funcref)))
					(import "env" "third" (global (mut funcref))))`)
					consumer, err = rt.Instantiate(context.Background(), module, WithImports(testImports(
						"env.first", first, "env.second", second, "env.third", third)))
					if err != nil {
						t.Fatal(err)
					}
					binary.LittleEndian.PutUint64(localGlobal.cell, localDescriptor)
					binary.LittleEndian.PutUint64(foreignGlobal.cell, foreignDescriptor)
					if !retain() {
						t.Fatal("no global roots retained")
					}
					destinationRoots = foreignGlobal.funcrefProducerRoots()
				case "mixed":
					if order == "foreign-table" {
						foreignTable, _ := NewTable(1, 1)
						localGlobal, _ := rt.NewFuncRefGlobal(NullFuncRef(), true)
						defer foreignTable.Close()
						defer localGlobal.Close()
						closeSource, closeDestination = localGlobal.Close, foreignTable.Close
						module := mustCompileWat(rt, t, `(module
						(import "env" "foreign-table" (table 1 funcref))
						(import "env" "local-global" (global (mut funcref))))`)
						consumer, err = rt.Instantiate(context.Background(), module, WithImports(testImports(
							"env.foreign-table", foreignTable, "env.local-global", localGlobal)))
						if err != nil {
							t.Fatal(err)
						}
						binary.LittleEndian.PutUint64(foreignTable.desc[8+coreruntime.TableEntryRefSlotOffset:], foreignDescriptor)
						binary.LittleEndian.PutUint64(localGlobal.cell, localDescriptor)
					} else {
						localTable, _ := NewTable(1, 1)
						foreignGlobal, _ := rt.NewFuncRefGlobal(NullFuncRef(), true)
						defer localTable.Close()
						defer foreignGlobal.Close()
						closeSource, closeDestination = localTable.Close, foreignGlobal.Close
						module := mustCompileWat(rt, t, `(module
						(import "env" "local-table" (table 1 funcref))
						(import "env" "foreign-global" (global (mut funcref))))`)
						consumer, err = rt.Instantiate(context.Background(), module, WithImports(testImports(
							"env.local-table", localTable, "env.foreign-global", foreignGlobal)))
						if err != nil {
							t.Fatal(err)
						}
						binary.LittleEndian.PutUint64(localTable.desc[8+coreruntime.TableEntryRefSlotOffset:], localDescriptor)
						binary.LittleEndian.PutUint64(foreignGlobal.cell, foreignDescriptor)
					}
					if !retain() {
						t.Fatal("no mixed-container roots retained")
					}
					// Read roots after retention, including the cross-container owner.
					if order == "foreign-table" {
						provided, _ := consumer.imports.table(consumer.c.tableImportBindingKey(0))
						destinationRoots = provided.funcrefProducerRoots()
					} else {
						provided, _ := consumer.imports.global(consumer.c.globalImportBindingKey(0))
						destinationRoots = provided.Global.funcrefProducerRoots()
					}
				}
				if !handoverChecked {
					t.Fatal("retention did not reach the pre-handover invariant")
				}
				defer consumer.Close()
				found := false
				for _, root := range destinationRoots {
					if root == owner {
						found = true
					}
				}
				if !found {
					t.Fatal("foreign descriptor destination did not retain the discovered owner")
				}
				if err := consumer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := owner.Close(); err != nil {
					t.Fatal(err)
				}
				if err := closeSource(); err != nil {
					t.Fatal(err)
				}
				if !owner.hasPhysicalResources() {
					t.Fatal("owner resources released while the later destination still holds its descriptor")
				}
				if !foreign.hasPhysicalResources() {
					t.Fatal("foreign producer resources released while the later destination still holds its descriptor")
				}
				if err := closeDestination(); err != nil {
					t.Fatal(err)
				}
				if owner.hasPhysicalResources() {
					t.Fatal("owner resources remained after the final destination closed")
				}
				if foreign.hasPhysicalResources() {
					t.Fatal("foreign producer resources remained after the final destination closed")
				}
			})
		}
	}
}
