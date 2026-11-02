//go:build !(linux || (darwin && arm64)) || tinygo || wago_target_tinygo

package gc

func (*Collector) notifyObjectReclaimed(Ref) {}
func (*Collector) notifyCollectorClosed()    {}
