// Command diagnostic-dce keeps an ordinary embedding application's compile,
// instantiate, invoke, and teardown paths reachable for the symbol audit.
package main

import (
	"fmt"
	"os"

	"github.com/wago-org/wago"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--probe-profile" {
		session := wago.NewCodeProfile(wago.CodeProfileOptions{IncludeCode: true, SourceMaps: true, UnwindMaps: true, TraceBoundaries: true, TraceLifecycle: true})
		defer session.Close()
		token := session.BeginSpan(wago.CodeProfileSpan{Kind: "probe"})
		token.Finish("return")
		session.Retire(session.Register(wago.CodeProfileImage{}, nil))
		images, cursor, status := session.Snapshot()
		events, readStatus := session.Read(cursor)
		if !status.Closed || !readStatus.Closed || len(images) != 0 || len(events) != 0 || len(session.Spans()) != 0 || token.ID() != 0 || session.IncludeCode() || session.IncludeSources() || session.IncludeUnwind() || session.TraceBoundaries() || session.TraceLifecycle() {
			panic("ordinary build enabled profiling")
		}
		err := wago.NewRuntimeConfig().WithCodeProfile(session).Validate()
		if err == nil {
			panic("ordinary build accepted profiling configuration")
		}
		fmt.Println(err)
		return
	}

	if len(os.Args) != 3 {
		panic("usage: diagnostic-dce module.wasm export")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	compiled, err := wago.Compile(data)
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
