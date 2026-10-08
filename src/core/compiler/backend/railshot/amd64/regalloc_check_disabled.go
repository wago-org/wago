//go:build amd64 && !wago_regalloccheck

package amd64

// Guard each hook with this constant so ordinary builds erase argument setup too.
const regallocCheckEnabled = false

// First in fn so the empty ordinary-build state does not change its layout.
type regallocFnState struct{}

func (*fn) checkBeginFlush([]*elem) bool         { return false }
func (*fn) checkEndFlush()                       {}
func (*fn) checkUse(*elem)                       {}
func (*fn) checkOccupy(*elem, Reg, bool)         {}
func (*fn) checkBeginSlots(int, int, int) func() { return nil }
func (*fn) checkImmutable(Reg, bool, int)        {}
func (*fn) checkReleaseImmutable(Reg, bool)      {}
func (*fn) checkCallClobber()                    {}
func checkSize(machineType) int                  { return 0 }

func (*fn) checkBeginRegMoves([]regMove, bool) func() { return nil }

func (*fn) checkInputs(*elem)    {}
func (*fn) checkFoldedUse(*elem) {}

func (*fn) checkEndLifetimes() {}

// Ordinary trap scopes must not capture a diagnostic mask in a defer.
type regallocWriteMask struct{}

func (*fn) checkTerminalWrites() regallocWriteMask { return regallocWriteMask{} }
func (*fn) checkRestoreWrites(regallocWriteMask)   {}
