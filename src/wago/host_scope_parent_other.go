//go:build !riscv64

package wago

import "context"

type railshotHostScopeParent struct{}

func (*hostCallScope) invocationParent(fallback context.Context) context.Context { return fallback }

func (*hostCallScope) releaseInvocationParent() {}
