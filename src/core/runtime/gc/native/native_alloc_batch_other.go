//go:build !amd64 && !arm64

package gc

const (
	nativeStructHandleBatch     = 32
	nativeStructHandleBatchSize = nativeStructHandleBatch
)
