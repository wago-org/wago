//go:build amd64

package amd64

func testValueElem(st storage) *elem {
	e := &elem{}
	e.setElemKind(ekValue)
	e.st = st
	return e
}

func testDeferredElem(op wOp, typ machineType, arg0, arg1 *elem) *elem {
	e := &elem{arg0: arg0, arg1: arg1}
	e.setElemKind(ekDeferred)
	e.setDeferredOp(op)
	e.setValueType(typ)
	return e
}
