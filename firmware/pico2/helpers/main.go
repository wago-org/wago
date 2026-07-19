// Package main retains the four //export helper entries when TinyGo emits a
// relocatable object for Pico SDK firmware. The CMake integration localizes all
// other TinyGo symbols so the Pico SDK owns reset, IRQ, allocator, and runtime
// entry points.
package main

import _ "github.com/wago-org/wago/src/core/runtime/embedded32"

func main() {}
