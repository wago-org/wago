package wago

import (
	"context"
	"strings"
	"testing"
)

func TestSpawnRejectsMismatchedProcessImportSignature(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()

	// Declare wago_mailbox.send with the old three-parameter shape instead of the
	// canonical four-parameter envelope (pid, tag, ptr, len). The module itself is
	// valid because its call site matches the declared import type, but Spawn must
	// reject it before binding the reserved process host import.
	body := []byte{0x42, 0x00}       // i64.const 0 (pid)
	body = append(body, 0x41, 0x00)  // i32.const 0 (ptr)
	body = append(body, 0x41, 0x00)  // i32.const 0 (len)
	body = append(body, 0x10, 0x00, 0x0b) // call 0; end

	mod := procModule(t, []impSpec{{
		module:  "wago_mailbox",
		name:    "send",
		params:  []byte{i64b, i32b, i32b},
		results: []byte{i32b},
	}}, 1, []byte{i32b}, body)
	class := classFor(t, rt, mod)
	defer class.Close()

	pid, err := rt.Spawn(context.Background(), class, SpawnOptions{Entry: "run"})
	if err == nil {
		_ = rt.Kill(context.Background(), pid, ExitReason{})
		t.Fatal("Spawn succeeded with mismatched reserved process import signature")
	}
	if !strings.Contains(err.Error(), "wago_mailbox.send") || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("Spawn error = %v, want send signature mismatch", err)
	}
}
