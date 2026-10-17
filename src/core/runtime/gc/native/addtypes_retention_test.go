//go:build !tinygo

package gc

import (
	"runtime"
	"testing"
	"time"
)

type rejectedTypeFields struct{ fields [1 << 18]FieldDesc }

//go:noinline
func addRejectedFields(c *Collector, done chan struct{}) error {
	fields := new(rejectedTypeFields)
	runtime.SetFinalizer(fields, func(*rejectedTypeFields) { close(done) })
	err := c.AddTypes([]TypeDesc{{ID: TypeID(len(c.types) + 1), Kind: KindStruct, Align: 1, Fields: fields.fields[:]}})
	runtime.KeepAlive(fields)
	return err
}

func TestAddTypesRejectedAppendDoesNotRetainFields(t *testing.T) {
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Make the rejected append reuse backing storage owned by the collector.
	c.types = make([]TypeDesc, 0, 1)
	done := make(chan struct{})
	if err := addRejectedFields(c, done); err == nil {
		t.Fatal("invalid descriptor accepted")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		select {
		case <-done:
			runtime.KeepAlive(c)
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
	runtime.KeepAlive(c)
	t.Fatal("rejected descriptor retains its field array")
}
