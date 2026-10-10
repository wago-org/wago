//go:build !riscv64

package wago

import (
	"testing"
	"unsafe"
)

func TestTableDescriptorDependenciesExistingTargetFootprint(t *testing.T) {
	if unsafe.Sizeof(tableDescriptorDependencies{}) != 0 || unsafe.Sizeof(Table{}) != 64 || unsafe.Offsetof(tableOwner{}.mu) != 0 {
		t.Fatal("existing-target table descriptor sidecar changed object footprint")
	}
}
