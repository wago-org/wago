//go:build amd64 && !tinygo

package wago

func (in *Instance) callPreparedDirectInt(entry uintptr, args []uint64, wideMask uint8, activeTrap []byte) (uint64, error) {
	var a0, a1, a2, a3 uint64
	switch len(args) {
	case 4:
		a3 = args[3]
		if wideMask&8 == 0 {
			a3 = uint64(uint32(a3))
		}
		fallthrough
	case 3:
		a2 = args[2]
		if wideMask&4 == 0 {
			a2 = uint64(uint32(a2))
		}
		fallthrough
	case 2:
		a1 = args[1]
		if wideMask&2 == 0 {
			a1 = uint64(uint32(a1))
		}
		fallthrough
	case 1:
		a0 = args[0]
		if wideMask&1 == 0 {
			a0 = uint64(uint32(a0))
		}
	}
	result, err := in.eng.CallPreparedInt(entry, in.jm.LinMemBase(), a0, a1, a2, a3, activeTrap)
	if err != nil {
		return 0, in.decorateTrap(err)
	}
	return result, nil
}
