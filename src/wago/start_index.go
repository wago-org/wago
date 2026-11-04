package wago

import "fmt"

// Optional start metadata uses the Wasm function index space. The requirement
// bit makes old readers reject the extension rather than omit initialization.
// Non-start artifacts retain their existing byte format.
func compiledStartIndex(c *Compiled) (uint32, error) {
	if c.NumImports < 0 {
		return 0, fmt.Errorf("start function has a negative import count")
	}
	var sig FuncSig
	var index uint64
	if c.StartIsImport {
		if c.StartImportIdx < 0 || c.StartImportIdx >= c.NumImports || c.StartImportIdx >= len(c.importFuncSigs) {
			return 0, fmt.Errorf("start function requires a bounded imported signature")
		}
		sig = c.importFuncSigs[c.StartImportIdx]
		index = uint64(c.StartImportIdx)
	} else {
		if c.StartLocalFunc < 0 || c.StartLocalFunc >= len(c.Funcs) || c.StartLocalFunc >= len(c.Entry) {
			return 0, fmt.Errorf("start function requires a bounded defined signature and entry")
		}
		sig = c.Funcs[c.StartLocalFunc]
		index = uint64(c.NumImports) + uint64(c.StartLocalFunc)
	}
	if index > uint64(^uint32(0)) || len(sig.Params) != 0 || len(sig.Results) != 0 {
		return 0, fmt.Errorf("start function requires a void signature and uint32 index")
	}
	return uint32(index), nil
}
