//go:build !linux || tinygo || wago_target_tinygo

package gc

func (*Collector) notifyObjectReclaimed(Ref) {}
func (*Collector) notifyCollectorClosed()    {}
