package wasm

// FuncName returns the first name-section function name for a global function
// index. Empty names are reported as present.
func (n *NameSec) FuncName(idx uint32) (string, bool) {
	if n == nil {
		return "", false
	}
	for i := range n.FunctionNames {
		if n.FunctionNames[i].Index == idx {
			return n.FunctionNames[i].Name, true
		}
	}
	return "", false
}

// LocalName returns the first name-section local name for localIdx in global
// function index funcIdx. Empty names are reported as present.
func (n *NameSec) LocalName(funcIdx, localIdx uint32) (string, bool) {
	if n == nil {
		return "", false
	}
	for i := range n.LocalNames {
		if n.LocalNames[i].Index != funcIdx {
			continue
		}
		for j := range n.LocalNames[i].Names {
			if n.LocalNames[i].Names[j].Index == localIdx {
				return n.LocalNames[i].Names[j].Name, true
			}
		}
	}
	return "", false
}
