// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package arm64

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Raw encoder Boolean widths differ from compiler Boolean widths. Require the
// explicit 64-bit form in indirect call address construction on every host.
func TestIndirectCallTableShiftWidth(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "call.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || (fn.Name.Name != "callIndirect" && fn.Name.Name != "returnCallIndirect") {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "LslImm" {
				t.Errorf("%s: indirect table address must use an explicit 64-bit shift", fset.Position(call.Pos()))
			}
			return true
		})
	}
}
