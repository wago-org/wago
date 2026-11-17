//go:build amd64

package amd64

func testValueElem(s *stack, st storage) *elem {
	e := s.alloc()
	e.setElemKind(ekValue)
	e.st = st
	return e
}

func testDeferredElem(s *stack, op wOp, typ machineType, arg0, arg1 *elem) *elem {
	e := s.alloc()
	e.arg0, e.arg1 = s.ref(arg0), s.ref(arg1)
	e.setElemKind(ekDeferred)
	e.setDeferredOp(op)
	e.setValueType(typ)
	return e
}

func (s *stack) testElem(value elem) *elem { e := s.alloc(); *e = value; return e }
