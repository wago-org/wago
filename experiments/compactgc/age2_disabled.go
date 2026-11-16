//go:build !wago_gc_compact_age2

package compactgc

const age2Build = false

type age2State struct{}

func (c *Collector) Age2Enabled() bool              { return false }
func (c *Collector) freshStart() uint32             { return c.nurseryStart }
func (c *Collector) collectAge2([]*Ref, bool) error { panic("compactgc: age2 disabled") }
func NewAge2WithTypes(uint32, uint32, uint32, []Descriptor) (*Collector, error) {
	return nil, ErrConfig
}
func (c *Collector) YoungBudget() (total, active, cap uint32) {
	if c == nil {
		return 0, 0, 0
	}
	return c.nurseryEnd - c.nurseryStart, c.nurseryEnd - c.nurseryStart, 0
}
