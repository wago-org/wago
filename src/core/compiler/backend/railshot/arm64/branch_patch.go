//go:build arm64

package arm64

func (f *fn) patchAdr(site, target int) {
	if !f.a.PatchAdr(site, target) {
		f.setRepresentationLimit(functionRepresentationADRRange)
	}
}
