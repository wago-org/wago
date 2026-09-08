//go:build arm64

package arm64

func testElem(kind elemKind) *elem {
	e := new(elem)
	e.setElemKind(kind)
	return e
}
