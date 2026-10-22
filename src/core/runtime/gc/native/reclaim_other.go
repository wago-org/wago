//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package gc

func (*Collector) notifyObjectReclaimed(Ref) {}
func (*Collector) notifyCollectorClosed()    {}
