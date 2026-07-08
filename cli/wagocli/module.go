package wagocli

import (
	"fmt"
	"os"

	"github.com/wago-org/wago"
)

// runtimeWithAllPlugins builds a runtime with every compiled-in plugin registered,
// so a module's imports resolve to their providing plugin's signatures and caps.
func runtimeWithAllPlugins() *wago.Runtime {
	rt := wago.NewRuntime()
	for _, name := range wago.RegisteredPluginNames() {
		_ = rt.UsePlugin(name)
	}
	return rt
}

func compileForInspect(rt *wago.Runtime, file string) *wago.Module {
	src, err := os.ReadFile(file)
	if err != nil {
		fatal("%v", err)
	}
	mod, err := rt.Compile(src)
	if err != nil {
		fatal("compile: %v", err)
	}
	return mod
}

func moduleImports(file string) {
	mod := compileForInspect(runtimeWithAllPlugins(), file)
	imports := mod.Imports()
	if len(imports) == 0 {
		fmt.Println(dim("module has no imports"))
		return
	}
	fmt.Printf("%s\n", bold("imports:"))
	for _, s := range imports {
		mark := red("unresolved")
		if s.Provided {
			mark = cyan("provided")
		}
		line := fmt.Sprintf("  %s  %s  %s", s.Key(), dim(s.Kind.String()), mark)
		if s.Kind == wago.ImportFunc && (len(s.Params) > 0 || len(s.Results) > 0) {
			line += "  " + dim(sigString(s.Params, s.Results))
		}
		if s.HasCapability {
			line += "  " + dim("["+string(s.Capability)+"]")
		}
		fmt.Println(line)
	}
}

func moduleCapabilities(file string) {
	mod := compileForInspect(runtimeWithAllPlugins(), file)
	caps := mod.RequiredCapabilities()
	if len(caps) == 0 {
		fmt.Println(dim("module requires no capabilities"))
		return
	}
	fmt.Printf("%s\n", bold("required capabilities:"))
	for _, c := range caps {
		fmt.Printf("  %s\n", string(c))
	}
}
