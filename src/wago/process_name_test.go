package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

type testHostModule struct{ mem []byte }

func (m testHostModule) Memory() []byte { return m.mem }

func TestSpawnRegistersAndReleasesName(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	class := classFor(t, rt, blockingReceiveModule(t))
	defer class.Close()

	pid, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run", Name: "worker"})
	if err != nil {
		t.Fatalf("spawn named process: %v", err)
	}
	if got, err := rt.LookupProcessName(context.Background(), "worker"); err != nil || got != pid {
		t.Fatalf("LookupProcessName(worker) = %d, %v; want %d, nil", got, err, pid)
	}
	if _, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run", Name: "worker"}); !errors.Is(err, ErrProcessNameTaken) {
		t.Fatalf("duplicate named spawn = %v, want ErrProcessNameTaken", err)
	}

	mon := mustMonitor(t, rt, pid)
	if err := rt.Kill(context.Background(), pid, ExitReason{}); err != nil {
		t.Fatalf("kill named process: %v", err)
	}
	<-mon
	if _, err := rt.LookupProcessName(context.Background(), "worker"); !errors.Is(err, ErrProcessNameNotFound) {
		t.Fatalf("LookupProcessName after exit = %v, want ErrProcessNameNotFound", err)
	}
}

func TestProcessNameHostRegisterLookupUnregister(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	class := classFor(t, rt, blockingReceiveModule(t))
	defer class.Close()

	pid, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer rt.Kill(context.Background(), pid, ExitReason{})

	if err := rt.RegisterProcessName(context.Background(), "alias", pid); err != nil {
		t.Fatalf("RegisterProcessName: %v", err)
	}
	if err := rt.RegisterProcessName(context.Background(), "alias", pid); err != nil {
		t.Fatalf("idempotent RegisterProcessName: %v", err)
	}
	if got, err := rt.LookupProcessName(context.Background(), "alias"); err != nil || got != pid {
		t.Fatalf("LookupProcessName(alias) = %d, %v; want %d, nil", got, err, pid)
	}
	if err := rt.UnregisterProcessName(context.Background(), "alias"); err != nil {
		t.Fatalf("UnregisterProcessName: %v", err)
	}
	if _, err := rt.LookupProcessName(context.Background(), "alias"); !errors.Is(err, ErrProcessNameNotFound) {
		t.Fatalf("LookupProcessName after unregister = %v, want ErrProcessNameNotFound", err)
	}
}

func TestProcessNameGuestImports(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	const pid PID = 77
	proc := &Process{PID: pid, mailbox: newMailbox(1)}
	rt.procs[pid] = proc

	mem := make([]byte, 64)
	copy(mem[0:], "worker")
	imports := rt.processImports(proc)
	var res [1]uint64

	imports["wago_process.register"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 6}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("guest register status = %d, want %d", got, statusOK)
	}
	imports["wago_process.get"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 6, 16}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("guest get status = %d, want %d", got, statusOK)
	}
	if got := PID(binary.LittleEndian.Uint64(mem[16:])); got != pid {
		t.Fatalf("guest get pid = %d, want %d", got, pid)
	}
	imports["wago_process.unregister"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 6}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("guest unregister status = %d, want %d", got, statusOK)
	}
	imports["wago_process.get"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 6, 16}, res[:])
	if got := int32(res[0]); got != statusNameNotFound {
		t.Fatalf("guest get after unregister = %d, want %d", got, statusNameNotFound)
	}
}

func TestProcessNameGuestUnregisterRequiresOwnership(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	const owner PID = 1
	const other PID = 2
	rt.procs[owner] = &Process{PID: owner, mailbox: newMailbox(1)}
	otherProc := &Process{PID: other, mailbox: newMailbox(1)}
	rt.procs[other] = otherProc
	if err := rt.RegisterProcessName(context.Background(), "owned", owner); err != nil {
		t.Fatalf("RegisterProcessName: %v", err)
	}

	mem := make([]byte, 64)
	copy(mem[0:], "owned")
	imports := rt.processImports(otherProc)
	var res [1]uint64
	imports["wago_process.unregister"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 5}, res[:])
	if got := int32(res[0]); got != statusPermissionDenied {
		t.Fatalf("guest unregister by non-owner = %d, want %d", got, statusPermissionDenied)
	}
	if got, err := rt.LookupProcessName(context.Background(), "owned"); err != nil || got != owner {
		t.Fatalf("LookupProcessName after denied unregister = %d, %v; want %d, nil", got, err, owner)
	}
}

func TestProcessNameGuestInvalidMemoryAndName(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	const pid PID = 3
	proc := &Process{PID: pid, mailbox: newMailbox(1)}
	rt.procs[pid] = proc
	imports := rt.processImports(proc)
	var res [1]uint64

	mem := make([]byte, 4)
	imports["wago_process.register"].(HostFunc)(testHostModule{mem: mem}, []uint64{2, 4}, res[:])
	if got := int32(res[0]); got != statusInvalidMemory {
		t.Fatalf("guest register invalid memory = %d, want %d", got, statusInvalidMemory)
	}
	imports["wago_process.register"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 0}, res[:])
	if got := int32(res[0]); got != statusInvalidName {
		t.Fatalf("guest register empty name = %d, want %d", got, statusInvalidName)
	}
}
