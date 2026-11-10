//go:build amd64

package amd64

import (
	"testing"
	"unsafe"
)

func TestSharingScratchLayout(t *testing.T) {
	t.Logf("scratch=%d fn=%d node=%d control=%d", unsafe.Sizeof(scratch{}), unsafe.Sizeof(fn{}), unsafe.Sizeof(elem{}), unsafe.Sizeof(ctrlFrame{}))
}
