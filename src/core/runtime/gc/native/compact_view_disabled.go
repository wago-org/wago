//go:build !wago_gc_compact_helpers && !(wago_gc_compact_reads || (wago_gc_compact_alloc || (wago_gc_compact_refstore || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || wago_gc_compact_fill)))))

package gc

// Empty and embedded before pointer fields: no default Collector footprint cost.
type compactViewState struct{}

func (c *Collector) compactReadSnapshot() CompactCollectorView { return CompactCollectorView{} }
func (c *Collector) currentCompactView() *CompactCollectorView { return nil }
func (c *Collector) refreshCompactView()                       {}
func (c *Collector) CompactView() *CompactCollectorView        { return nil }
