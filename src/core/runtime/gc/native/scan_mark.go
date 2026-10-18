package gc

import (
	"encoding/binary"
	"runtime"
)

// scanObjectMark uses a direct marking call for each reference.
func (c *Collector) scanObjectMark(h uint32) {
	// Keep reference order and filtering in step with scanObjectRefs.
	// A separate loop lets the compiler inline marking without a callback.
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs {
		return
	}
	hdr := c.header(r)
	b := c.bytes(r)
	if d.Kind == KindStruct {
		if runtime.GOARCH == "amd64" && d.firstRef != 0 {
			for index := uint32(d.firstRef); index != 0; {
				field := d.Fields[index-1]
				c.markRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:])))
				index = uint32(field.nextRef)
			}
			return
		}
		for _, f := range d.Fields {
			if isCollectorRefKind(f.Kind) {
				c.markRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
			}
		}
	} else if d.ArrayElementsAreRefs() {
		for i := uint32(0); i < hdr.Aux; i++ {
			off := PayloadOffset + i*d.ElemSize
			c.markRef(Ref(binary.LittleEndian.Uint32(b[off:])))
		}
	}
}

// scanObjectNursery uses a direct marking call for each reference.
func (c *Collector) scanObjectNursery(h uint32) {
	// Keep reference order and filtering in step with scanObjectRefs.
	// A separate loop lets the compiler inline marking without a callback.
	r := makeObjRef(h)
	d, err := c.refDesc(r)
	if err != nil || !d.HasRefs {
		return
	}
	hdr := c.header(r)
	b := c.bytes(r)
	if d.Kind == KindStruct {
		if runtime.GOARCH == "amd64" && d.firstRef != 0 {
			for index := uint32(d.firstRef); index != 0; {
				field := d.Fields[index-1]
				c.markNurseryRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+field.Offset:])))
				index = uint32(field.nextRef)
			}
			return
		}
		for _, f := range d.Fields {
			if isCollectorRefKind(f.Kind) {
				c.markNurseryRef(Ref(binary.LittleEndian.Uint32(b[PayloadOffset+f.Offset:])))
			}
		}
	} else if d.ArrayElementsAreRefs() {
		for i := uint32(0); i < hdr.Aux; i++ {
			off := PayloadOffset + i*d.ElemSize
			c.markNurseryRef(Ref(binary.LittleEndian.Uint32(b[off:])))
		}
	}
}
