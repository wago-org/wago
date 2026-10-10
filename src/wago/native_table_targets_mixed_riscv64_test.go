//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRV64NativeTableTargetScannerUsesDeclaredRowWidth(t *testing.T) {
	compact := make([]byte, 8+2*8)
	binary.LittleEndian.PutUint32(compact, 2)
	binary.LittleEndian.PutUint32(compact[4:], 2)
	binary.LittleEndian.PutUint64(compact[8:], 0xfedcba9876543210)
	binary.LittleEndian.PutUint64(compact[16:], 0x0123456789abcdef)

	var targets rv64NativeTableTargets
	if err := targets.validateTable(compact, 8); err != nil {
		t.Fatal("opaque compact reference rows", err)
	}
	if err := targets.validateTable(compact, coreruntime.TableEntryBytes); err == nil {
		t.Fatal("compact storage accepted as full-width descriptors")
	}
	if err := targets.validateTable(compact, 16); err == nil {
		t.Fatal("unknown table row width accepted")
	}

	full := make([]byte, 8+coreruntime.TableEntryBytes)
	binary.LittleEndian.PutUint32(full, 1)
	binary.LittleEndian.PutUint32(full[4:], 1)
	if err := targets.validateTable(full, coreruntime.TableEntryBytes); err != nil {
		t.Fatal("canonical null descriptor row", err)
	}
	full[8] = 1
	if err := targets.validateTable(full, coreruntime.TableEntryBytes); err == nil {
		t.Fatal("unauthenticated full-width row accepted")
	}
}

func TestRV64NativeTableTargetScannerBoundsCompactExtent(t *testing.T) {
	var targets rv64NativeTableTargets
	for name, descriptor := range map[string][]byte{
		"header":   make([]byte, 7),
		"capacity": append([]byte{1, 0, 0, 0, 2, 0, 0, 0}, make([]byte, 8)...),
		"size":     []byte{2, 0, 0, 0, 1, 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if err := targets.validateTable(descriptor, 8); err == nil {
				t.Fatal("invalid compact table descriptor accepted")
			}
		})
	}
}
