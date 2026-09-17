//go:build !linux

package hwcounters

type Group struct{}

func Open() (*Group, error)           { return nil, ErrUnsupported }
func (*Group) Start() error           { return ErrUnsupported }
func (*Group) Stop() ([]Count, error) { return nil, ErrUnsupported }
func (*Group) Close() error           { return nil }
