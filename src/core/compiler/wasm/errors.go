package wasm

import "fmt"

type DecodeErrorCode int

const (
	ErrIndexOutOfBounds DecodeErrorCode = iota
	ErrMalformedLEB
	ErrBadMagic
	ErrBadVersion
	ErrInvalidSection
	ErrSectionOrder
	ErrDuplicateSection
	ErrSectionSizeMismatch
	ErrInvalidType
	ErrInvalidLimits
	ErrInvalidImport
	ErrInvalidExport
	ErrInvalidInstruction
	ErrInvalidBlockType
	ErrInstructionNestingLimitExceeded
	ErrInvalidModule
)

type DecodeError struct {
	Code         DecodeErrorCode
	Offset       int
	SectionID    byte
	SectionStart int
	SectionEnd   int
	Cause        error
}

func (e *DecodeError) Error() string {
	if e.SectionEnd > 0 {
		return fmt.Sprintf("wasm decode: %v at offset %d in section %d [%d,%d)", e.Code, e.Offset, e.SectionID, e.SectionStart, e.SectionEnd)
	}
	return fmt.Sprintf("wasm decode: %v at offset %d", e.Code, e.Offset)
}

func (c DecodeErrorCode) String() string {
	switch c {
	case ErrIndexOutOfBounds:
		return "index out of bounds"
	case ErrMalformedLEB:
		return "malformed LEB128"
	case ErrBadMagic:
		return "bad magic"
	case ErrBadVersion:
		return "bad version"
	case ErrInvalidSection:
		return "invalid section"
	case ErrSectionOrder:
		return "section order"
	case ErrDuplicateSection:
		return "duplicate section"
	case ErrSectionSizeMismatch:
		return "section size mismatch"
	case ErrInvalidType:
		return "invalid type"
	case ErrInvalidLimits:
		return "invalid limits"
	case ErrInvalidImport:
		return "invalid import"
	case ErrInvalidExport:
		return "invalid export"
	case ErrInvalidInstruction:
		return "invalid instruction"
	case ErrInvalidBlockType:
		return "invalid block type"
	case ErrInstructionNestingLimitExceeded:
		return "instruction nesting limit exceeded"
	case ErrInvalidModule:
		return "invalid module"
	default:
		return fmt.Sprintf("decode error %d", int(c))
	}
}

type ValidationErrorCode int

const (
	ErrTypeMismatch ValidationErrorCode = iota
	ErrUnknownType
	ErrUnknownFunc
	ErrUnknownTable
	ErrUnknownMemory
	ErrUnknownGlobal
	ErrUnknownTag
	ErrUnknownLabel
	ErrUnknownLocal
	ErrImmutableGlobal
	ErrInvalidAlignment
	ErrInvalidSharedMemory
	ErrInvalidLimitRange
	ErrInvalidDataCount
	ErrConstExprRequired
	ErrDuplicateExport
	ErrUnsupportedValidationOpcode
	ErrUnsupportedFeature
)

type ValidationError struct {
	Code   ValidationErrorCode
	Func   int
	Detail string
}

func (e *ValidationError) Error() string {
	if e.Detail != "" {
		if e.Func >= 0 {
			return fmt.Sprintf("wasm validate: %v in function %d: %s", e.Code, e.Func, e.Detail)
		}
		return fmt.Sprintf("wasm validate: %v: %s", e.Code, e.Detail)
	}
	if e.Func >= 0 {
		return fmt.Sprintf("wasm validate: %v in function %d", e.Code, e.Func)
	}
	return fmt.Sprintf("wasm validate: %v", e.Code)
}

func (c ValidationErrorCode) String() string {
	switch c {
	case ErrTypeMismatch:
		return "type mismatch"
	case ErrUnknownType:
		return "unknown type"
	case ErrUnknownFunc:
		return "unknown function"
	case ErrUnknownTable:
		return "unknown table"
	case ErrUnknownMemory:
		return "unknown memory"
	case ErrUnknownGlobal:
		return "unknown global"
	case ErrUnknownTag:
		return "unknown tag"
	case ErrUnknownLabel:
		return "unknown label"
	case ErrUnknownLocal:
		return "unknown local"
	case ErrImmutableGlobal:
		return "immutable global"
	case ErrInvalidAlignment:
		return "invalid alignment"
	case ErrInvalidSharedMemory:
		return "shared memory requires a maximum"
	case ErrInvalidLimitRange:
		return "invalid limits"
	case ErrInvalidDataCount:
		return "invalid data count"
	case ErrConstExprRequired:
		return "constant expression required"
	case ErrDuplicateExport:
		return "duplicate export"
	case ErrUnsupportedValidationOpcode:
		return "unsupported validation opcode"
	case ErrUnsupportedFeature:
		return "unsupported feature"
	default:
		return fmt.Sprintf("validation error %d", int(c))
	}
}
