//go:build !riscv64

package wago

// The leading empty cache field preserves existing-target cache footprints.
type railshotHostThunkConstruction struct{}

func railshotInitHostThunkConstruction(*railshotHostThunkConstruction, *Compiled) {}
func railshotCertifiedHostThunk(_ *railshotHostThunkConstruction, _ int, dispatch uint32, params, results int, owned bool) ([]byte, error) {
	if owned {
		return railshotHostIndirectOwnedSyncThunk(dispatch, params, results), nil
	}
	return railshotHostIndirectSyncThunk(dispatch, params, results), nil
}
func railshotOwnedHostThunk(_ *railshotNativeImportConstruction, _ int, dispatch uint32, params, results int) ([]byte, error) {
	return railshotHostIndirectOwnedSyncThunk(dispatch, params, results), nil
}
func railshotRecordHostThunkSpan(*railshotHostThunkConstruction, int, int)    {}
func railshotFinishHostThunkConstruction(*railshotHostThunkConstruction, int) {}
