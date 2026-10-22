//go:build linux && !tinygo

package riscv64

import (
	"encoding/hex"
	"fmt"
)

type GeneralGCSourceFixture struct {
	Name            string
	Wire, Canonical []byte
}

func GeneralGCSourceFixtures() []GeneralGCSourceFixture {
	var cases []GeneralGCSourceFixture
	add := func(name string, wire []byte) {
		cases = append(cases, GeneralGCSourceFixture{name, append([]byte(nil), wire...), canonicalSectionSizes(wire)})
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayCopyHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayCopyHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayDefaultOfficialHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayDefaultOfficialHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayFillHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayFillHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayFixedLocalHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayFixedLocalHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayFixedOfficialHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayFixedOfficialHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayInitDataHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayInitDataHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayInitDataWideHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayInitDataWideHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayInitElemHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayInitElemHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayLocalHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayLocalHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayPackedDataHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayPackedDataHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayReferenceHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayReferenceHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCArrayUniformHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCArrayUniformHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCBrOnCastAbstractHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCBrOnCastAbstractHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCBrOnCastConcreteHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCBrOnCastConcreteHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCBrOnCastFailAbstractHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCBrOnCastFailAbstractHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCBrOnCastFailConcreteHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCBrOnCastFailConcreteHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCBrOnCastFailNullabilityHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCBrOnCastFailNullabilityHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCBrOnCastNullabilityHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCBrOnCastNullabilityHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCExternHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCExternHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCRefCastAbstractHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCRefCastAbstractHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCRefCastConcreteHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCRefCastConcreteHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCRefTestAbstractHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCRefTestAbstractHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCRefTestConcreteHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCRefTestConcreteHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCRefTestTableHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCRefTestTableHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCStructBasicHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCStructBasicHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCStructGetHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCStructGetHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCStructMutationHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCStructMutationHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCStructNullHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCStructNullHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCStructNumericGlobalsHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCStructNumericGlobalsHex", wire)
	}
	{
		wire, err := hex.DecodeString(rv64GCStructPackedHex)
		if err != nil {
			panic(err)
		}
		add("rv64GCStructPackedHex", wire)
	}
	for i, wire := range rv64GCTypeSubtypingCrossGroupMismatchLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingCrossGroupMismatchLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingDeclarationsWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingDeclarationsWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingDuplicateRecursiveLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingDuplicateRecursiveLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingExtendedProjectionLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingExtendedProjectionLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingExtendedRecursiveLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingExtendedRecursiveLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingFinalityLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingFinalityLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingIndependentStructLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingIndependentStructLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingLinkWires/%d", i), wire)
	}
	add("rv64GCTypeSubtypingNonFlatExportWire", rv64GCTypeSubtypingNonFlatExportWire)
	for i, wire := range rv64GCTypeSubtypingRecursiveFunctionWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingRecursiveFunctionWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingRefFuncGlobalWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingRefFuncGlobalWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingRefTestWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingRefTestWires/%d", i), wire)
	}
	add("rv64GCTypeSubtypingRuntimeCallCastWire", rv64GCTypeSubtypingRuntimeCallCastWire)
	add("rv64GCTypeSubtypingRuntimeFinalityCallCastWire", rv64GCTypeSubtypingRuntimeFinalityCallCastWire)
	add("rv64GCTypeSubtypingRuntimeTypedTableCallWire", rv64GCTypeSubtypingRuntimeTypedTableCallWire)
	for i, wire := range rv64GCTypeSubtypingStructLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingStructLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingStructMismatchLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingStructMismatchLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingStructProjectionLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingStructProjectionLinkWires/%d", i), wire)
	}
	for i, wire := range rv64GCTypeSubtypingTransitiveMismatchLinkWires {
		add(fmt.Sprintf("rv64GCTypeSubtypingTransitiveMismatchLinkWires/%d", i), wire)
	}
	{
		wire, err := hex.DecodeString(rv64OfficialObjectRefEqHex)
		if err != nil {
			panic(err)
		}
		add("rv64OfficialObjectRefEqHex", wire)
	}
	return cases
}
