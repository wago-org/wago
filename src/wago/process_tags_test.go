package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestMailboxPrepareReceiveFiltersByTag(t *testing.T) {
	mb := newMailbox(4)
	if err := mb.send(7, []byte("tagged")); err != nil {
		t.Fatalf("send tagged: %v", err)
	}
	if err := mb.send(0, []byte("plain")); err != nil {
		t.Fatalf("send untagged: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.prepareReceive(mem, 4, 0, 0); got != statusOK {
		t.Fatalf("prepare untagged status = %d, want %d", got, statusOK)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 5 {
		t.Fatalf("untagged length = %d, want 5", n)
	}
	if got := mb.receivePrepared(mem, 16, 5); got != statusOK {
		t.Fatalf("receive untagged status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+5]); got != "plain" {
		t.Fatalf("untagged payload = %q, want plain", got)
	}

	if got := mb.prepareReceive(mem, 4, 7, 0); got != statusOK {
		t.Fatalf("prepare tagged status = %d, want %d", got, statusOK)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 6 {
		t.Fatalf("tagged length = %d, want 6", n)
	}
	if got := mb.receivePrepared(mem, 16, 6); got != statusOK {
		t.Fatalf("receive tagged status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+6]); got != "tagged" {
		t.Fatalf("tagged payload = %q, want tagged", got)
	}
}

func TestMailboxPrepareReceivePreservesMismatchedMessages(t *testing.T) {
	mb := newMailbox(4)
	if err := mb.send(9, []byte("nine")); err != nil {
		t.Fatalf("send tag 9: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.prepareReceive(mem, 4, 8, 0); got != statusWouldBlock {
		t.Fatalf("prepare wrong tag status = %d, want %d", got, statusWouldBlock)
	}
	if got := mb.queuedLen(); got != 1 {
		t.Fatalf("mailbox length after mismatched prepare = %d, want 1", got)
	}

	if got := mb.prepareReceive(mem, 4, 9, 0); got != statusOK {
		t.Fatalf("prepare tag 9 status = %d, want %d", got, statusOK)
	}
	if got := mb.receivePrepared(mem, 16, 4); got != statusOK {
		t.Fatalf("receive tag 9 status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+4]); got != "nine" {
		t.Fatalf("tag 9 payload = %q, want nine", got)
	}
}

func TestMailboxPrepareReceiveRequiresSizeAcknowledgement(t *testing.T) {
	mb := newMailbox(4)
	if err := mb.send(7, []byte("payload")); err != nil {
		t.Fatalf("send tag 7: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.prepareReceive(mem, 4, 7, 0); got != statusOK {
		t.Fatalf("prepare status = %d, want %d", got, statusOK)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 7 {
		t.Fatalf("prepared length = %d, want 7", n)
	}
	if got := mb.queuedLen(); got != 0 {
		t.Fatalf("mailbox length after prepare = %d, want 0", got)
	}

	if got := mb.receivePrepared(mem, 16, 6); got != statusSizeMismatch {
		t.Fatalf("receive with wrong ack size = %d, want %d", got, statusSizeMismatch)
	}
	if got := mb.receivePrepared(mem, 16, 7); got != statusOK {
		t.Fatalf("receive with acked size = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+7]); got != "payload" {
		t.Fatalf("prepared payload = %q, want payload", got)
	}
	if got := mb.receivePrepared(mem, 16, 7); got != statusNoPreparedMessage {
		t.Fatalf("receive after consume = %d, want %d", got, statusNoPreparedMessage)
	}
}

func TestMailboxReceivePreparedInvalidMemoryKeepsPending(t *testing.T) {
	mb := newMailbox(2)
	if err := mb.send(12, []byte("message")); err != nil {
		t.Fatalf("send tag 12: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.prepareReceive(mem, 4, 12, 0); got != statusOK {
		t.Fatalf("prepare status = %d, want %d", got, statusOK)
	}
	if got := mb.receivePrepared(mem, 60, 7); got != statusInvalidMemory {
		t.Fatalf("too-small destination status = %d, want %d", got, statusInvalidMemory)
	}
	if got := mb.receivePrepared(mem, 16, 7); got != statusOK {
		t.Fatalf("retry receive status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+7]); got != "message" {
		t.Fatalf("retried prepared payload = %q, want message", got)
	}
}

func TestMailboxPrepareReceiveRejectsBadLengthPointerWithoutConsuming(t *testing.T) {
	mb := newMailbox(2)
	if err := mb.send(4, []byte("keep")); err != nil {
		t.Fatalf("send tag 4: %v", err)
	}

	shortMem := make([]byte, 4)
	if got := mb.prepareReceive(shortMem, 1, 4, 0); got != statusInvalidMemory {
		t.Fatalf("prepare with bad length pointer = %d, want %d", got, statusInvalidMemory)
	}
	if got := mb.queuedLen(); got != 1 {
		t.Fatalf("mailbox length after bad length pointer = %d, want 1", got)
	}

	mem := make([]byte, 64)
	if got := mb.prepareReceive(mem, 4, 4, 0); got != statusOK {
		t.Fatalf("prepare after bad pointer = %d, want %d", got, statusOK)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 4 {
		t.Fatalf("prepared length after bad pointer = %d, want 4", n)
	}
	if got := mb.receivePrepared(mem, 16, 4); got != statusOK {
		t.Fatalf("receive after bad pointer = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+4]); got != "keep" {
		t.Fatalf("payload after bad pointer = %q, want keep", got)
	}
}

func TestMailboxPrepareReceiveZeroLengthMessage(t *testing.T) {
	mb := newMailbox(1)
	if err := mb.send(2, nil); err != nil {
		t.Fatalf("send zero-length message: %v", err)
	}

	mem := make([]byte, 16)
	if got := mb.prepareReceive(mem, 4, 2, 0); got != statusOK {
		t.Fatalf("prepare zero-length status = %d, want %d", got, statusOK)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 0 {
		t.Fatalf("zero-length prepared length = %d, want 0", n)
	}
	if got := mb.receivePrepared(mem, uint32(len(mem)), 0); got != statusOK {
		t.Fatalf("receive zero-length at end pointer = %d, want %d", got, statusOK)
	}
}

func TestMailboxPrepareReceiveRejectsOutOfRangeZeroLengthDestination(t *testing.T) {
	mb := newMailbox(1)
	if err := mb.send(2, nil); err != nil {
		t.Fatalf("send zero-length message: %v", err)
	}

	mem := make([]byte, 16)
	if got := mb.prepareReceive(mem, 4, 2, 0); got != statusOK {
		t.Fatalf("prepare zero-length status = %d, want %d", got, statusOK)
	}
	if got := mb.receivePrepared(mem, uint32(len(mem)+1), 0); got != statusInvalidMemory {
		t.Fatalf("receive zero-length out of range = %d, want %d", got, statusInvalidMemory)
	}
	if got := mb.receivePrepared(mem, uint32(len(mem)), 0); got != statusOK {
		t.Fatalf("retry zero-length receive = %d, want %d", got, statusOK)
	}
}

func TestMailboxPrepareReceiveCloseSemantics(t *testing.T) {
	mb := newMailbox(4)
	if err := mb.send(1, []byte("one")); err != nil {
		t.Fatalf("send tag 1: %v", err)
	}
	if err := mb.send(2, []byte("two")); err != nil {
		t.Fatalf("send tag 2: %v", err)
	}
	mb.close()

	mem := make([]byte, 64)
	if got := mb.prepareReceive(mem, 4, 3, 0); got != statusMailboxClosed {
		t.Fatalf("prepare missing tag on closed mailbox = %d, want %d", got, statusMailboxClosed)
	}
	if got := mb.prepareReceive(mem, 4, 2, 0); got != statusOK {
		t.Fatalf("prepare queued tag on closed mailbox = %d, want %d", got, statusOK)
	}
	if got := mb.receivePrepared(mem, 16, 3); got != statusOK {
		t.Fatalf("receive queued tag on closed mailbox = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+3]); got != "two" {
		t.Fatalf("closed queued payload = %q, want two", got)
	}
	if got := mb.prepareReceive(mem, 4, 2, 0); got != statusMailboxClosed {
		t.Fatalf("prepare consumed tag on closed mailbox = %d, want %d", got, statusMailboxClosed)
	}
	if got := mb.prepareReceive(mem, 4, 1, 0); got != statusOK {
		t.Fatalf("prepare remaining queued tag on closed mailbox = %d, want %d", got, statusOK)
	}
}

func TestMailboxPrepareReceiveTimeout(t *testing.T) {
	mb := newMailbox(1)
	mem := make([]byte, 64)
	start := time.Now()
	if got := mb.prepareReceive(mem, 4, 99, 5); got != statusTimeout {
		t.Fatalf("prepare timeout status = %d, want %d", got, statusTimeout)
	}
	if elapsed := time.Since(start); elapsed <= 0 {
		t.Fatalf("prepare timeout elapsed = %s, want positive duration", elapsed)
	}
}

func TestMailboxPrepareReceiveRejectsSecondPendingMessage(t *testing.T) {
	mb := newMailbox(4)
	if err := mb.send(1, []byte("one")); err != nil {
		t.Fatalf("send tag 1: %v", err)
	}
	if err := mb.send(1, []byte("two")); err != nil {
		t.Fatalf("send tag 1 second: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.prepareReceive(mem, 4, 1, 0); got != statusOK {
		t.Fatalf("first prepare status = %d, want %d", got, statusOK)
	}
	if got := mb.prepareReceive(mem, 4, 1, 0); got != statusPendingMessage {
		t.Fatalf("second prepare status = %d, want %d", got, statusPendingMessage)
	}
	if got := mb.receivePrepared(mem, 16, 3); got != statusOK {
		t.Fatalf("receive first pending = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+3]); got != "one" {
		t.Fatalf("first pending payload = %q, want one", got)
	}
	if got := mb.prepareReceive(mem, 4, 1, 0); got != statusOK {
		t.Fatalf("prepare after consume = %d, want %d", got, statusOK)
	}
	if got := mb.receivePrepared(mem, 16, 3); got != statusOK {
		t.Fatalf("receive second pending = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+3]); got != "two" {
		t.Fatalf("second pending payload = %q, want two", got)
	}
}

func TestMailboxCapacityAndClosedSendErrors(t *testing.T) {
	mb := newMailbox(1)
	if err := mb.send(0, []byte("one")); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := mb.send(0, []byte("two")); !errors.Is(err, ErrMailboxFull) {
		t.Fatalf("second send = %v, want ErrMailboxFull", err)
	}
	mb.close()
	if err := mb.send(0, []byte("three")); !errors.Is(err, ErrMailboxClosed) {
		t.Fatalf("send after close = %v, want ErrMailboxClosed", err)
	}
}

func TestRuntimeSendTagged(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	const pid PID = 42
	rt.procs[pid] = &Process{PID: pid, mailbox: newMailbox(2)}

	if err := rt.SendTagged(context.Background(), pid, 55, []byte("hello")); err != nil {
		t.Fatalf("SendTagged: %v", err)
	}
	if err := rt.Send(context.Background(), pid, []byte("plain")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mem := make([]byte, 64)
	mb := rt.procs[pid].mailbox
	if got := mb.prepareReceive(mem, 4, 0, 0); got != statusOK {
		t.Fatalf("prepare untagged status = %d, want %d", got, statusOK)
	}
	if got := mb.receivePrepared(mem, 16, binary.LittleEndian.Uint32(mem[4:])); got != statusOK {
		t.Fatalf("receive untagged status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+5]); got != "plain" {
		t.Fatalf("untagged payload = %q, want plain", got)
	}
	if got := mb.prepareReceive(mem, 4, 55, 0); got != statusOK {
		t.Fatalf("prepare tagged status = %d, want %d", got, statusOK)
	}
	if got := mb.receivePrepared(mem, 16, binary.LittleEndian.Uint32(mem[4:])); got != statusOK {
		t.Fatalf("receive prepared tagged status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+5]); got != "hello" {
		t.Fatalf("tagged payload = %q, want hello", got)
	}
}
