//go:build !wago_profile

package wago

// Keep even explicit uses of the unavailable public API independent of the
// journal implementation. RuntimeConfig.Validate reports the missing build tag;
// these compatibility stubs cannot allocate buffers, read clocks, or record.
type codeProfileSession struct{}
type codeProfileToken struct{}

func newCodeProfile(CodeProfileOptions) *CodeProfile { return &CodeProfile{} }

func (*codeProfileSession) IncludeCode() bool                        { return false }
func (*codeProfileSession) IncludeSources() bool                     { return false }
func (*codeProfileSession) IncludeUnwind() bool                      { return false }
func (*codeProfileSession) TraceBoundaries() bool                    { return false }
func (*codeProfileSession) TraceLifecycle() bool                     { return false }
func (*codeProfileSession) Register(CodeProfileImage, []byte) uint64 { return 0 }
func (*codeProfileSession) Retire(uint64)                            {}
func (*codeProfileSession) Close()                                   {}
func (*codeProfileSession) Status() CodeProfileStatus {
	return CodeProfileStatus{Clock: "unavailable", Closed: true}
}
func (s *codeProfileSession) Snapshot() ([]CodeProfileImage, uint64, CodeProfileStatus) {
	return nil, 0, s.Status()
}
func (s *codeProfileSession) Read(uint64) ([]CodeProfileEvent, CodeProfileStatus) {
	return nil, s.Status()
}
func (*codeProfileSession) Spans() []CodeProfileSpan { return nil }
func (*codeProfileSession) BeginSpan(CodeProfileSpan) CodeProfileSpanToken {
	return CodeProfileSpanToken{}
}
func (codeProfileToken) ID() uint64    { return 0 }
func (codeProfileToken) Finish(string) {}
