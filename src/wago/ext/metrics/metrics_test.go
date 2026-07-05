package metrics_test

import (
	"context"
	"testing"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/wago/ext/exttest"
	"github.com/wago-org/wago/src/wago/ext/metrics"
)

func TestMetricsCounterAdd(t *testing.T) {
	rt := wago.NewRuntime()
	m := metrics.Ext()
	if err := rt.Use(m); err != nil {
		t.Fatalf("use: %v", err)
	}
	// run() -> i32 = counter_add(ptr=0, len=4, delta=5) over memory "reqs".
	body := []byte{
		0x41, 0x00, // i32.const 0 (name_ptr)
		0x41, 0x04, // i32.const 4 (name_len)
		0x42, 0x05, // i64.const 5 (delta)
		0x10, 0x00, // call 0
		0x0b, // end
	}
	mod := exttest.Module("wago_metrics", "counter_add",
		[]byte{exttest.I32, exttest.I32, exttest.I64}, []byte{exttest.I32},
		[]byte{exttest.I32}, body, []byte("reqs"))
	comp, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), comp)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	if _, err := in.Call(context.Background(), "run"); err != nil {
		t.Fatalf("call: %v", err)
	}
	if _, err := in.Call(context.Background(), "run"); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if got := m.Counter("reqs"); got != 10 {
		t.Fatalf("counter reqs = %d, want 10", got)
	}
}

func TestMetricsInMemorySink(t *testing.T) {
	m := metrics.Ext()
	// Exercise the in-memory sink directly through the extension accessors.
	if got := m.Counter("absent"); got != 0 {
		t.Fatalf("absent counter = %d, want 0", got)
	}
	if h := m.Histogram("absent"); len(h) != 0 {
		t.Fatalf("absent histogram = %v, want empty", h)
	}
}
