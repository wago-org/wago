package wasm

// skipExprOp skips exactly one already-prefixed instruction from r and returns
// its structural kind. It is allocation-free for vector immediates such as
// br_table labels and try_table catches.
func skipExprOp(r *reader) (directOpKind, error) {
	op, err := r.byte()
	if err != nil {
		return directInstr, err
	}
	return skipExprOpAfterOpcode(r, op)
}

func skipExprOpAfterOpcode(r *reader, op byte) (directOpKind, error) {
	if simpleOpcode[op] != InstrInvalid {
		return directInstr, nil
	}
	switch op {
	case 0x02, 0x03, 0x04:
		if _, err := decodeBlockType(r); err != nil {
			return directInstr, err
		}
		if op == 0x02 {
			return directBlock, nil
		}
		if op == 0x03 {
			return directLoop, nil
		}
		return directIf, nil
	case 0x05:
		return directElse, nil
	case 0x08, 0x0c, 0x0d, 0x10, 0x12, 0x14, 0x15, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
		_, err := r.u32()
		return directInstr, err
	case 0x0b:
		return directEnd, nil
	case 0x0e:
		n, err := r.u32()
		if err != nil {
			return directInstr, err
		}
		for i := uint32(0); i < n; i++ {
			if _, err := r.u32(); err != nil {
				return directInstr, err
			}
		}
		_, err = r.u32()
		return directInstr, err
	case 0x11, 0x13:
		if _, err := r.u32(); err != nil {
			return directInstr, err
		}
		_, err := r.u32()
		return directInstr, err
	case 0x1c:
		return directInstr, skipResultTypeBytes(r)
	case 0x1f:
		if _, err := decodeBlockType(r); err != nil {
			return directInstr, err
		}
		if err := skipCatchVecBytes(r); err != nil {
			return directInstr, err
		}
		return directTryTable, nil
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
		return directInstr, skipMemArgBytes(r)
	case 0x3f, 0x40:
		_, err := r.u32()
		return directInstr, err
	case 0x41:
		_, err := r.i32()
		return directInstr, err
	case 0x42:
		_, err := r.i64()
		return directInstr, err
	case 0x43:
		_, err := r.bytes(4)
		return directInstr, err
	case 0x44:
		_, err := r.bytes(8)
		return directInstr, err
	case 0xd0:
		return directInstr, skipRefHeapTypeBytes(r)
	case 0xd3, 0xd4:
		return directInstr, nil
	case 0xfb:
		return directInstr, skipFBBytes(r)
	case 0xfc:
		return directInstr, skipFCBytes(r)
	case 0xfd:
		return directInstr, skipFDBytes(r)
	case 0xfe:
		return directInstr, skipFEBytes(r)
	default:
		return directInstr, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
	}
}

func skipResultTypeBytes(r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		if err := skipValTypeBytes(r); err != nil {
			return err
		}
	}
	return nil
}

func skipValTypeBytes(r *reader) error {
	_, err := decodeValType(r)
	return err
}

func skipRefHeapTypeBytes(r *reader) error {
	_, _, err := decodeRefHeapType(r)
	return err
}

func skipMemArgBytes(r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	if n >= 64 && n < 128 {
		if _, err := r.u32(); err != nil {
			return err
		}
	} else if n >= 64 {
		return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
	_, err = r.u64()
	return err
}

func skipCatchVecBytes(r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		kind, err := r.byte()
		if err != nil {
			return err
		}
		switch CatchKind(kind) {
		case CatchTag, CatchRef:
			if _, err := r.u32(); err != nil {
				return err
			}
			if _, err := r.u32(); err != nil {
				return err
			}
		case CatchAll, CatchAllRef:
			if _, err := r.u32(); err != nil {
				return err
			}
		default:
			return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
		}
	}
	return nil
}

func skipFCBytes(r *reader) error {
	sub, err := r.u32()
	if err != nil {
		return err
	}
	if _, ok := fcNoImm[sub]; ok {
		return nil
	}
	switch sub {
	case 8, 10, 12, 14:
		if _, err := r.u32(); err != nil {
			return err
		}
		_, err := r.u32()
		return err
	case 9, 11, 13, 15, 16, 17:
		_, err := r.u32()
		return err
	default:
		return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
}

func skipFBBytes(r *reader) error {
	sub, err := r.u32()
	if err != nil {
		return err
	}
	if _, ok := fbNoImm[sub]; ok {
		return nil
	}
	switch sub {
	case 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7:
		return nil
	case 0, 1, 6, 7, 11, 12, 13, 14, 16, 32, 33, 34, 0x82:
		_, err := r.u32()
		return err
	case 2, 3, 4, 5, 8, 9, 10, 17, 18, 19:
		if _, err := r.u32(); err != nil {
			return err
		}
		_, err := r.u32()
		return err
	case 20, 21:
		return skipHeapTypeBytes(r)
	case 22, 23, 35, 36:
		return skipRefHeapTypeBytes(r)
	case 24, 25:
		if _, err := decodeCastOp(r); err != nil {
			return err
		}
		if _, err := r.u32(); err != nil {
			return err
		}
		if err := skipHeapTypeBytes(r); err != nil {
			return err
		}
		return skipHeapTypeBytes(r)
	default:
		return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
}

func skipHeapTypeBytes(r *reader) error {
	_, err := decodeHeapType(r)
	return err
}

func skipFDBytes(r *reader) error {
	sub, err := r.u32()
	if err != nil {
		return err
	}
	if sub == 12 || sub == 13 {
		_, err := r.bytes(16)
		if err != nil {
			return err
		}
		if sub == 13 {
			// decodeFD validates shuffle lanes. Keep the same structural decode check.
			start := r.pos - 16
			for i, b := range r.data[start:r.pos] {
				if b >= 32 {
					return &DecodeError{Code: ErrInvalidInstruction, Offset: start + i}
				}
			}
		}
		return nil
	}
	if _, ok := fdNoImm[sub]; ok {
		return nil
	}
	if _, ok := fdMem[sub]; ok {
		if err := skipMemArgBytes(r); err != nil {
			return err
		}
		if sub >= 84 && sub <= 91 {
			_, err := r.byte()
			return err
		}
		return nil
	}
	if _, ok := fdLane[sub]; ok {
		_, err := r.byte()
		return err
	}
	return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
}

func skipFEBytes(r *reader) error {
	sub, err := r.u32()
	if err != nil {
		return err
	}
	if sub == 0x03 {
		b, err := r.byte()
		if err != nil {
			return err
		}
		if b != 0 {
			return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
		}
		return nil
	}
	if sub >= 0x5c && sub <= 0x5e {
		if _, err := decodeAtomicOrder(r); err != nil {
			return err
		}
		if _, err := r.u32(); err != nil {
			return err
		}
		_, err := r.u32()
		return err
	}
	if _, ok := feMem[sub]; ok || sub >= 30 && sub <= 78 {
		return skipMemArgBytes(r)
	}
	return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
}
