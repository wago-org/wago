//go:build arm64

package arm64

// Mandatory patches must not discard a range failure. Keep the first error in
// the existing function state so hot patching needs no allocation or extra code
// bytes. Finalization rejects the function, including failures in late stubs.
// Speculative short-branch probes keep using the encoder directly: they check
// its Boolean result and can replace the instruction with their fallback.
func (f *fn) patchBranch19(site, target int) {
	if !f.a.PatchBranch19(site, target) {
		f.setRepresentationLimit(functionRepresentationBranchRange)
	}
}

func (f *fn) patchBranch26(site, target int) {
	if !f.a.PatchBranch26(site, target) {
		f.setRepresentationLimit(functionRepresentationBranchRange)
	}
}
