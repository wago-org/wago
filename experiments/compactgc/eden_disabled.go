//go:build !wago_gc_compact_eden

package compactgc

type edenState struct{}

func (c *Collector) EdenEnabled() bool                    { return false }
func (c *Collector) edenYoung(Ref) bool                   { return false }
func (c *Collector) EdenBudget() (uint32, uint32, uint32) { return 0, 0, 0 }
func (c *Collector) collectEden([]*Ref, bool) error       { panic("compactgc: eden disabled") }
func NewEdenWithTypes(uint32, uint32, uint32, []Descriptor) (*Collector, error) {
	return nil, ErrConfig
}
