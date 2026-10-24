package wago

func (p stagedGCStructProduct) requiresHelpers() bool {
	return p == stagedGCStructGeneric || p == stagedGCStructNamedGets || p == stagedGCStructNumericLocal || p == stagedGCStructNullDereference || p == stagedGCStructPacked || p == stagedGCStructBasic || p == stagedGCStructRefTestTable || p == stagedGCStructRefTestConcrete || p == stagedGCStructRefTestAbstract || p == stagedGCStructExtern || p == stagedGCStructRefEq || p == stagedGCStructRefCastAbstract || p == stagedGCStructRefCastConcrete || p == stagedGCStructBrOnCastAbstract || p == stagedGCStructBrOnCastConcrete || p == stagedGCStructBrOnCastNullability || p == stagedGCStructBrOnCastFailAbstract || p == stagedGCStructBrOnCastFailConcrete || p == stagedGCStructBrOnCastFailNullability
}

func (p stagedGCStructProduct) requiresArrayHelpers() bool {
	return p == stagedGCStructRefTestAbstract || p == stagedGCStructExtern || p == stagedGCStructRefEq || p == stagedGCStructRefCastAbstract || p == stagedGCStructBrOnCastAbstract || p == stagedGCStructBrOnCastFailAbstract
}
