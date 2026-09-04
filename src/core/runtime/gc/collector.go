package gc

import (
	"errors"
	raw "github.com/wago-org/wago/src/core/runtime/gc/native"
)

var ErrInvalidReference = errors.New("gc: foreign, stale, or invalid reference")
var ErrCollectorClosed = errors.New("gc: collector closed")

// Collector owns a checked Go heap. It never exposes the raw native collector.
type Collector struct {
	heap   *raw.Collector
	closed bool
}

func NewCollector(config Config, types []TypeDesc) (*Collector, error) {
	heap, err := raw.NewCollector(config, copyTypeDescs(types))
	if err != nil {
		return nil, err
	}
	if err := heap.EnableCheckedHandles(); err != nil {
		heap.Close()
		return nil, err
	}
	return &Collector{heap: heap}, nil
}
func (c *Collector) available() error {
	if c == nil || c.closed || c.heap == nil {
		return ErrCollectorClosed
	}
	return nil
}
func (c *Collector) Close() {
	if c != nil && !c.closed {
		c.heap.Close()
		c.closed = true
	}
}
func (c *Collector) unwrap(ref Ref) (raw.Ref, error) {
	if err := c.available(); err != nil {
		return 0, err
	}
	if !ref.IsObj() {
		if ref.owner != nil || ref.generation != 0 {
			return 0, ErrInvalidReference
		}
		return ref.value, nil
	}
	if ref.owner != c {
		return 0, ErrInvalidReference
	}
	generation, err := c.heap.CheckedIdentity(ref.value)
	if err != nil || generation != ref.generation {
		return 0, ErrInvalidReference
	}
	return ref.value, nil
}
func (c *Collector) wrap(ref raw.Ref, err error) (Ref, error) {
	if err != nil {
		return Ref{}, err
	}
	if !ref.IsObj() {
		return Ref{value: ref}, nil
	}
	generation, err := c.heap.CheckedIdentity(ref)
	if err != nil {
		return Ref{}, err
	}
	return Ref{owner: c, value: ref, generation: generation}, nil
}
func (c *Collector) input(value Value) (raw.Value, error) {
	out := raw.Value{Bits: value.Bits, BitsHi: value.BitsHi, Kind: value.Kind}
	if value.Kind == StorageRef || value.Kind == StorageRefNull {
		ref, err := c.unwrap(value.Ref)
		if err != nil {
			return out, err
		}
		out.Ref = ref
	}
	return out, nil
}
func (c *Collector) inputs(values []Value) ([]raw.Value, error) {
	if err := c.available(); err != nil {
		return nil, err
	}
	out := make([]raw.Value, len(values))
	for i, value := range values {
		v, err := c.input(value)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}
func (c *Collector) output(value raw.Value, err error) (Value, error) {
	if err != nil {
		return Value{}, err
	}
	out := Value{Bits: value.Bits, BitsHi: value.BitsHi, Kind: value.Kind}
	if value.Kind == StorageRef || value.Kind == StorageRefNull {
		out.Ref, err = c.wrap(value.Ref, nil)
	}
	return out, err
}
func (c *Collector) Profile() Profile {
	if c == nil {
		return ProfileThroughput
	}
	return c.heap.Profile()
}
func (c *Collector) Stats() Stats { return c.heap.Stats() }
func (c *Collector) AddTypes(types []TypeDesc) error {
	if err := c.available(); err != nil {
		return err
	}
	return c.heap.AddTypes(copyTypeDescs(types))
}
func (c *Collector) CollectFull(roots RootSet) error {
	if err := c.available(); err != nil {
		return err
	}
	r, err := c.roots(roots)
	if err != nil {
		return err
	}
	return c.heap.CollectFull(r)
}
func (c *Collector) CollectMinor(roots RootSet) error {
	if err := c.available(); err != nil {
		return err
	}
	r, err := c.roots(roots)
	if err != nil {
		return err
	}
	return c.heap.CollectMinor(r)
}
func (c *Collector) Step(roots RootSet) error {
	if err := c.available(); err != nil {
		return err
	}
	r, err := c.roots(roots)
	if err != nil {
		return err
	}
	return c.heap.Step(r)
}
func (c *Collector) Verify(roots RootSet) error {
	if err := c.available(); err != nil {
		return err
	}
	r, err := c.roots(roots)
	if err != nil {
		return err
	}
	return c.heap.Verify(r)
}

func copyTypeDescs(types []TypeDesc) []TypeDesc {
	out := append([]TypeDesc(nil), types...)
	for i := range out {
		out[i].Fields = append([]FieldDesc(nil), out[i].Fields...)
	}
	return out
}
