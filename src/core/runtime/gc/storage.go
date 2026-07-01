package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (c *Collector) loadValue(r Ref, off uint64, k StorageKind) (Value, error) {
	b := c.bytes(r)
	_, size, err := storageLayout(k)
	if err != nil {
		return Value{}, err
	}
	if off > uint64(len(b)) || uint64(len(b))-off < uint64(size) {
		return Value{}, errors.New("gc: load out of bounds")
	}
	switch k {
	case StorageI8:
		return Value{Kind: k, Bits: uint64(b[off])}, nil
	case StorageI16:
		return Value{Kind: k, Bits: uint64(binary.LittleEndian.Uint16(b[off:]))}, nil
	case StorageI32, StorageF32:
		return Value{Kind: k, Bits: uint64(binary.LittleEndian.Uint32(b[off:]))}, nil
	case StorageI64, StorageF64:
		return Value{Kind: k, Bits: binary.LittleEndian.Uint64(b[off:])}, nil
	case StorageRef, StorageRefNull:
		return Value{Kind: k, Ref: Ref(binary.LittleEndian.Uint32(b[off:]))}, nil
	}
	return Value{}, errors.New("gc: bad kind")
}
func (c *Collector) storeValue(r Ref, d TypeDesc, off uint64, k StorageKind, v Value) error {
	_ = d
	b := c.bytes(r)
	_, size, err := storageLayout(k)
	if err != nil {
		return err
	}
	if off > uint64(len(b)) || uint64(len(b))-off < uint64(size) {
		return errors.New("gc: store out of bounds")
	}
	if err := checkValueCompatible(k, v); err != nil {
		return err
	}
	switch k {
	case StorageI8:
		b[off] = byte(v.Bits)
	case StorageI16:
		binary.LittleEndian.PutUint16(b[off:], uint16(v.Bits))
	case StorageI32, StorageF32:
		binary.LittleEndian.PutUint32(b[off:], uint32(v.Bits))
	case StorageI64, StorageF64:
		binary.LittleEndian.PutUint64(b[off:], v.Bits)
	case StorageRef, StorageRefNull:
		binary.LittleEndian.PutUint32(b[off:], uint32(v.Ref))
	default:
		return errors.New("gc: bad kind")
	}
	return nil
}

func checkDefaultable(d TypeDesc) error {
	switch d.Kind {
	case KindStruct:
		for i, f := range d.Fields {
			if f.Kind == StorageRef {
				return fmt.Errorf("gc: struct type %d field %d is non-null ref and not defaultable", d.ID, i)
			}
		}
	case KindArray:
		if d.Elem == StorageRef {
			return fmt.Errorf("gc: array type %d element is non-null ref and not defaultable", d.ID)
		}
	}
	return nil
}

func checkValueCompatible(k StorageKind, v Value) error {
	switch k {
	case StorageI8, StorageI16:
		if v.Kind != StorageI32 && v.Kind != k {
			return fmt.Errorf("gc: value kind %d incompatible with packed storage %d", v.Kind, k)
		}
	case StorageI32, StorageI64, StorageF32, StorageF64:
		if v.Kind != k {
			return fmt.Errorf("gc: value kind %d incompatible with storage %d", v.Kind, k)
		}
	case StorageRef:
		if !isRefKind(v.Kind) {
			return fmt.Errorf("gc: value kind %d incompatible with non-null ref storage", v.Kind)
		}
		if v.Ref.IsNull() {
			return errors.New("gc: cannot store null in non-null ref slot")
		}
	case StorageRefNull:
		if !isRefKind(v.Kind) {
			return fmt.Errorf("gc: value kind %d incompatible with nullable ref storage", v.Kind)
		}
	default:
		return errors.New("gc: bad kind")
	}
	return nil
}
