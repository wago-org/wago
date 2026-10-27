package wago

func compiledMetadataUsesSIMD(c *Compiled) bool {
	if c == nil {
		return false
	}
	for _, sig := range c.importFuncSigs {
		if valTypesUseSIMD(sig.Params) || valTypesUseSIMD(sig.Results) {
			return true
		}
	}
	for _, sig := range c.Funcs {
		if valTypesUseSIMD(sig.Params) || valTypesUseSIMD(sig.Results) {
			return true
		}
	}
	for _, g := range c.GlobalImports {
		if g.Type == ValV128 {
			return true
		}
	}
	for _, g := range c.Globals {
		if g.Type == ValV128 {
			return true
		}
	}
	for _, typ := range c.Types {
		switch typ.Kind {
		case CompositeTypeStruct:
			for _, field := range typ.Fields {
				if !field.Storage.Packed && field.Storage.Value.Kind == ValueTypeV128 {
					return true
				}
			}
		case CompositeTypeArray:
			if !typ.Array.Storage.Packed && typ.Array.Storage.Value.Kind == ValueTypeV128 {
				return true
			}
		}
	}
	return false
}

func valTypesUseSIMD(ts []ValType) bool {
	for _, t := range ts {
		if t == ValV128 {
			return true
		}
	}
	return false
}
