//go:build !linux || tinygo

package jitprofile

import "time"

const Clock = "unix"

func Now() uint64 { return uint64(time.Now().UnixNano()) }

func threadID() uint32 { return 0 }
