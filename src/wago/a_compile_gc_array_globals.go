package wago

func (c *Compiled) gcArrayGlobalInit(globalIndex int) (gcArrayGlobalInit, bool) {
	if c == nil || c.memoryDir == nil {
		return gcArrayGlobalInit{}, false
	}
	for _, init := range c.memoryDir.gcArrayGlobals {
		if int(init.GlobalIndex) == globalIndex {
			return init, true
		}
	}
	return gcArrayGlobalInit{}, false
}
