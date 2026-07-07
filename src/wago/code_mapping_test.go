//go:build linux && amd64

package wago

import (
	"strings"
	"testing"
	"unsafe"
)

func TestCompiledCloseRejectsFutureInstantiate(t *testing.T) {
	c, err := Compile(nil, fibWasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Instantiate(c, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate after Close error = %v, want closed", err)
	}
}

func TestCompiledCloseKeepsExistingInstanceAlive(t *testing.T) {
	c, err := Compile(nil, fibWasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	eng := in.eng
	jm := in.jm
	ar := in.ar
	serArgsPtr := unsafe.Pointer(nil)
	if len(in.serArgs) > 0 {
		serArgsPtr = unsafe.Pointer(&in.serArgs[0])
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close with live instance: %v", err)
	}
	if _, err := Instantiate(c, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate after Close error = %v, want closed", err)
	}
	if in.eng != eng || in.jm != jm || in.ar != ar {
		t.Fatalf("live instance runtime objects changed after failed Instantiate")
	}
	if len(in.serArgs) > 0 && unsafe.Pointer(&in.serArgs[0]) != serArgsPtr {
		t.Fatalf("live instance argument buffer changed after failed Instantiate")
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
