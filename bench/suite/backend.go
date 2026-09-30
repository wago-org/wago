package wagobench

import "github.com/wago-org/wago/src/core/codeimage"

type benchCompiledModule struct {
	Code  []byte
	Entry []int
	image codeimage.Image
}

func (m *benchCompiledModule) Close() error {
	if m == nil {
		return nil
	}
	image := m.image
	m.Code, m.Entry, m.image = nil, nil, nil
	if image != nil {
		return image.Close()
	}
	return nil
}
