//go:build (darwin || windows) && (amd64 || arm64)

package runtime

func (e *Engine) prepareIdleStackForCache() bool { return true }
