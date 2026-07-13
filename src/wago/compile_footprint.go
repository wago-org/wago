package wago

// CompileFootprint reports exact byte ranges retained by a Compiled product.
// It deliberately does not estimate pointer-rich Go metadata, allocator slack,
// or process RSS: those quantities are runtime/version dependent and should not
// be conflated with the product payload controlled by the compiler.
type CompileFootprint struct {
	// NativeCodeBytes is the used native-code length. NativeCodeBackingBytes is
	// the retained backing capacity, which can be larger because Railshot reserves
	// a bounded final-code tail while compiling.
	NativeCodeBytes        int
	NativeCodeBackingBytes int
	// ExecutableImageBytes is non-zero after SealCode has adopted the RX image.
	ExecutableImageBytes int

	// Active/PassiveDataBytes are the payload lengths retained for future
	// instantiations; their Backing variants include their actual Go-slice
	// capacities.
	ActiveDataBytes         int
	ActiveDataBackingBytes  int
	PassiveDataBytes        int
	PassiveDataBackingBytes int

	// LinkReplayBytes is the retained local-body payload for a module with
	// function imports. On Unix, LinkReplayMapped identifies the unlinked
	// file mapping used instead of Go-heap body copies.
	LinkReplayBytes  int
	LinkReplayMapped bool
}

// Footprint returns the product payload currently retained by c. It is safe to
// call before or after instantiation and does not allocate.
func (c *Compiled) Footprint() CompileFootprint {
	if c == nil {
		return CompileFootprint{}
	}
	f := CompileFootprint{NativeCodeBytes: c.codeLen(), NativeCodeBackingBytes: cap(c.Code)}
	for i := range c.Data {
		f.ActiveDataBytes += len(c.Data[i].Bytes)
		f.ActiveDataBackingBytes += cap(c.Data[i].Bytes)
	}
	for i := range c.PassiveData {
		f.PassiveDataBytes += len(c.PassiveData[i].Bytes)
		f.PassiveDataBackingBytes += cap(c.PassiveData[i].Bytes)
	}
	if c.hostLink != nil && c.hostLink.bodyStore != nil {
		f.LinkReplayBytes = len(c.hostLink.bodyStore.data)
		f.LinkReplayMapped = c.hostLink.bodyStore.isMapped()
	}
	if c.codeCache != nil {
		c.codeCache.mu.Lock()
		f.ExecutableImageBytes = c.codeCache.codeLen
		c.codeCache.mu.Unlock()
	}
	return f
}
