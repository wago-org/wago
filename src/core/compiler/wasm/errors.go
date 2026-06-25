// Package wasm decodes and validates WebAssembly binary modules.
package wasm

import "fmt"

// ErrCode classifies a decode or validation failure.
type ErrCode int

const (
	ErrBytecodeOutOfRange ErrCode = iota
	ErrMalformedLEBOutOfBounds
	ErrMalformedLEBSignedPadding
	ErrMalformedLEBUnsignedPadding
	ErrBadMagic
	ErrBadVersion
	ErrUnknownSectionID
	ErrSectionSizeMismatch
	ErrInvalidValType
	ErrUnknownImportKind
	ErrUnknownExportKind
	ErrBadConstExpr
	ErrFuncCodeCountMismatch
	ErrBadTypeForm
	ErrBadLimits
	ErrBadMutability
	ErrBadElementFlags
	ErrBadDataFlags
	ErrBadElemKind

	ErrTypeMismatch
	ErrUnknownLocal
	ErrUnknownGlobal
	ErrImmutableGlobal
	ErrUnknownFunc
	ErrUnknownType
	ErrUnknownTable
	ErrUnknownMemory
	ErrUnknownLabel
	ErrInvalidAlignment
	ErrInvalidBlockType
	ErrUnsupportedOpcode
	ErrInvalidResultArity
	ErrConstExprRequired
	ErrUnknownExport
)

var errMessages = map[ErrCode]string{
	ErrBytecodeOutOfRange:          "bytecode out of range",
	ErrMalformedLEBOutOfBounds:     "malformed LEB128 integer (out of bounds)",
	ErrMalformedLEBSignedPadding:   "malformed signed LEB128 integer (wrong padding)",
	ErrMalformedLEBUnsignedPadding: "malformed unsigned LEB128 integer (wrong padding)",
	ErrBadMagic:                    "bad magic (not a wasm module)",
	ErrBadVersion:                  "unsupported wasm version",
	ErrUnknownSectionID:            "unknown section id",
	ErrSectionSizeMismatch:         "section size mismatch",
	ErrInvalidValType:              "invalid value type",
	ErrUnknownImportKind:           "unknown import kind",
	ErrUnknownExportKind:           "unknown export kind",
	ErrBadConstExpr:                "malformed constant expression",
	ErrFuncCodeCountMismatch:       "function and code section count mismatch",
	ErrBadTypeForm:                 "bad function type form (expected 0x60)",
	ErrBadLimits:                   "bad limits flag",
	ErrBadMutability:               "bad global mutability flag",
	ErrBadElementFlags:             "bad element segment flags",
	ErrBadDataFlags:                "bad data segment flags",
	ErrBadElemKind:                 "bad element kind",
	ErrTypeMismatch:                "type mismatch",
	ErrUnknownLocal:                "unknown local",
	ErrUnknownGlobal:               "unknown global",
	ErrImmutableGlobal:             "global is immutable",
	ErrUnknownFunc:                 "unknown function",
	ErrUnknownType:                 "unknown type",
	ErrUnknownTable:                "unknown table",
	ErrUnknownMemory:               "unknown memory",
	ErrUnknownLabel:                "unknown label",
	ErrInvalidAlignment:            "alignment must not be larger than natural",
	ErrInvalidBlockType:            "invalid block type",
	ErrUnsupportedOpcode:           "unsupported opcode",
	ErrInvalidResultArity:          "invalid result arity",
	ErrConstExprRequired:           "constant expression required",
	ErrUnknownExport:               "unknown export index",
}

func (c ErrCode) String() string {
	if m, ok := errMessages[c]; ok {
		return m
	}
	return fmt.Sprintf("decode error %d", int(c))
}

type DecodeError struct {
	Code   ErrCode
	Offset int
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("wasm decode: %s at offset %d", e.Code, e.Offset)
}

type ValidationError struct {
	Code ErrCode
	Func int // function index (-1 for module-level)
}

func (e *ValidationError) Error() string {
	if e.Func >= 0 {
		return fmt.Sprintf("wasm validate: %s in function %d", e.Code, e.Func)
	}
	return fmt.Sprintf("wasm validate: %s", e.Code)
}
