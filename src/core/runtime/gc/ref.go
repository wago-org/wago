package gc

import raw "github.com/wago-org/wago/src/core/runtime/gc/native"

// Ref is an opaque Go reference. Objects carry their collector and generation;
// null and i31 immediates are portable between collectors. It is not a native ABI
// word. Use only the native subpackage inside trusted JIT adapters.
type Ref struct {
	owner      *Collector
	value      raw.Ref
	generation uint64
}

func Null() Ref               { return Ref{} }
func I31New(v int32) Ref      { return Ref{value: raw.I31New(v)} }
func (r Ref) IsNull() bool    { return r.value.IsNull() }
func (r Ref) IsI31() bool     { return r.value.IsI31() }
func (r Ref) IsObj() bool     { return r.value.IsObj() }
func (r Ref) I31GetS() int32  { return r.value.I31GetS() }
func (r Ref) I31GetU() uint32 { return r.value.I31GetU() }
func RefEq(a, b Ref) bool     { return a == b }
