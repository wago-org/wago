//go:build windows && arm64 && !tinygo

package wago

import "testing"

func TestPreparedDirectARM64DisabledOnWindows(t *testing.T) {
	if preparedDirectIntSupported || preparedDirectIntPrivateSupported {
		t.Fatal("Windows ARM64 exposed the register-ABI prepared entry")
	}
}
