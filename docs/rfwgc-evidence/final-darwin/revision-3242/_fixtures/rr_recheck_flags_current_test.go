//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package wago

func rrConversionOnly(entry *referenceStoreInstance) bool {
	return entry.conversionOnlyInvocationDomains
}
