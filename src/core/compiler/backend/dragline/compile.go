// Package dragline contains Wago's optimizing sibling compiler engine.
package dragline

import (
	corecompiler "github.com/wago-org/wago/src/core/compiler"
)

// UnavailableError is returned while the independent Dragline pipeline is
// empty. It is deliberately strict: selecting Dragline never invokes Railshot.
type UnavailableError struct{}

func (*UnavailableError) Error() string { return "dragline compiler is not implemented" }

// Compiler is the Dragline backend. Its pipeline will be filled in behind this
// boundary without importing or consuming Railshot internals.
type Compiler struct{}

func (Compiler) Compile(corecompiler.Input) (corecompiler.Output, error) {
	return corecompiler.Output{}, &UnavailableError{}
}
