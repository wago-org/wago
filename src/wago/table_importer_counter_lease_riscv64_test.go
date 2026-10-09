//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"strings"
	"testing"
)

func TestRV64TableImporterCounterParkedEntryStillOwnsStorageLease(t *testing.T) {
	table, err := NewTable(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer table.Close()
	c, err := Compile(ownedTableConfig().WithIndependentInstanceExecution(false), tableCounterModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	consumer, err := Instantiate(c, NewImports().Table("env", "table", table))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	// This acquires the actual production entry scope and its writer/storage
	// obligations. No guest instruction is executed while the scope is parked.
	entry, err := consumer.beginNativeEntry()
	if err != nil {
		t.Fatal(err)
	}
	if entry.local != nil || entry.tables.targets == nil {
		consumer.unlockNativeEntry(entry)
		t.Fatal("fixture did not acquire a process-mutex table scope")
	}
	// Park only the execution mutex, as a host callback does. The ordinary
	// unlock helper retires and releases the scope, so cannot park this lease.
	nativeExecutionMu.Unlock()
	held := true
	release := func() {
		nativeExecutionMu.Lock()
		err := railshotRetireNativeTableTargets(entry.tables)
		nativeExecutionMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		railshotReleaseNativeTableTargets(entry.tables)
		held = false
	}
	defer func() {
		if held {
			release()
		}
	}()
	if table.owner.nativeLeases != 1 || table.owner.importers != 1 || entry.tables.targets == nil {
		t.Fatal("fixture did not acquire real importer and native storage units")
	}
	if err := consumer.Close(); err != nil || consumer.resourcesClosed {
		t.Fatal("parked entry lost its real consumer lifetime", err)
	}
	consumer.transferImportedTableAttachment(table)
	if table.owner.importers != 0 || table.owner.nativeLeases != 1 || len(table.owner.funcrefStores) != 0 {
		t.Fatal("ordinary counter consumption changed native scope ownership")
	}
	if err := table.Close(); err == nil || !strings.Contains(err.Error(), "active native storage leases") || table.owner.closed {
		t.Fatal("zero importer count admitted Close during a real native lease", err)
	}
	release()
	if table.owner.nativeLeases != 0 || table.owner.importers != 0 || !consumer.resourcesClosed || consumer.resourceRefs != 0 || consumer.jm != nil || consumer.ar != nil || consumer.eng != nil || consumer.nativeContext != 0 {
		t.Fatal("real entry-scope release stranded native or ordinary ownership")
	}
	if err := table.Close(); err != nil {
		t.Fatal(err)
	}
}
