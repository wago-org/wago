package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func TestGuestMonitorReceivesNormalExit(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	watcher := &Process{PID: 1, mailbox: newMailbox(1)}
	target := &Process{PID: 2, mailbox: newMailbox(1)}
	rt.procs[watcher.PID] = watcher
	rt.procs[target.PID] = target

	imports := rt.processImports(watcher)
	var res [1]uint64
	imports["wago_process.monitor"].(HostFunc)(testHostModule{}, []uint64{uint64(target.PID)}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("monitor status = %d, want %d", got, statusOK)
	}

	rt.finishProcess(target, nil, nil)
	mem := make([]byte, 64)
	imports["wago_monitor.prepare"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 8, 12, 0}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("monitor prepare status = %d, want %d", got, statusOK)
	}
	if got := PID(binary.LittleEndian.Uint64(mem[0:])); got != target.PID {
		t.Fatalf("monitor pid = %d, want %d", got, target.PID)
	}
	if got := int32(binary.LittleEndian.Uint32(mem[8:])); got != monitorReasonNormal {
		t.Fatalf("monitor reason = %d, want %d", got, monitorReasonNormal)
	}
	if got := binary.LittleEndian.Uint32(mem[12:]); got != 0 {
		t.Fatalf("monitor payload length = %d, want 0", got)
	}
	imports["wago_monitor.receive"].(HostFunc)(testHostModule{mem: mem}, []uint64{16, 0}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("monitor receive status = %d, want %d", got, statusOK)
	}
	imports["wago_monitor.receive"].(HostFunc)(testHostModule{mem: mem}, []uint64{16, 0}, res[:])
	if got := int32(res[0]); got != statusNoPreparedMessage {
		t.Fatalf("monitor receive after consume = %d, want %d", got, statusNoPreparedMessage)
	}
}

func TestGuestMonitorReceivesErrorPayloadAndRetries(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	watcher := &Process{PID: 1, mailbox: newMailbox(1)}
	target := &Process{PID: 2, mailbox: newMailbox(1)}
	rt.procs[watcher.PID] = watcher
	rt.procs[target.PID] = target

	imports := rt.processImports(watcher)
	var res [1]uint64
	imports["wago_process.monitor"].(HostFunc)(testHostModule{}, []uint64{uint64(target.PID)}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("monitor status = %d, want %d", got, statusOK)
	}

	rt.finishProcess(target, nil, errors.New("boom"))
	mem := make([]byte, 64)
	imports["wago_monitor.prepare"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 8, 12, 0}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("monitor prepare status = %d, want %d", got, statusOK)
	}
	if got := int32(binary.LittleEndian.Uint32(mem[8:])); got != monitorReasonError {
		t.Fatalf("monitor reason = %d, want %d", got, monitorReasonError)
	}
	if got := binary.LittleEndian.Uint32(mem[12:]); got != 4 {
		t.Fatalf("monitor payload length = %d, want 4", got)
	}
	imports["wago_monitor.receive"].(HostFunc)(testHostModule{mem: mem}, []uint64{16, 3}, res[:])
	if got := int32(res[0]); got != statusSizeMismatch {
		t.Fatalf("monitor receive wrong size = %d, want %d", got, statusSizeMismatch)
	}
	imports["wago_monitor.receive"].(HostFunc)(testHostModule{mem: mem}, []uint64{16, 4}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("monitor receive retry = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+4]); got != "boom" {
		t.Fatalf("monitor payload = %q, want boom", got)
	}
}

func TestGuestMonitorImmediateEventForAlreadyExitedProcess(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	watcher := &Process{PID: 1, mailbox: newMailbox(1)}
	target := &Process{PID: 2, mailbox: newMailbox(1)}
	rt.procs[watcher.PID] = watcher
	rt.procs[target.PID] = target
	rt.finishProcess(target, nil, nil)

	imports := rt.processImports(watcher)
	var res [1]uint64
	imports["wago_process.monitor"].(HostFunc)(testHostModule{}, []uint64{uint64(target.PID)}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("monitor exited process = %d, want %d", got, statusOK)
	}
	mem := make([]byte, 64)
	imports["wago_monitor.prepare"].(HostFunc)(testHostModule{mem: mem}, []uint64{0, 8, 12, 0}, res[:])
	if got := int32(res[0]); got != statusOK {
		t.Fatalf("prepare immediate monitor event = %d, want %d", got, statusOK)
	}
	if got := PID(binary.LittleEndian.Uint64(mem[0:])); got != target.PID {
		t.Fatalf("immediate monitor pid = %d, want %d", got, target.PID)
	}
}

func TestGuestMonitorPrepareInvalidMemoryKeepsEvent(t *testing.T) {
	proc := &Process{PID: 1, mailbox: newMailbox(1)}
	proc.enqueueMonitorEvent(monitorEvent{PID: 2, Reason: monitorReasonKilled})

	shortMem := make([]byte, 8)
	if got := proc.prepareMonitor(shortMem, 0, 8, 12, 0); got != statusInvalidMemory {
		t.Fatalf("prepare invalid memory = %d, want %d", got, statusInvalidMemory)
	}
	mem := make([]byte, 64)
	if got := proc.prepareMonitor(mem, 0, 8, 12, 0); got != statusOK {
		t.Fatalf("prepare after invalid memory = %d, want %d", got, statusOK)
	}
	if got := PID(binary.LittleEndian.Uint64(mem[0:])); got != 2 {
		t.Fatalf("monitor pid after invalid memory = %d, want 2", got)
	}
}

func TestGuestMonitorPrepareTimeoutAndWouldBlock(t *testing.T) {
	proc := &Process{PID: 1, mailbox: newMailbox(1)}
	mem := make([]byte, 64)
	if got := proc.prepareMonitor(mem, 0, 8, 12, 0); got != statusWouldBlock {
		t.Fatalf("monitor prepare nonblocking empty = %d, want %d", got, statusWouldBlock)
	}
	if got := proc.prepareMonitor(mem, 0, 8, 12, 1); got != statusTimeout {
		t.Fatalf("monitor prepare timeout = %d, want %d", got, statusTimeout)
	}
}

func TestGuestMonitorUnknownProcess(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	watcher := &Process{PID: 1, mailbox: newMailbox(1)}
	rt.procs[watcher.PID] = watcher
	imports := rt.processImports(watcher)
	var res [1]uint64
	imports["wago_process.monitor"].(HostFunc)(testHostModule{}, []uint64{999}, res[:])
	if got := int32(res[0]); got != statusNoProcess {
		t.Fatalf("monitor unknown process = %d, want %d", got, statusNoProcess)
	}
}

func TestMonitorProcessHostAPI(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	watcher := &Process{PID: 1, mailbox: newMailbox(1)}
	target := &Process{PID: 2, mailbox: newMailbox(1)}
	rt.procs[watcher.PID] = watcher
	rt.procs[target.PID] = target
	if err := rt.MonitorProcess(context.Background(), watcher.PID, target.PID); err != nil {
		t.Fatalf("MonitorProcess: %v", err)
	}
	rt.finishProcess(target, nil, nil)
	mem := make([]byte, 64)
	if got := watcher.prepareMonitor(mem, 0, 8, 12, 0); got != statusOK {
		t.Fatalf("prepare monitor event from host API = %d, want %d", got, statusOK)
	}
}
