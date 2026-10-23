//go:build !linux || (!amd64 && !arm64) || tinygo

package wago

import "github.com/wago-org/wago/src/core/runtime"

func (c *Compiled) experimentalCOWImageMemory(_, _ int) (*runtime.JobMemory, bool, error) {
	return nil, false, nil
}
