// Command precompiled-dce keeps artifact loading and execution reachable without
// checking that explicit source-compilation requests remain unavailable stubs.
package main

import (
	"fmt"
	"os"

	"github.com/wago-org/wago"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: precompiled-dce module.wago export")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	if os.Args[2] == "--probe-compile" {
		compiled, err := wago.Compile(data)
		if compiled != nil || err == nil {
			panic("precompiled build accepted source compilation")
		}
		fmt.Println(err)
		return
	}
	compiled, err := wago.LoadTrustedArtifact(data)
	if err != nil {
		panic(err)
	}
	defer compiled.Close()
	instance, err := wago.Instantiate(compiled)
	if err != nil {
		panic(err)
	}
	defer instance.Close()
	result, err := instance.Invoke(os.Args[2])
	if err != nil {
		panic(err)
	}
	fmt.Println(result)
}
