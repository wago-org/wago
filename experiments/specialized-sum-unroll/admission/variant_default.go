//go:build amd64 && !wago_sumunroll

package main

func selectVariant(v string) {
	if v != "baseline" {
		panic("experimental variant needs wago_sumunroll")
	}
}
