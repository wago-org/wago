//go:build linux && !tinygo

package riscv64

import "github.com/wago-org/wago/src/core/compiler/wasm"

// The frontend validates type relations. This pass checks the retained public
// function grammar directly in source bytes, without rebuilding its type graph.
func admitPublicFunctionTypeWire(s *wasm.Reader, floating, vectors bool) error {
	count, err := s.U32()
	if err != nil || uint64(count) > uint64(s.BytesLeft()) {
		return unsupported("public wire type count")
	}
	for range count {
		kind, err := s.Byte()
		if err != nil {
			return err
		}
		members := uint32(1)
		if kind == 0x4e {
			members, err = s.U32()
			if err != nil || uint64(members) > uint64(s.BytesLeft()) {
				return unsupported("public recursive function group bound")
			}
			if members == 0 {
				continue
			}
			kind, err = s.Byte()
			if err != nil {
				return err
			}
		}
		for member := uint32(0); member < members; member++ {
			if member != 0 {
				kind, err = s.Byte()
				if err != nil {
					return err
				}
			}
			if kind == 0x4f || kind == 0x50 {
				supers, err := s.U32()
				if err != nil || uint64(supers) > uint64(s.BytesLeft()) {
					return unsupported("public function supertype bound")
				}
				for range supers {
					if _, err := s.U32(); err != nil {
						return err
					}
				}
				kind, err = s.Byte()
				if err != nil {
					return err
				}
			}
			if kind == 0x5e || kind == 0x5f {
				fields := uint32(1)
				if kind == 0x5f {
					fields, err = s.U32()
					if err != nil || uint64(fields) > uint64(s.BytesLeft())/2 {
						return unsupported("public aggregate field bound")
					}
				}
				for range fields {
					if err := admitPublicAggregateFieldWire(s, floating, vectors); err != nil {
						return err
					}
				}
				continue
			}
			if kind != 0x60 {
				return unsupported("public function type grammar")
			}
			for range 2 {
				arity, err := s.U32()
				if err != nil || arity > 256 {
					return unsupported("public function signature arity")
				}
				for range arity {
					typ, err := s.Byte()
					if err != nil {
						return err
					}
					if typ >= 0x6a && typ <= 0x73 {
						continue
					}
					if typ == 0x63 || typ == 0x64 {
						heap, err := s.S33()
						if err != nil {
							return err
						}
						if heap >= 0 || heap >= -22 && heap <= -13 {
							continue
						}
						return unsupported("public function reference heap")
					}
					if typ != 0x7f && typ != 0x7e && (!floating || typ != 0x7d && typ != 0x7c) && (!vectors || typ != 0x7b) {
						return unsupported("public function numeric type")
					}
				}
			}
		}
	}
	if s.HasNext() {
		return unsupported("extra public type bytes")
	}
	return nil
}

func admitPublicAggregateFieldWire(s *wasm.Reader, floating, vectors bool) error {
	typ, err := s.Byte()
	if err != nil {
		return err
	}
	valid := typ == 0x78 || typ == 0x77 || typ == 0x7f || typ == 0x7e || floating && (typ == 0x7d || typ == 0x7c) || vectors && typ == 0x7b || typ >= 0x6a && typ <= 0x73
	if typ == 0x63 || typ == 0x64 {
		heap, err := s.S33()
		if err != nil {
			return err
		}
		valid = heap >= 0 || heap >= -22 && heap <= -13
	}
	if !valid {
		return unsupported("public aggregate storage type")
	}
	mutable, err := s.Byte()
	if err != nil || mutable > 1 {
		return unsupported("public aggregate field mutability")
	}
	return nil
}
