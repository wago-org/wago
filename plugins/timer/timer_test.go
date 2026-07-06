package timer_test

import (
	"context"
	"testing"
	"time"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/plugins/exttest"
	"github.com/wago-org/wago/plugins/timer"
)

type fakeClock struct{ mono int64 }

func (f fakeClock) UnixMilli() int64      { return 12345 }
func (f fakeClock) MonotonicNanos() int64 { return f.mono }
func (f fakeClock) Sleep(d time.Duration) {}

func TestTimerNowUnixMs(t *testing.T) {
	rt := wago.NewRuntime()
	if err := rt.Use(timer.Ext(timer.WithClock(fakeClock{}))); err != nil {
		t.Fatalf("use: %v", err)
	}
	// run() -> i64 = now_unix_ms()
	body := []byte{0x10, 0x00, 0x0b} // call 0; end
	mod := exttest.Module("wago_timer", "now_unix_ms", nil, []byte{exttest.I64}, []byte{exttest.I64}, body, nil)
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
	if out[0].I64() != 12345 {
		t.Fatalf("now_unix_ms = %d, want 12345", out[0].I64())
	}
}

func TestTimerCapabilityAndInfo(t *testing.T) {
	rt := wago.NewRuntime()
	if err := rt.Use(timer.Ext()); err != nil {
		t.Fatalf("use: %v", err)
	}
	if exts := rt.Extensions(); len(exts) != 1 || exts[0].ID != "wago.timer" {
		t.Fatalf("extensions = %+v", exts)
	}
	caps := rt.Capabilities()
	if len(caps) != 1 || caps[0] != timer.CapRead {
		t.Fatalf("capabilities = %v", caps)
	}
}
