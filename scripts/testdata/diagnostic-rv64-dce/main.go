//go:build linux && !tinygo

// This harness force-links experimental RV64 emission on either host ISA.
package main

import (
	"fmt"
	"os"

	rv "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func main() {
	data := []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 5, 1, 0x60, 0, 1, 0x7f, 3, 2, 1, 0, 10, 6, 1, 4, 0, 0x41, 3, 0x0b}
	c, err := rv.Compile(data)
	if err != nil {
		panic(err)
	}
	var a enc.Asm
	a.Constant64(enc.X9, int64(len(os.Args)))
	a.Addw(enc.X9, enc.X9, enc.X10)
	fmt.Println(c.Functions(), a.Len(), a.Err())
}
