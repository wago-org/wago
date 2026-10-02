package wasm

import "testing"

func descriptorTestStruct(final bool, supers []TypeIdx, describes, descriptor OptionalTypeIdx) SubType {
	return SubType{
		Final:  final,
		Supers: supers,
		Metadata: TypeMetadata{
			Describes:  describes,
			Descriptor: descriptor,
		},
		Comp: CompType{Kind: CompStruct},
	}
}

func descriptorTestPair(final bool) RecType {
	return RecType{SubTypes: []SubType{
		descriptorTestStruct(final, nil, OptionalTypeIdx{}, SomeTypeIdx(TypeIdx{Index: 1, Rec: true})),
		descriptorTestStruct(final, nil, SomeTypeIdx(TypeIdx{Index: 0, Rec: true}), OptionalTypeIdx{}),
	}}
}

func TestValidateDescriptorMetadataInvariants(t *testing.T) {
	rec := func(index uint32) TypeIdx { return TypeIdx{Index: index, Rec: true} }
	abs := func(index uint32) TypeIdx { return TypeIdx{Index: index} }
	some := func(index TypeIdx) OptionalTypeIdx { return SomeTypeIdx(index) }

	t.Run("valid descriptor pair and complete subtype square", func(t *testing.T) {
		m := &Module{Types: []RecType{
			descriptorTestPair(false),
			{SubTypes: []SubType{
				descriptorTestStruct(true, []TypeIdx{abs(0)}, OptionalTypeIdx{}, some(rec(1))),
				descriptorTestStruct(true, []TypeIdx{abs(1)}, some(rec(0)), OptionalTypeIdx{}),
			}},
		}}
		if err := ValidateModule(m); err != nil {
			t.Fatalf("ValidateModule: %v", err)
		}
	})
	t.Run("descriptor pair finality matches", func(t *testing.T) {
		for _, final := range []bool{false, true} {
			m := &Module{Types: []RecType{descriptorTestPair(final)}}
			if err := ValidateModule(m); err != nil {
				t.Fatalf("final=%v: %v", final, err)
			}
		}
	})
	t.Run("descriptor pair finality mismatch", func(t *testing.T) {
		for _, finals := range [][2]bool{{true, false}, {false, true}} {
			m := &Module{Types: []RecType{{SubTypes: []SubType{
				descriptorTestStruct(finals[0], nil, OptionalTypeIdx{}, some(rec(1))),
				descriptorTestStruct(finals[1], nil, some(rec(0)), OptionalTypeIdx{}),
			}}}}
			expectValidateErr(t, m, ErrTypeMismatch)
		}
	})

	tests := []struct {
		name string
		m    *Module
	}{
		{
			name: "descriptor requires reciprocal describes",
			m: &Module{Types: []RecType{{SubTypes: []SubType{
				descriptorTestStruct(true, nil, OptionalTypeIdx{}, some(rec(1))),
				descriptorTestStruct(true, nil, OptionalTypeIdx{}, OptionalTypeIdx{}),
			}}}},
		},
		{
			name: "describes requires reciprocal descriptor",
			m: &Module{Types: []RecType{{SubTypes: []SubType{
				descriptorTestStruct(true, nil, OptionalTypeIdx{}, OptionalTypeIdx{}),
				descriptorTestStruct(true, nil, some(rec(0)), OptionalTypeIdx{}),
			}}}},
		},
		{
			name: "descriptor must stay in recursion group",
			m: &Module{Types: []RecType{
				{SubTypes: []SubType{descriptorTestStruct(true, nil, OptionalTypeIdx{}, OptionalTypeIdx{})}},
				{SubTypes: []SubType{descriptorTestStruct(true, nil, OptionalTypeIdx{}, some(abs(0)))}},
			}},
		},
		{
			name: "describes must stay in recursion group",
			m: &Module{Types: []RecType{
				{SubTypes: []SubType{descriptorTestStruct(true, nil, OptionalTypeIdx{}, OptionalTypeIdx{})}},
				{SubTypes: []SubType{descriptorTestStruct(true, nil, some(abs(0)), OptionalTypeIdx{})}},
			}},
		},
		{
			name: "describes must name an earlier member",
			m: &Module{Types: []RecType{{SubTypes: []SubType{
				descriptorTestStruct(true, nil, some(rec(1)), OptionalTypeIdx{}),
				descriptorTestStruct(true, nil, OptionalTypeIdx{}, some(rec(0))),
			}}}},
		},
		{
			name: "descriptor metadata is struct-only",
			m: &Module{Types: []RecType{{SubTypes: []SubType{
				{Final: true, Metadata: TypeMetadata{Descriptor: some(rec(1))}, Comp: CompType{Kind: CompFunc}},
				descriptorTestStruct(true, nil, some(rec(0)), OptionalTypeIdx{}),
			}}}},
		},
		{
			name: "describes metadata is struct-only",
			m: &Module{Types: []RecType{{SubTypes: []SubType{
				descriptorTestStruct(true, nil, OptionalTypeIdx{}, some(rec(1))),
				{Final: true, Metadata: TypeMetadata{Describes: some(rec(0))}, Comp: CompType{Kind: CompArray, Array: field(I32, Var)}},
			}}}},
		},
		{
			name: "described subtype cannot omit descriptor",
			m: &Module{Types: []RecType{
				descriptorTestPair(false),
				{SubTypes: []SubType{descriptorTestStruct(true, []TypeIdx{abs(0)}, OptionalTypeIdx{}, OptionalTypeIdx{})}},
			}},
		},
		{
			name: "described subtype cannot introduce descriptor",
			m: &Module{Types: []RecType{
				{SubTypes: []SubType{descriptorTestStruct(false, nil, OptionalTypeIdx{}, OptionalTypeIdx{})}},
				{SubTypes: []SubType{
					descriptorTestStruct(true, []TypeIdx{abs(0)}, OptionalTypeIdx{}, some(rec(1))),
					descriptorTestStruct(true, nil, some(rec(0)), OptionalTypeIdx{}),
				}},
			}},
		},
		{
			name: "descriptor subtype cannot omit describes",
			m: &Module{Types: []RecType{
				descriptorTestPair(false),
				{SubTypes: []SubType{descriptorTestStruct(true, []TypeIdx{abs(1)}, OptionalTypeIdx{}, OptionalTypeIdx{})}},
			}},
		},
		{
			name: "descriptor subtype cannot introduce describes",
			m: &Module{Types: []RecType{
				{SubTypes: []SubType{descriptorTestStruct(false, nil, OptionalTypeIdx{}, OptionalTypeIdx{})}},
				{SubTypes: []SubType{
					descriptorTestStruct(true, nil, OptionalTypeIdx{}, some(rec(1))),
					descriptorTestStruct(true, []TypeIdx{abs(0)}, some(rec(0)), OptionalTypeIdx{}),
				}},
			}},
		},
		{
			name: "descriptor must declare corresponding supertype",
			m: &Module{Types: []RecType{
				descriptorTestPair(false),
				{SubTypes: []SubType{
					descriptorTestStruct(true, []TypeIdx{abs(0)}, OptionalTypeIdx{}, some(rec(1))),
					descriptorTestStruct(true, nil, some(rec(0)), OptionalTypeIdx{}),
				}},
			}},
		},
		{
			name: "described type must declare corresponding supertype",
			m: &Module{Types: []RecType{
				descriptorTestPair(false),
				{SubTypes: []SubType{
					descriptorTestStruct(true, nil, OptionalTypeIdx{}, some(rec(1))),
					descriptorTestStruct(true, []TypeIdx{abs(1)}, some(rec(0)), OptionalTypeIdx{}),
				}},
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expectValidateErr(t, tc.m, ErrTypeMismatch)
		})
	}
}
