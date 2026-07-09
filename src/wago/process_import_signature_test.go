package wago

import (
	"context"
	"strings"
	"testing"
)

func TestSpawnRejectsMismatchedProcessImportSignature(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	// Declare recv_tagged with four params instead of the required five. The module
	// itself is valid because its call site matches the declared import type, but
	// Spawn must reject it before binding the reserved process host import.
	body := []byte{0x41, 0x00} // i32.const 0 (buf_ptr)
	body = append(body, 0x41, 0x00) // i32.const 0 (buf_cap)
	body = append(body, 0x41, 0x00) // i32.const 0 (out_len_ptr)
	body = append(body, 0x42, 0x00) // i64.const 0 (tag)
	body = append(body, 0x10, 0x00, 0x0b) // call 0; end

	mod := procModule(t, []impSpec{{
		module:  "wago_mailbox",
		name:    "recv_tagged",
		params:  []byte{i32b, i32b, i32b, i64b},
		results: []byte{i32b},
	}}, 1, []byte{i32b}, body)
	class := classFor(t, rt, mod)
	defer class.Close()

	pid, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run"})
	if err == nil {
		_ = rt.Kill(context.Background(), pid, ExitReason{})
		t.Fatal("Spawn succeeded with mismatched reserved process import signature")
	}
	if !strings.Contains(err.Error(), "wago_mailbox.recv_tagged") || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("Spawn error = %v, want recv_tagged signature mismatch", err)
	}
}
