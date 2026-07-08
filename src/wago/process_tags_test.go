package wago

import (
	"context"
	"encoding/binary"
	"testing"
)

func TestMailboxReceiveFiltersByTag(t *testing.T) {
	mb := newMailbox(4)
	if err := mb.send(7, []byte("tagged")); err != nil {
		t.Fatalf("send tagged: %v", err)
	}
	if err := mb.send(0, []byte("plain")); err != nil {
		t.Fatalf("send untagged: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.receiveIntoTag(mem, 0, 16, 16, 4, 0); got != statusOK {
		t.Fatalf("receive untagged status = %d, want %d", got, statusOK)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 5 {
		t.Fatalf("untagged length = %d, want 5", n)
	}
	if got := string(mem[16 : 16+5]); got != "plain" {
		t.Fatalf("untagged payload = %q, want plain", got)
	}

	if got := mb.receiveIntoTag(mem, 7, 16, 16, 4, 0); got != statusOK {
		t.Fatalf("receive tagged status = %d, want %d", got, statusOK)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 6 {
		t.Fatalf("tagged length = %d, want 6", n)
	}
	if got := string(mem[16 : 16+6]); got != "tagged" {
		t.Fatalf("tagged payload = %q, want tagged", got)
	}
}

func TestMailboxTaggedReceivePreservesMismatchedMessages(t *testing.T) {
	mb := newMailbox(4)
	if err := mb.send(9, []byte("nine")); err != nil {
		t.Fatalf("send tag 9: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.receiveIntoTag(mem, 8, 16, 16, 4, 0); got != statusWouldBlock {
		t.Fatalf("receive wrong tag status = %d, want %d", got, statusWouldBlock)
	}
	if got := mb.length(); got != 1 {
		t.Fatalf("mailbox length after mismatched receive = %d, want 1", got)
	}

	if got := mb.receiveIntoTag(mem, 9, 16, 16, 4, 0); got != statusOK {
		t.Fatalf("receive tag 9 status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+4]); got != "nine" {
		t.Fatalf("tag 9 payload = %q, want nine", got)
	}
}

func TestMailboxTaggedBufferTooSmallLeavesMessageQueued(t *testing.T) {
	mb := newMailbox(2)
	if err := mb.send(3, []byte("payload")); err != nil {
		t.Fatalf("send tag 3: %v", err)
	}

	mem := make([]byte, 64)
	if got := mb.receiveIntoTag(mem, 3, 16, 3, 4, 0); got != statusBufTooSmall {
		t.Fatalf("short receive status = %d, want %d", got, statusBufTooSmall)
	}
	if n := binary.LittleEndian.Uint32(mem[4:]); n != 7 {
		t.Fatalf("short receive length = %d, want 7", n)
	}
	if got := mb.length(); got != 1 {
		t.Fatalf("mailbox length after short receive = %d, want 1", got)
	}

	if got := mb.receiveIntoTag(mem, 3, 16, 7, 4, 0); got != statusOK {
		t.Fatalf("retry receive status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+7]); got != "payload" {
		t.Fatalf("retried payload = %q, want payload", got)
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
	if got := mb.receiveIntoTag(mem, 0, 16, 16, 4, 0); got != statusOK {
		t.Fatalf("receive untagged status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+5]); got != "plain" {
		t.Fatalf("untagged payload = %q, want plain", got)
	}
	if got := mb.receiveIntoTag(mem, 55, 16, 16, 4, 0); got != statusOK {
		t.Fatalf("receive tagged status = %d, want %d", got, statusOK)
	}
	if got := string(mem[16 : 16+5]); got != "hello" {
		t.Fatalf("tagged payload = %q, want hello", got)
	}
}
