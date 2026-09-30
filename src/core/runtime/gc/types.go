package gc

import raw "github.com/wago-org/wago/src/core/runtime/gc/native"

type Config = raw.Config
type Stats = raw.Stats
type Profile = raw.Profile
type AllocatorKind = raw.AllocatorKind
type RuntimeKind = raw.RuntimeKind
type TypeID = raw.TypeID
type TypeKind = raw.TypeKind
type StorageKind = raw.StorageKind
type FieldDesc = raw.FieldDesc
type TypeDesc = raw.TypeDesc
type StructDescBuilder = raw.StructDescBuilder
type RefTestKind = raw.RefTestKind
type RefTestTarget = raw.RefTestTarget
type TypeCanonicalization = raw.TypeCanonicalization
type RootClass = raw.RootClass

const ProfileThroughput = raw.ProfileThroughput
const ProfileTiny = raw.ProfileTiny
const AllocatorPagedSizeClass = raw.AllocatorPagedSizeClass
const AllocatorTinyFixedBlock = raw.AllocatorTinyFixedBlock
const RuntimeGenerational = raw.RuntimeGenerational
const RuntimeIncrementalMarkSweep = raw.RuntimeIncrementalMarkSweep
const KindFunc = raw.KindFunc
const KindStruct = raw.KindStruct
const KindArray = raw.KindArray
const StorageI8 = raw.StorageI8
const StorageI16 = raw.StorageI16
const StorageI32 = raw.StorageI32
const StorageI64 = raw.StorageI64
const StorageF32 = raw.StorageF32
const StorageF64 = raw.StorageF64
const StorageRef = raw.StorageRef
const StorageRefNull = raw.StorageRefNull
const StorageFuncRef = raw.StorageFuncRef
const StorageFuncRefNull = raw.StorageFuncRefNull
const StorageExternRef = raw.StorageExternRef
const StorageExternRefNull = raw.StorageExternRefNull
const StorageV128 = raw.StorageV128
const RefTestAny = raw.RefTestAny
const RefTestEq = raw.RefTestEq
const RefTestI31 = raw.RefTestI31
const RefTestStruct = raw.RefTestStruct
const RefTestArray = raw.RefTestArray
const RefTestNone = raw.RefTestNone
const RefTestDefined = raw.RefTestDefined

func NewStructDesc(id TypeID, fields []StorageKind) (TypeDesc, error) {
	return raw.NewStructDesc(id, fields)
}
func NewStructDescBuilder(id TypeID, count int) StructDescBuilder {
	return raw.NewStructDescBuilder(id, count)
}
func NewArrayDesc(id TypeID, element StorageKind) (TypeDesc, error) {
	return raw.NewArrayDesc(id, element)
}
func ValidateTypeDescs(types []TypeDesc) error { return raw.ValidateTypeDescs(types) }
func ValidateConfig(config Config) error       { return raw.ValidateConfig(config) }

var ErrCastFailure = raw.ErrCastFailure
