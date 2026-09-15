//go:build tinygo

package main

//go:wasmimport tutorial answer
func answer() int32

//go:wasmexport run
func run() int32 { return answer() }

func main() {}
