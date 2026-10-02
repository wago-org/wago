//go:build !tinygo || windows || (darwin && amd64)

package runtime

type tinygoPreparedIntState struct{}

func (*tinygoPreparedIntState) close() error { return nil }
