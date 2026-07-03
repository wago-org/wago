//go:build linux && amd64

package wago

import (
	"strings"
	"testing"
)

func TestCompiledCloseRejectsFutureInstantiate(t *testing.T) {
	c, err := Compile(fibWasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Instantiate(c, nil); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate after Close error = %v, want closed", err)
	}
}

func TestCompiledCloseKeepsExistingInstanceAlive(t *testing.T) {
	c, err := Compile(fibWasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close with live instance: %v", err)
	}
	if _, err := Instantiate(c, nil); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate after Close error = %v, want closed", err)
	}
	res, err := in.Invoke("fib", I32(10))
	if err != nil {
		t.Fatalf("Invoke after Compiled.Close: %v", err)
	}
	if got := AsI32(res[0]); got != 55 {
		t.Fatalf("fib(10) = %d, want 55", got)
	}
	in.Close()
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
