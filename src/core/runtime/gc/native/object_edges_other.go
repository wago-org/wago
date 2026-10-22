//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package gc

type objectEdgeSourceDisabled struct{}

func (*objectEdgeSourceDisabled) ObjectEdgeCount(Ref) uint32   { return 0 }
func (*objectEdgeSourceDisabled) ObjectEdgeAt(Ref, uint32) Ref { return Null() }

func (*Collector) objectEdgeSource() *objectEdgeSourceDisabled  { return nil }
func (*Collector) objectEdgeCount(Ref) uint32                   { return 0 }
func (*Collector) persistentRootIsStrong(SlotKind, uint32) bool { return true }
func (*Collector) objectEdgesMutated(Ref)                       {}
func (*Collector) objectRootPublished(Ref)                      {}
func (*Collector) closeObjectEdgeSource()                       {}
