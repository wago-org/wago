//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

type swarActiveData struct {
	index  uint32
	offset uint32
	bytes  []byte
}

// All segment shapes, literal offsets and aggregate resources are checked
// before copying any payload. Active descriptors remain dropped at their
// original indices; source-order initialization is kept separately.
func swarModuleData(data []wasm.Data, memory *wasm.MemType) ([][]byte, []swarActiveData, error) {
	if len(data) > 256 {
		return nil, nil, unsupported("SWAR data segment bound")
	}
	total := 0
	offsets := make([]uint32, len(data))
	for i, segment := range data {
		if len(segment.Init) > MaxModuleBytes-total {
			return nil, nil, unsupported("SWAR aggregate data byte bound")
		}
		total += len(segment.Init)
		switch segment.Mode.Kind {
		case wasm.DataPassive:
			if segment.Mode.Mem != 0 || len(segment.Mode.Offset.BodyBytes) != 0 || len(segment.Mode.Offset.Instrs) != 0 {
				return nil, nil, unsupported("SWAR passive data shape")
			}
		case wasm.DataActive:
			if memory == nil || memory.Shared || memory.Limits.Addr64 || segment.Mode.Mem != 0 {
				return nil, nil, unsupported("SWAR active data memory32")
			}
			offset, err := plainScalarConst(segment.Mode.Offset, wasm.I32, false)
			if err != nil {
				return nil, nil, err
			}
			offsets[i] = uint32(offset)
		default:
			return nil, nil, unsupported("SWAR data mode")
		}
	}
	passive := make([][]byte, len(data))
	var active []swarActiveData
	for i, segment := range data {
		payload := append([]byte(nil), segment.Init...)
		if segment.Mode.Kind == wasm.DataPassive {
			passive[i] = payload
		} else {
			active = append(active, swarActiveData{index: uint32(i), offset: offsets[i], bytes: payload})
		}
	}
	return passive, active, nil
}
