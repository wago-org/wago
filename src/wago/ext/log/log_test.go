package log_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/wago/ext/exttest"
	"github.com/wago-org/wago/src/wago/ext/log"
)

func TestLogWrite(t *testing.T) {
	var buf bytes.Buffer
	rt := wago.NewRuntime()
	if err := rt.Use(log.Ext(log.WithWriter(&buf))); err != nil {
		t.Fatalf("use: %v", err)
	}
	// run() -> i32 = write(level=2 (WARN), ptr=0, len=5) over memory "hello".
	body := []byte{
		0x41, 0x02, // i32.const 2 (level)
		0x41, 0x00, // i32.const 0 (ptr)
		0x41, 0x05, // i32.const 5 (len)
		0x10, 0x00, // call 0
		0x0b, // end
	}
	mod := exttest.Module("wago_log", "write",
		[]byte{exttest.I32, exttest.I32, exttest.I32}, []byte{exttest.I32},
		[]byte{exttest.I32}, body, []byte("hello"))
	m, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), m)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	out, err := in.Call(context.Background(), "run")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out[0].I32() != 0 {
		t.Fatalf("write status = %d, want 0", out[0].I32())
	}
	got := buf.String()
	if !strings.Contains(got, "hello") || !strings.Contains(got, "WARN") {
		t.Fatalf("log output = %q, want it to contain WARN and hello", got)
	}
}
