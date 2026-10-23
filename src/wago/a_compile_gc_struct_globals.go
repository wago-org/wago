package wago

func (c *Compiled) gcStructGlobalInit(globalIndex int) (gcStructGlobalInit, bool) {
	if c == nil || c.memoryDir == nil {
		return gcStructGlobalInit{}, false
	}
	for _, init := range c.memoryDir.gcStructGlobals {
		if int(init.GlobalIndex) == globalIndex {
			return init, true
		}
	}
	return gcStructGlobalInit{}, false
}
