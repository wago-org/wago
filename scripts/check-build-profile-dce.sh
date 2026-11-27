#!/usr/bin/env bash
# Check linked code and static data in the runtime and a Go embedding application.
set -euo pipefail
cd "$(dirname "$0")/.."
check_dir=$(mktemp -d)
trap 'rm -rf -- "$check_dir"' EXIT

export WAGO_HOME="$check_dir/home"
printf '%s\n' '{"settings":{"build":{"core":"1.0"}}}' > "$check_dir/wago.json"
go build -o "$check_dir/wago" ./cli/wago
mvp_tags=$(cd "$check_dir" && "$check_dir/wago" config build-tags)
test -n "$mvp_tags"

simd='\.((\(\*funcValidator\)\.)?stepSIMD|simdEffects|simdLoads|simdLaneLimits|fdMem|fdNoImm|emitSIMD|simdFallback|readV128ConstExpr)$'
relaxed='\.(fdRelaxedNoImm|relaxedSIMDEffects|v128RelaxedMadd|relaxedDotI8x16I7x16|swarRelaxedMadd|swarRelaxedDot|swarRelaxedCoreOpcode|swarRelaxedMaddOpcode)([.-].*)?$'
exceptions='\.(decodeTryTableHeader|decodeCatch|decodeTagType|validateTagType|directStartTryTable|emitEHHandler|emitEHCatchRoute|ScanEHFrameShape|exceptionAdmissionProjection|parseExceptionCatches|restoreExceptionStateTypes|newNativeExceptionTag|releaseNativeExceptionTags|ownedExceptionCollectors|dispatchOwnedExceptionHelper|managedExceptionPayloadType|initializeExceptionRoots|restoreExceptionHandlers)$'
exception_tag_lifetime='\.(attachTagImports|rollbackTagImports|installNativeTagDirectory|detachImportedTags)([.-].*)?$|\.\(\*tagImportAttachments\)\.(attach|detachAll|recordAttached)([.-].*)?$'
exception_compiled_metadata='\.(exceptionTags|exceptionTagExports|hasExceptionTagMetadata)([.-].*)?$'
threads='\.(emitAtomics|publishAtomicWaitContext|cancelAtomicWaitContext|atomicHelperSlots|appendAtomicHelpers|rv64ThreadedMemoryProfile|prepareThreadedExecution|lockThreadedInstanceState|usesPrivateThreadedExecution|setAtomicWaitHelperPanicError|errAtomicWaitInstanceClosed|atomicWaitInvocationContexts|(atomicWaitCloseError|\(\*atomicWaitCloseError\))\.Error)$'
thread_wait_closed='wago: instance closed while waiting'
tail_inlining='\.(tailInlineZeroMask|tailLoopInlineCandidate|detachInlineTailPrefix|restoreInlineTailPrefix|tryReuseInlineTailCompare|rememberInlineTailCompare|finishLocalTailArgumentFlags|localTailEntryTarget|threadTailEntryTest|tryInlineColdReturn|selfTailEntryCompare|reuseInlineTailArgumentFlags|selfTailAssignmentSetsZero|inlineTailConstantSet|captureInlineTailConstants|integerTailTransfers|tailTransferLeaf|noteTailInlineDivConstant(s)?|preloadTailInlineDivConstants)([.-].*)?$'
tailcalls='\.(returnCallIndex|proveSelfTailCallerPins|rv64NewDefinedTailGenerationWithBudgets|newNativeTailImportedOwner|newNativeTailDispatcher|rv64NewNativeTailOwner|admitClosedReferenceImportTailPlans|railshotPrepareNativeTail|rv64ValidateTailTargets|rv64ValidateNativeTailPlansWithCodeBytes|dynamicReferenceTailCall|dynamicIndirectTailCall|localIndirectTailCall)$|/riscv64.*\.closeMappings$'
rv64_tail_metadata='stageNativeTailMetadata|NativeTailInstanceBudget|NativeTailMetadataBudget|NativeTailHostThunkWithCertificate|rv64StoreNativeTailModule'
rv64_tail_retirement='railshotRetry(Closed)?NativeTableRetirement|\(\*NativeTailDispatcher\)\.(Close|CheckDetach)|\(\*rv64NativeTailGeneration\)\.(retire|deactivate|releaseReservation)'
rv64_tail_continuation='NativeTailHostContinuation|nativeTailExitPointer|tailRAExitCodeInvocation'
rv64_tail_admission='admitReferenceTailResults|rv64RootedBodyHasTailBridge|rootedScalarNumeric(Indirect|Reference)Tail|rv64(Imported|Owned)Table64IndirectTailProduct|rv64OwnedTwoTable64IndirectTail|admitOwned(Two)?Table64IndirectTail|rv64CollectorTailDeclaredCarrier|tailRASignatureMode'
rv64_tail_storage="\.($rv64_tail_metadata|$rv64_tail_retirement|$rv64_tail_continuation|$rv64_tail_admission)([.-].*)?$"
extended_const_arithmetic='\.(evalScalarConstExprProgram|moduleConstExprGlobalResolver|evalConstIntegerArithmetic)(\.func[0-9]+)?$'
gc_legacy_reference='/src/wago\.rv64GC(RefTest|RefCast|BrOnCast|Extern)[[:alnum:]]*([.-].*)?$'
gc_legacy_aggregate='/src/wago\.rv64GC(Array|Struct)[[:alnum:]]+([.-].*)?$'
gc_runtime_dispatch='\.(dispatchGC(Array|Struct)HelperLocked|dispatchManagedFunctionAggregate|managedFunctionAggregateMayAllocate)([.-].*)?$'
gc_prepared_scalar='/src/wago\.(\(\*WasmFunc\)\.(prepareGCScalar|callGCPrepared|invokeGCScalar(32Admitted)?|tryBeginPrivateGCState|tryInvokeRevocableGCScalar|invokePrivateGCScalar(Slow)?|privateGCNativeContextValid)|\(\*Instance\)\.(beginPreparedGCInvocation|endPrivateGCInvocation|privateGCStateValid))(\.[[:alnum:]]+)?$'
gc_prepared_inductions='/src/wago\.(clearPreparedInductionTrap|\(\*WasmFunc\)\.(prepareGCInductions|tryInvokePreparedI31|tryInvokePreparedDeadInduction)|\(\*Compiled\)\.setPreparedGCInductions)([.-].*)?$'
gc_compiled_products='/src/wago\.\(\*Compiled\)\.(stagedGC(TypeSubtyping|Struct|Array|I31)Product|usesGC(Struct|Array)Helpers|usesGenericGCExecution|collectorFreeGCArrayMetadata)([.-].*)?$|/src/wago\.\(\*compiledCodeCache\)\.(setGCProducts|hasGCObjectProducts)([.-].*)?$'
gc_const_expr='\.(evalCompiledGCConstExpr|gcConstStorageValue|gcConstHeapKind)([.-].*)?$|\.((\(\*gcConstStackRoot(Set|Slot)\))|gcConstStackRoot(Set|Slot))\.'
gc_lowering='\.(emitGCObjectStub|emitGCScalarFill|emitNativeGCStubs|emitNativeArrayAllocStub|newGCRefTestTableStateFor|compileGCInitializers|admitRV64LegacyGCProducts|rv64LegacyGCHelperSlots|evalNullExternConversionConstExpr|evalI31ConstExprBytes|evalCompiledI31ConstExpr|lowerI31ElementInitializer|addI31TableInitializer|admitRV64I31OfficialGlobalInitializer|admitRV64I31OfficialTableGlobalInitializer|abstractCollectorNullInitializer|validateGCConstExpr|admitLegacyGCWire|rv64FixedRootlessGCProduct|rv64LegacyGCImportSubtype|railshotLegacyGCImportIdentity|rv64GCTypeSubtypingRuntimeTypedTableCallProfileWithCode|buildModuleGCSharedStubs|emitModuleGCResolveObjectStub|validateGCBodyViews|prepareGCFrameCallsite|admitLegacyGCStateProducts|admitRV64ImmediateReferenceModule|rv64I31OfficialTableShape)$'
gc_names='\.(\(\*stagedGC(Array|Struct|I31)Product\)|stagedGC(Array|Struct|I31)Product)\.String$'
bulk_lowering='\.(boundedBulkArgumentPlans|\(\*bulkAnalyzer\)\.sequence)$'
bulk_validation='/compiler/wasm\.\(\*funcValidator\)\.stepBulk(Memory|Table)Instruction([.-].*)?$'
table_bulk_runtime='\.(dispatchCompactExternTableBulk|dispatchCollectorTableBulkHelperLocked|gcRootedTableElement(Descriptor|Segment|Bits))([.-].*)?$|\.\(\*compactExternPreparedRoots\)\.'
table_reference_runtime='\.(dispatchCompactExternTableWrite|dispatchCollectorTableOpsLocked|compactExternPins|gcRootedTableReference)([.-].*)?$'
table_reference_validation='/compiler/wasm\.\(\*funcValidator\)\.step(BulkTable|ReferenceTable)Instruction([.-].*)?$'
fc_tables='\.(fcSaturating|fcWide)$'
saturating_float='\.(fcSaturating|truncSat(Signed|U32|U64)?|saturatingFloatKind|swarSaturatingFloatOpcode)([.-].*)?$'
wide_arithmetic='\.(fcWide|wideArithmetic|wideAddSub|wideMultiply)([.-].*)?$'
reference_const_expr='\.(readConstRefNullType|constRefNullASTType|constRefFuncType|referenceConstExprTypeMatches)$'
reference_signatures='\.(marshalPublicReferenceArgs|translatePublicReferenceResultsMode|translateHostReferenceArgsMode|translateHostReferenceResults|optDirectReferenceABI|directReferenceABIEnabled|admitReferenceWire)$'
collector_object_tokens='/src/wago\.\(\*(referenceStore|Instance)\)\.(issueGCRefLockedMode|stageGCRefArgument|stageGCHostResult|publishGCRefGlobal|rootGCHostArguments|clearGCHostResultRoots|clearGCRefArgumentRoots)([.-].*)?$'
collector_object_token_index='/src/wago\.\(\*gcObjectTokenIndex\)\.(insertObjectTokenLocked|takeObjectTokenRootsLocked)([.-].*)?$'
collector_argument_roots='/src/wago\.\(\*gcArgumentRoots\)\.(stage|clear|slot)([.-].*)?$'
gc_clone_roots='/src/wago\.\(\*gcCloneRoot\)\.(stage|clear)([.-].*)?$'
parked_host_object_roots='/src/wago\.\(\*parkedHostRootSlots\)\.(add|clear)([.-].*)?$|^_?type:\*?github\.com/wago-org/wago/src/wago\.(parkedHostRoots|parkedHostRootSlots)$|^_?reflect/types\.type:.*named:github\.com/wago-org/wago/src/wago\.(parkedHostRoots|parkedHostRootSlots)([^[:alnum:]_]|$)'
gc_carrier_elision='^github\.com/wago-org/wago/src/wago\.(buildGCOptionBodyViewWithLocalLimit|gcOptionMayContainCarrier|\(\*gcOptionView\)\.(applyEdits|metadata))([.-].*)?$'
gc_call_boundaries='^github\.com/wago-org/wago/src/wago\.(collectorObjectFrameRefType|wasmFuncTypeTransfersCollectorRefs|moduleHasCollectorReferenceCallBoundary|moduleHasInternalCollectorReferenceFunctions|valueTypeTransfersCollectorObject|\(\*(Compiled|Instance)\)\.(hasCollectorReferenceCallBoundary|hasInternalCollectorReferenceFunctions|importTransfersCollectorObjects|signatureTransfersCollectorObjects|validateCollectorHostImport|pluginGCHostSignature)|rv64CollectorHostTailTransport)([.-].*)?$'
gc_host_and_heap_binding='^github\.com/wago-org/wago/src/wago\.(\(\*HostFuncRef\)\.isGCBridge|hasEquivalentLocalGCHeapTypes)([.-].*)?$'
gc_object_global_roots='^github\.com/wago-org/wago/src/wago\.\(\*Instance\)\.(installGlobalRootMappings|syncGenericGCGlobalRootsLocked|reconcileGCGlobalRoots(After)?)([.-].*)?$'
gc_heap_ref_matching='\.(collectGenericGCAtBoundary|genericGCBoundaryCollectionSafe|gcHeapRefMatchesValueType|compactForeignAnyMatches|stageCompactExternAnyArgument)([.-].*)?$'
collector_ref_matching='\.(gcRefMatchesValueType|hasGCRefGlobals)([.-].*)?$'
gc_host_result_journal='^github\.com/wago-org/wago/src/wago\.\(\*hostGCResultJournal\)\.(addGCResult|releaseGCResults)([.-].*)?$'
collector_host_token_journal='^github\.com/wago-org/wago/src/wago\.\(\*gcHostTempTokens\)\.(add|release|token|setToken)([.-].*)?$'
gc_staged_object_cleanup='^github\.com/wago-org/wago/src/wago\.(\(\*Instance\)\.(dropStagedGCObjectRoots|existingStagedGCObjectRoots|existingGCRefTestTableState|existingGCExternConversionState|existingGCArrayElementState)|\(\*(gcRefTestTableState|gcArrayElementState)\)\.drop|\(\*gcExternConversionState\)\.close)([.-].*)?$'
gc_constructor_capacity='^github\.com/wago-org/wago/src/wago\.(gcStructConstructorSlotCapacity|gcInternalHelperSlotCapacity|gcSyncHostSlotCapacity|\(\*gcArrayConstructorScratch\)\.constructorValues)([.-].*)?$'
oversized_table64='\.(stagedInertOversizedTable64|inertOversizedLocalTable64)$'
rv64_gc_roots='\.(recordGCFrameSafepointAt|recordGCFrameCallsiteAdjusted|rv64PopulateSourceReceipt|rv64RootedTableDigest|admitRV64GCRootedScalarWire|exceptionOpaqueReferences|prepareCollectorOperandPlans|prepareCollectorOperands|NativeNonCollectingGCCall|newInternalReferenceTypeDirectory)$'
rv64_function_environments='\.(managedFunctionGraphs|managedFunctionCollectors|managedFunctionMemberCount|managedFunctionAfterClose|tryCollectAfterGate|collectManagedFunctionGraph|managedFunctionDescriptorOwner|\(\*managedFunctionConstruction\)\.close)$'
rv64_collector_reference_lifetime='\.(managedFunction(InvocationStates|Members|Graphs|Collectors|MemberCount|AfterClose|AfterInvocationUnlock|BindInvocationState|RootEpochLocked|RetainAllowedLocked|PrepareRegistration|RegisteredLocked|LifetimeLocked|DeferFinalization)|drainManagedFunction(Store)?Reclaims|\(\*managedFunctionGraph\)\.(drainAfterGate|tryCollectAfterGate)|compactExtern(Collectors|Words|WordReserved|HostGlobals|HostTables|HostTableCount)|releaseInstanceCompactExtern|releaseClosedHostPublicTokensLocked|railshotCompactExternUnpinnedAfterClose)([.-].*)?$'
rv64_reference_validation='\.(admitClosedReferenceFunctionModule|admitClosedReferenceProductionOptions|hasDefinedReferenceFunctions|rv64ValidateReferenceFunctions|rv64ValidateReferenceFunctionState|rv64ValidateReferenceFunctionTable|rv64ValidateLocalMutableReferences)$'
rv64_typed_keys='\.(rv64ValidatePrimitiveFunctionKeys|rv64NonCallableReferenceFunction)$'
rv64_primitive_declarations='\.(admitPublicPrimitiveReferenceModule|NeedsFunctionSubtypeHelpers|needsFunctionSubtypeHelpers|validatePrimitiveReferenceInitializer|primitiveReferenceTargetWithTypes)$'
rv64_function_carriers='\.admitPublicFunctionCarrierModule$'
rv64_reference_boundaries='/riscv64\.(admitPublic(DynamicHostReference|HostReference|ReferenceCall|ReferenceCarrier)Boundary|admitMutableFunctionGlobal[[:alnum:]]+|admitStateOnlyMutableFunctionGlobalImport|publicReferenceCarrierTargetMode)$'
rv64_reference_runtime='/src/wago\.(rv64Public(DynamicHostReference|HostReference|ReferenceCall)BoundaryWithPlan|rv64MutableFuncrefGlobal(Get|Clear|StoreSelf|Copy|Move|BidirectionalCopy|LocalCopy|LocalPublish|LocalTransfer|LocalExport|LocalObserve|LocalCall|TransferMetadata)|railshotUsesGuestGlobalStoreHelper|\(\*Instance\)\.retainGuestGlobalStoreDescriptor)$'
rv64_bulk_lowering='/riscv64\..*\.(memoryBulkAt|bulkWidths|privateTableCopyBetween|gcObjectTableCopy)$'
rv64_optional_numeric='/riscv64\..*\.(wideAddSub|wideMultiply|saturatingFloatKind|swarSaturatingFloatOpcode|floatTruncSaturating)$'
rv64_compact_extern='\.(compactExternCollectors|compactExternWords|compactExternHostGlobals|compactExternHostTables|compactExternHostAccessRoots|admitStateOnlyExternrefGlobals|railshotFinishCompactExternHostGlobalDetach|\(\*compactExternCollector\)\.persistentWord|\(\*compactExternConstruction\)\.(close|record)|\(\*Compiled\)\.compactExternFrameRoots)$'
rv64_compact_extern_cleanup='\.(releaseRootedExternref|claimCompactExternRoot|publicExternrefValue|closedHostPublicTokenEntriesLocked|releaseClosedHostPublicTokensLocked|claimCompactExternRoots|releaseDirectExternrefs)([.-].*)?$'
gc_extern_conversion='\.\(\*gcExternConversionState\)\.(internalExternFromPublic|publicExternFromInternal)$'
table64_admission='\.(admitLegacyTable64|admitLegacyIndexedElements|rv64QualifiedTable64|rv64LegacyIndexedTable64|admitLegacyTable64IndirectTail|rv64LegacyTable64IndirectTailPlanShape|admitOwnedTwoTable64IndirectTail|rv64(Owned|Imported)Table64IndirectTailProduct)([.-].*)?$'
rv64_indexed_table32='\.(rv64IndexedTable32Witness|rv64StateOnlyTwoTable32|rv64OwnedTwoTable32[[:alnum:]]+)$'
rv64_bulk_table32='\.rv64OwnedTwoTable32(Copy|InitDrop|Indirect)$'
rv64_core3_references='\.rv64Core3(RefIsNullExecution|RefIsNullExecutionCodecPrefix|ReferenceDeclarationWitness)$'
rv64_gc_table='\.rv64(ImmediateReferenceContract|GeneralI31Contract)$'
rv64_reference_globals='\.(rv64(FreezeGlobalReferences|ValidateResolvedGlobals|AdmitPrimitiveReferenceValue|ReferenceGlobalSnapshot|GlobalDescriptorOwner|ImportedGlobalValue)|validateFrozenGlobalTargets|validateRuntimeReferenceGlobalMetadata|moduleDynamicFuncrefEscape|moduleDynamicFuncrefEscapeWithValidation)$'
rv64_reference_controls='\.(internalFunctionReferenceWithTypes|internalExplicitReferenceTypeMode)$'
rv64_reference_emitters='/riscv64\.\(\*integerEmitter\)\.(referenceCall|dynamicReferenceTailCall|storePrivateReference|tableReferenceForSubtype|guardReferenceSubtype|canonicalReferenceGuard|guardCollectorTailContext)$'
rv64_rooted_receipt_type='^_?type:\*?github\.com/wago-org/wago/src/wago\.rv64RootedReceipt$|^_?reflect/types\.type:.*named:github\.com/wago-org/wago/src/wago\.rv64RootedReceipt([^[:alnum:]_]|$)'
exact_collector_frames='^github\.com/wago-org/wago/src/wago\.(newGCFrameRootPlanOwned|newGCFrameRootPlanWithInstructions|importOnlyGCFrameRoots|\(\*Compiled\)\.(setCompiledGCFrameRoots|needsExactNativeGCRoots|needsNativeGCABI|hasCollectorDeclarationRoots|needsRuntimeGCCollectorDomain|compactExternFrameRoots|opaqueExternHelperSlots)|\(\*gcNativeFrameRoots\)\.(rangeChain|rangeSuspendedRoots|syncGlobalsBeforeCollection)|managedFunctionTransientRoots\.(RangeRoots|RangeRootRefs)|\(\*referenceStore\)\.(rangeGCDomainSuspendedRoots|registerGCFrameCodeRangeLocked|unregisterGCFrameCodeRangeLocked|gcFrameOwner)|rv64PopulateSourceReceipt|rv64FreshRootedGC|rv64RootedClosedCallableFunction|rv64RootedClosedCallableOwner|validateCompiledGCFrameRoots|validCompiledGCFunctionTables|\(\*compiledWriter\)\.gcFrameRoots)([.-].*)?$'
gc_frame_planning='\.(ModuleFunctionReferenceCarriers|moduleHasCollectorReferenceCallBoundary|moduleHasGCAllocationSitesWithValidation|validGCModuleFrameRootPlanKinds|validateCollectorHostImport)$'
reference_import_dispatch='\.(dispatchSyncHostReference(Gated)?|validateReferenceImportWithCollector|hasInternalCollectorReferenceFunctions)$'
rv64_reference_initializers='/riscv64\.(elementInitializerExpression|admitTableInitializer|abstractCollectorNullInitializer)$'
rv64_collector_frame_masks='/riscv64\.finalizeGCFrameRootMasks$'
rv64_gc_bridge_catalog='/railshot/riscv64\.(gcStructBridgeSpecs|gcArrayAndConversionBridgeSpecs|gcObjectBridgeAllocationSlots|gcObjectBridgeHasNoResult|gcObjectBridgeIsNonCollecting)$'
rv64_backend_frame_roots='/riscv64\.(validateGCFrameRootPlansOwned|rv64CollectorFrameRefType|referenceCallCarriesCollector|\(\*integerEmitter\)\.(recordDeadGCFrameSites|recordGCFrameSafepointAt|recordGCFrameCallsiteAdjusted|initializeGCFrameRoots|finishScalarGCFrameRoots|finishSWARGCFrameRoots)|\(\*gcFrameEmissionMode\)\.relocateGCFrameCallsites)([.-].*)?$'
rv64_collector_tail_carriers='\.(rv64CollectorTail(Metadata|DeclaredCarrier)|nativeTailCollectorHostCarrier|NewNativeTailHostCollectorFunction)([.-].*)?$'
rv64_legacy_gc_module='\.(selectLegacyGCModule|configureLegacyGCModule|AdmitGeneralI31Module|admitRV64I31OfficialRefTest|isI31CoreRef|isI31RefType)([.-].*)?$'
rv64_reference_type_resolution='/riscv64\.(sourceFunctionTypeNeedsResolution|\(\*sourceFunctionTypeDirectory\)\.resolveReferenceSignatures)$'
collector_import_domains='/src/wago\.\(\*Instance\)\.(validateGCInvocationImport|reachesPrivateGCInvocationDomain|gcInvocationDomain(s(ForInspection)?)?)([.-].*)?$'
collector_store_topology='\.(takeGCCollectorsLocked|ensureGCTopology(Locked)?|gcTopologyLocked|lockGCDomainTopology(ForInstance)?|lockFuncrefTableTopology)([.-].*)?$'
collector_instance_registration='\.(newGCInstanceRegistration|gcRegistration|gcInvocationDomainsForInspection|gcInvocationDomain|gcInvocationDomains)([.-].*)?$'
collector_invocation_records='\.(hostGCLeaseOwner|lockGCInvocation(Context)?|ownsGCInvocation|suspendGCInvocation)([.-].*)?$|\.\(\*?(gcInvocationLease|gcInvocationSuspension)\)\.(unlock|release|resume)([.-].*)?$'
collector_release_batch='\.(releaseLifetimeEntriesWith|releaseGC(ResultRootMetadata(With)?|ResultRoots|Collectors)|clearGCResultRootSlot|claimGC(ResultRoots|Collectors)Locked)([.-].*)?$'
collector_teardown='/gc/native\.\(\*Collector\)\.(Close|SetGlobalSlot)([.-].*)?$|/src/wago\.(\(\*referenceStore\)\.releaseUnclaimedGCCollector|releaseCompactExternHostRoots|railshotReleaseCompactExternHost(Global|Table))([.-].*)?$'
rv64_collector_construction='\.(managedFunctionConstructions|managedFunctionConstructionCount|newManagedFunctionConstruction|recordManagedFunctionConstruction|newCompactExternConstruction|compactExternConstructionNeeded|\(\*(managedFunctionConstruction|compactExternConstruction)\)\.(bind|close|record|ensureState))([.-].*)?$'
collector_construction='/gc/native\.(NewCollector|newTinyCollector|normalizeConfig|\(\*Collector\)\.CollectFull)([.-].*)?$|/src/wago\.\(\*instanceBuilder\)\.(prepareCollector|prepareNativeGCView|rollbackCollector|buildNativeGCInstanceView)([.-].*)?$'
collector_instance_lifecycle='/src/wago\.((\(\*instanceBuilder\)\.(prepareCollector|rollbackCollector|collectorBuildState|prepareNativeGCView|buildNativeGCInstanceView))|(\(\*referenceStore\)\.(ownsGCCollector|releaseGCDomainLocked|lockGCDomainTopologyForInstance)))$'
collector_instance_metadata='\.(newInstanceCollectorMetadata|gcTypeMap|gcNativeView|hasNativeCollector|revokePrivateGCEntry|validateNativeGCEntry|\(\*JobMemory\)\.(Set)?GCNativeViewPtr)([.-].*)?$'
gc_host_activations='\.(pushGCHostActivation|popGCHostActivation|restoreGCHostActivation|newGCHostActivationToken)([.-].*)?$'
collector_public_state="\.(publicGCState|existingPublicGCState)([.-].*)?$|$gc_host_activations"
gc_extern_setup='/src/wago\.newGCExternConversionState$'
rv64_frozen_global_cleanup='/src/wago\.(railshot(ReleaseInstanceGlobalReferences|ReleaseGlobalReferences|InstallFrozenGlobalDependencies|AdoptFrozenGlobalDependencies))$'
rv64_terminal_close_collection='\.(finishLogicalCloseCollectionHandoff|completeLogicalCloseCollection|publishTerminalCollectionReady|railshot(CollectClosingDependencyComponent|CollectTerminalClosedDependencyComponent|TerminalClosedContainerDependencyAdoption))([.-].*)?$'
reference_table_init='\.(compileTableInitializers|funcrefExprPayload|ParseFuncrefElementExpr)$'
rv64_reference_lowering='/riscv64\.(internalReferenceBlockType|\(\*integerEmitter\)\.storeExternGlobal)$'
rv64_immediate_collector_type='/riscv64\.immediateCollectorType$'
opaque_extern_planning='\.(moduleHasOpaqueExternTable|opaqueExternHelperSlots|ExternPersistentMutation)$'
gc_funcref_topology='\.(lockFuncrefTableTopology|hasIncompatibleGCProducer|ensureGCTopology|lockGCDomainTopology)$'
reference_argument_scopes='\.(railshot(BeginPublicFuncrefArguments|StagePublic(Funcref|Externref)Argument|ReleasePublicFuncrefArguments)|unpinExternref)$'
gc_cli='\.parseGCBytes$'
gc_metadata_lowering='/frontend\.(BuildGCTypeMetadataWithOptions|lowerGCTypeMetadata|flattenGCTypes|gcTypeResolver\.resolve)$'
gc_compiled_initializers='\.(compileGCInitializers|hasGCInitializers|gcStructGlobals|gcArrayGlobals|gcArrayElement|validateStagedI31TableInitializer)([.-].*)?$'
gc_source_type_metadata='/src/wago\.wasmTypeDescriptorConverter\.(lowerAggregateTypeFields|fieldType)$'
gc_artifact_type_metadata='/src/wago\.(validateAggregate(TypeFields|Field)|\(\*compiledWriter\)\.(aggregateTypeFields|fieldType))$'
gc_wire_types='/railshot/riscv64\.admitPublicAggregateFieldWire$'
rv64_aggregate_initializers='/railshot/riscv64\.(\(\*rootedScalarReferenceInitializer\)\.(validateAggregateExpression|valueMatches|constructedSubtype)|newRootedScalarAggregateParser(\..*)?|rootedScalarAggregate(At|Type))$'
memory64_width_scan='/wasm\.moduleMemargWidths$|/railshot/arm64\.\(\*fn\)\.memoryAddr64([.-].*)?$'
multi_memory='/src/wago\.(\(\*instanceBuilder\)\.instantiateSecondaryMemories|\(\*Instance\)\.releaseSecondaryMemories)([.-].*)?$'
native_indexed_memory='/railshot/(amd64|arm64)\.\(\*fn\)\.indexedMemAddr([.-].*)?$'
multi_value_blocks='/railshot/(amd64|arm64)\.\(\*fn\)\.indexedBlockType([.-].*)?$|/railshot/riscv64\.swarControlSignature([.-].*)?$'
compact_imports='/wasm\.decodeCompactImportGroup([.-].*)?$'
rv64_mutable_global_products='/railshot/riscv64\.(admitMutableFunctionGlobal[[:alnum:]]+|admitStateOnlyMutableFunctionGlobalImport)([.-].*)?$|/src/wago\.rv64MutableFuncrefGlobal[[:alnum:]]+([.-].*)?$'
gc_wasm_type_metadata='/wasm\.(decodeTypeMetadata|\(\*moduleValidator\)\.(validate(DeclaredSubtype|SubtypeMetadata|Descriptor(Metadata|SubtypeMetadata|SubtypeEdge))|compTypeSubtype|buildSupertypeAncestry))(\..*)?$'
rv64_extended_host_signature='/railshot/riscv64\.tailRASignatureMode$'
reference_subtype_bridge='\.(dispatchGCFunctionRefTest|functionSubtypeTestHelper|guardReferenceSubtype)([.-].*)?$'
gc_helper_recovery='\.(setGCHelperPanicError|(gcStructHelperError|\(\*gcStructHelperError\))\.Error)([.-].*)?$'
gc_helper_diagnostic='wago: WasmGC struct helper:'
reference_value_parser='/wasm\.(decodeRefType|decodeRefTypeForNull|decodeDirectReferenceElem|\(\*funcValidator\)\.stepReferenceInstruction|\(\*moduleValidator\)\.collectDeclaredFuncs(InExpr)?)$'
indexed_type_graph='/wasm\.\(\*Module\)\.writeStructuralFuncTypeGraph(\..*)?$'
typed_reference_ops='\.(stepCallRef|stepTypedReferenceInstruction|dynamicReferenceTailCall|referenceAsNonNull|\(\*swarFlow\)\.referenceBranch)([.-].*)?$|/railshot/(amd64|arm64)\.\(\*fn\)\.(callRef|returnCallRef|refAsNonNull|brOnNull|brOnNonNull)([.-].*)?$'
gc_metadata_validation='/gc/native\.(ValidateTypeDescs|validateTypeDescsFrom|validateStructDesc|validateUnorderedStructFields|validateSuperRelationsFrom|StructSize)([.-].*)?$'
gc_type_admission='/frontend\.supportPass\.(validateAggregateType|aggregateStorageType|supportedGCHeap|supportedGCReference|supportedStructuralTypeRef)$'
collector_type_admission='/wasm\.CollectorReferenceType$'
rv64_public_function_types='/riscv64\.internalReferenceTypes\.publicFunctionReference$'
collector='/gc/native\.(\(\*Collector\)|Collector\))\.'
simd_knobs='\.(optSIMD[[:alnum:]_]+|optAVX512[[:alnum:]_]+|optV128Pins|optV128DirectResults|simdSuperoptEnabled|simdLoadExtractEnabled|simdStoreLoadAddressEnabled)$|arm64\.(optV128ConstCache|v128ConstCacheEnabled)$'
simd_host_probe='/src/wago\.(cachedSIMDHostFeatures|simdHostFeaturesSupported)$'
thread_knobs='\.(optAtomic[[:alnum:]_]+|optSharedMemoryBase|atomicZeroWaitEnabled|atomicStaticBoundsEnabled|atomicNotifyEmptyEnabled|atomicLSEEnabled|sharedMemoryBaseEnabled)$'
bulk_knobs='\.(optBulk[[:alnum:]_]+|optMemcopy[[:alnum:]_]+|bulkMemoryLoopsEnabled|memcopyQPairsEnabled|memcopyTail4Enabled)$'
tail_knobs='\.(optTailSelfLoop|optTailLoopInline|tailSelfLoopEnabled|tailLoopInlineEnabled)$'
gc_knobs='\.(optDeadGCNew|optGCNativeAlloc|deadGCNewEnabled|nativeGCStructAllocEnabled|nativeStructAllocEnabled|gcSubtypeIntervalsEnabled|gcProductRequiredGateEnabled)$'
simd_names='V128LoadV128Load8x8SV128Load8x8U'
relaxed_names='I8x16RelaxedSwizzleI32x4RelaxedTruncF32x4S'
sign_extension_names='I32Extend8SI32Extend16SI64Extend8SI64Extend16SI64Extend32S'
simd_help='recognize bounded multi-operation SIMD sequences'
gc_help='allocate admitted GC objects through native nursery fast paths'
gc_heap_help='throughput GC heap capacity'

symbols() {
  local arch=$1 package=$2 tags=$3
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -tags "$tags" -o "$check_dir/artifact" "$package"
  artifact_symbols
}

artifact_symbols() {
  go tool nm "$check_dir/artifact" | awk '$2 ~ /^[TtDdBbRr]$/ && $3 ~ /github\.com\/wago-org\/wago\// { print $3 }'
}

assert_no_match() {
  local name=$1 kind=$2 status=0
  shift 2
  LC_ALL=C grep "$@" || status=$?
  case "$status" in
    0) printf 'Disabled proposal %s in %s\n' "$kind" "$name" >&2; return 1 ;;
    1) return 0 ;;
    *) printf 'Could not inspect %s in %s (grep status %s)\n' "$kind" "$name" "$status" >&2; return "$status" ;;
  esac
}

assert_absent() {
  local name=$1 inventory=$2 pattern=$3
  assert_no_match "$name" 'code or data' -E -- "$pattern" "$inventory"
}

assert_names_absent() {
  local name=$1 names=$2
  assert_no_match "$name" text -aqF -- "$names" "$check_dir/artifact"
}

assert_core1() {
  local name=$1 inventory=$2
  assert_names_absent "$name" "$thread_wait_closed"
  assert_names_absent "$name" "$gc_helper_diagnostic"
  grep -Eq '/src/wago.*(compileWithFrontendFeaturesAndInstructions|validateMetadata)' "$inventory"
  assert_absent "$name" "$inventory" "$simd|$relaxed|$exceptions|$exception_tag_lifetime|$exception_compiled_metadata|$threads|$tailcalls|$tail_inlining|$gc_lowering|$gc_compiled_products|$gc_prepared_inductions|$gc_prepared_scalar|$gc_const_expr|$gc_runtime_dispatch|$rv64_gc_bridge_catalog|$gc_legacy_aggregate|$gc_legacy_reference|$gc_names|$bulk_lowering|$fc_tables|$reference_signatures|$reference_const_expr|$oversized_table64|$table64_admission"
  assert_absent "$name" "$inventory" "$rv64_tail_storage|$extended_const_arithmetic|$compact_imports|$multi_memory|$multi_value_blocks|$native_indexed_memory"
  assert_absent "$name" "$inventory" "$collector|$rv64_gc_roots|$rv64_compact_extern|$rv64_compact_extern_cleanup|$gc_extern_conversion|$simd_knobs|$thread_knobs|$bulk_knobs|$tail_knobs|$gc_knobs"
  assert_absent "$name" "$inventory" "$rv64_bulk_lowering|$bulk_validation|$table_bulk_runtime|$table_reference_validation|$table_reference_runtime|$rv64_optional_numeric|$rv64_function_environments|$rv64_reference_validation|$rv64_typed_keys"
  assert_absent "$name" "$inventory" "$rv64_primitive_declarations|$rv64_function_carriers|$rv64_reference_boundaries|$rv64_reference_runtime"
  assert_absent "$name" "$inventory" "$rv64_indexed_table32|$rv64_core3_references|$rv64_gc_table|\.rv64ValidatePlainReferenceTableDirectory$"
  assert_absent "$name" "$inventory" "$rv64_reference_globals|$rv64_reference_controls|$gc_frame_planning|$exact_collector_frames|$rv64_rooted_receipt_type"
  assert_absent "$name" "$inventory" "$rv64_reference_emitters"
  assert_absent "$name" "$inventory" "$reference_import_dispatch"
  assert_absent "$name" "$inventory" "$rv64_reference_initializers|$rv64_collector_frame_masks|$rv64_backend_frame_roots"
  assert_absent "$name" "$inventory" "$rv64_reference_type_resolution|$collector_import_domains|$collector_store_topology|$collector_release_batch|$collector_instance_registration|$collector_invocation_records"
  assert_absent "$name" "$inventory" "$collector_instance_lifecycle|$collector_instance_metadata|$collector_public_state|$gc_extern_setup|$rv64_frozen_global_cleanup"
  assert_absent "$name" "$inventory" "$collector_object_tokens|$collector_object_token_index|$collector_argument_roots|$gc_clone_roots|$parked_host_object_roots|$gc_carrier_elision|$gc_call_boundaries|$gc_host_and_heap_binding|$gc_object_global_roots"
  assert_absent "$name" "$inventory" "$gc_heap_ref_matching|$collector_ref_matching"
  assert_absent "$name" "$inventory" "$gc_host_result_journal|$collector_host_token_journal"
  assert_absent "$name" "$inventory" "$gc_staged_object_cleanup|$gc_constructor_capacity|$gc_compiled_initializers|$gc_compiled_products"
  assert_absent "$name" "$inventory" "$reference_table_init|$rv64_reference_lowering|$rv64_immediate_collector_type"
  assert_absent "$name" "$inventory" "$opaque_extern_planning|$reference_argument_scopes"
  assert_absent "$name" "$inventory" "$gc_funcref_topology"
  assert_absent "$name" "$inventory" "$gc_metadata_lowering|$gc_compiled_initializers|$gc_metadata_validation|$gc_source_type_metadata|$gc_artifact_type_metadata|$gc_wire_types|$rv64_aggregate_initializers|$gc_wasm_type_metadata"
  assert_absent "$name" "$inventory" "$indexed_type_graph|$memory64_width_scan|$reference_value_parser|$reference_subtype_bridge|$rv64_extended_host_signature|$gc_helper_recovery"
  assert_absent "$name" "$inventory" "$gc_type_admission|$collector_type_admission|$rv64_public_function_types"
  assert_absent "$name" "$inventory" '\.(validatedFuncFlagsByKind|validatedFuncNeedsPayloadByKind|compiledMetadataUsesSIMD)$'
  assert_absent "$name" "$inventory" "$gc_cli"
  if [[ "$name" != linux/riscv64* ]]; then
    assert_absent "$name" "$inventory" "$rv64_terminal_close_collection"
  fi
  assert_names_absent "$name" "$simd_names"
  assert_names_absent "$name" "$relaxed_names"
  assert_names_absent "$name" "$sign_extension_names"
  assert_names_absent "$name" "$simd_help"
  assert_names_absent "$name" "$gc_help"
  assert_names_absent "$name" "$gc_heap_help"
  printf '%s passed\n' "$name"
}

for arch in amd64 arm64 riscv64; do
  for flavor in runtime embed; do
    package=./cli/wago
    tags="wago_runtime,$mvp_tags"
    if [[ "$flavor" == embed ]]; then
      package=./scripts/testdata/diagnostic-dce
      tags=$mvp_tags
    fi
    name="linux/$arch $flavor Core 1.0"
    inventory="$check_dir/symbols"
    symbols "$arch" "$package" "$tags" > "$inventory"
    if [[ "$arch" == amd64 ]]; then
      # Scalar native code still requires the amd64 CPU baseline check.
      grep -Eq "$simd_host_probe" "$inventory"
    else
      assert_absent "$name" "$inventory" "$simd_host_probe"
    fi
    assert_core1 "$name" "$inventory"
  done
done

for arch in amd64 arm64 riscv64; do
  name="linux/$arch runtime without compact imports"
  symbols "$arch" ./cli/wago wago_runtime > "$inventory"
  grep -Eq "$compact_imports" "$inventory"
  grep -Eq "$multi_memory" "$inventory"
  grep -Eq "$multi_value_blocks" "$inventory"
  LC_ALL=C grep -aqF "$sign_extension_names" "$check_dir/artifact"
  symbols "$arch" ./cli/wago wago_runtime,wago_no_compact_imports > "$inventory"
  grep -Eq '/wasm\.decodeImports$' "$inventory"
  assert_absent "$name" "$inventory" "$compact_imports"
  printf '%s passed\n' "$name"

  name="linux/$arch runtime without sign extension operations"
  symbols "$arch" ./cli/wago wago_runtime,wago_no_sign_extension_ops > "$inventory"
  grep -Eq "$compact_imports" "$inventory"
  assert_names_absent "$name" "$sign_extension_names"
  printf '%s passed\n' "$name"

  name="linux/$arch runtime without multi-memory"
  symbols "$arch" ./cli/wago wago_runtime,wago_no_multi_memory > "$inventory"
  grep -Eq '\.refreshMemoryDirectory$' "$inventory"
  if [[ "$arch" != riscv64 ]]; then
    # The shared primary memory also needs indexed native addressing.
    grep -Eq "$native_indexed_memory" "$inventory"
  fi
  assert_absent "$name" "$inventory" "$multi_memory"
  printf '%s passed\n' "$name"

  name="linux/$arch runtime without multi-value"
  symbols "$arch" ./cli/wago wago_runtime,wago_no_multi_value > "$inventory"
  grep -Eq '/wasm\.decodeBlockType$' "$inventory"
  assert_absent "$name" "$inventory" "$multi_value_blocks"
  printf '%s passed\n' "$name"
done

symbols riscv64 ./cli/wago wago_runtime > "$inventory"
grep -Eq "$rv64_mutable_global_products" "$inventory"
symbols riscv64 ./cli/wago wago_runtime,wago_no_mutable_global > "$inventory"
grep -Eq '\.rv64PrimitiveFunctionGlobalStores$' "$inventory"
assert_absent 'linux/riscv64 runtime without mutable global imports or exports' "$inventory" "$rv64_mutable_global_products"
printf '%s\n' 'linux/riscv64 runtime without mutable global imports or exports passed'

for arch in amd64 arm64; do
  symbols "$arch" ./scripts/testdata/diagnostic-dce wago_no_gc > "$inventory"
  grep -Eq '\.\(\*WasmFunc\)\.Invoke$' "$inventory"
  assert_absent "linux/$arch embed without gc" "$inventory" "$gc_prepared_inductions|$gc_prepared_scalar|$collector_instance_metadata|$collector_invocation_records|$gc_metadata_validation"
  symbols "$arch" ./scripts/testdata/diagnostic-dce "" > "$inventory"
  grep -Eq '\.\(\*WasmFunc\)\.tryInvokePreparedI31$' "$inventory"
  grep -Eq '\.\(\*WasmFunc\)\.tryInvokePreparedDeadInduction$' "$inventory"
  grep -Eq '\.\(\*WasmFunc\)\.invokeGCScalar$' "$inventory"
  printf 'linux/%s prepared GC selection passed\n' "$arch"
done

inventory="$check_dir/symbols"
symbols amd64 ./cli/wago wago_runtime > "$inventory"
assert_absent 'linux/amd64 full runtime' "$inventory" "$rv64_compact_extern_cleanup|$rv64_terminal_close_collection"
grep -Eq '/src/wago\.\(\*gcOptionView\)\.metadata$' "$inventory"
symbols arm64 ./cli/wago wago_runtime > "$inventory"
assert_absent 'linux/arm64 full runtime' "$inventory" "$gc_carrier_elision|$rv64_compact_extern_cleanup|$rv64_terminal_close_collection"
for pattern in "$simd" "$relaxed" "$exceptions" "$threads" "$tailcalls" "$gc_lowering" "$extended_const_arithmetic"; do
  grep -Eq "$pattern" "$inventory"
done
grep -Eq '\.evalCompiledGCConstExpr$' "$inventory"
grep -Eq '\.dispatchGCArrayHelperLocked$' "$inventory"
grep -Eq '\.dispatchGCStructHelperLocked$' "$inventory"
grep -Eq '\.dispatchCollectorTableSetLocked$' "$inventory"
grep -Eq '\.dispatchCollectorTableOpsLocked$' "$inventory"
grep -Eq '\.readV128ConstExpr$' "$inventory"
grep -Eq '\.readConstRefNullType$' "$inventory"
grep -Eq '\.constRefNullASTType$' "$inventory"
LC_ALL=C grep -aqF "$simd_names" "$check_dir/artifact"
LC_ALL=C grep -aqF "$relaxed_names" "$check_dir/artifact"
LC_ALL=C grep -aqF "$simd_help" "$check_dir/artifact"
LC_ALL=C grep -aqF "$gc_help" "$check_dir/artifact"
LC_ALL=C grep -aqF "$gc_heap_help" "$check_dir/artifact"
grep -Eq "$gc_cli" "$inventory"
grep -Eq "$simd_host_probe" "$inventory"
symbols riscv64 ./cli/wago wago_runtime > "$inventory"
grep -Eq '\.releaseRootedExternref$' "$inventory"
grep -Eq '\.dispatchGCFunctionRefTest$' "$inventory"
grep -Eq '/railshot/riscv64\.gcStructBridgeSpecs$' "$inventory"
grep -Eq '/railshot/riscv64\.gcArrayAndConversionBridgeSpecs$' "$inventory"
assert_absent 'linux/riscv64 full runtime' "$inventory" "$gc_carrier_elision"
grep -Eq '\.validateCollectorHostImport$' "$inventory"
grep -Eq '\.signatureTransfersCollectorObjects$' "$inventory"
grep -Eq '\.isGCBridge$' "$inventory"
grep -Eq '\.hasEquivalentLocalGCHeapTypes$' "$inventory"
grep -Eq '\.reconcileGCGlobalRoots$' "$inventory"
grep -Eq '\.installGlobalRootMappings$' "$inventory"
grep -Eq '\.gcHeapRefMatchesValueType$' "$inventory"
grep -Eq '\.collectGenericGCAtBoundary$' "$inventory"
grep -Eq '\.dropStagedGCObjectRoots$' "$inventory"
grep -Eq '\.gcStructConstructorSlotCapacity$' "$inventory"
grep -Eq '\.rv64GCArrayFixedOfficialMetadata$' "$inventory"
grep -Eq '\.rv64GCStructBasicMetadata$' "$inventory"
grep -Eq '\.rv64GCBrOnCastAbstractMetadata$' "$inventory"
grep -Eq '\.rv64GCExtern$' "$inventory"
grep -Eq '\.newGCFrameRootPlanOwned$' "$inventory"
grep -Eq '\.rv64FreshRootedGC$' "$inventory"
grep -Eq '\.validateCompiledGCFrameRoots$' "$inventory"
grep -Eq '/src/wago\.\(\*compiledWriter\)\.gcFrameRoots$' "$inventory"
grep -Eq "$simd_host_probe" "$inventory"
grep -Eq '\.initializeExceptionRoots$' "$inventory"
grep -Eq '\.restoreExceptionHandlers$' "$inventory"
grep -Eq "$rv64_collector_frame_masks" "$inventory"
grep -Eq '\.primitiveReferenceTargetWithTypes$' "$inventory"
grep -Eq '\.abstractCollectorNullInitializer$' "$inventory"
grep -Eq '\.resolveReferenceSignatures$' "$inventory"
grep -Eq '\.validateGCInvocationImport$' "$inventory"
grep -Eq '\.prepareThreadedExecution$' "$inventory"
grep -Eq '\.NativeTailHostThunkWithCertificate$' "$inventory"
grep -Eq '\.rv64StoreNativeTailModule$' "$inventory"
grep -Eq '\.stageNativeTailMetadata$' "$inventory"
grep -Eq '\.prepareNativeGCView$' "$inventory"
grep -Eq '\.releaseGCDomainLocked$' "$inventory"
grep -Eq "$gc_extern_setup" "$inventory"
grep -Eq '\.railshotReleaseInstanceGlobalReferences$' "$inventory"
grep -Eq '\.compileTableInitializers$' "$inventory"
grep -Eq '\.storeExternGlobal$' "$inventory"
grep -Eq '\.internalReferenceBlockType$' "$inventory"
grep -Eq '\.ExternPersistentMutation$' "$inventory"
grep -Eq '\.opaqueExternHelperSlots$' "$inventory"
grep -Eq '\.moduleHasOpaqueExternTable$' "$inventory"
grep -Eq '\.railshotReleasePublicFuncrefArguments$' "$inventory"
grep -Eq '\.lockFuncrefTableTopology$' "$inventory"
grep -Eq '\.hasIncompatibleGCProducer$' "$inventory"
grep -Eq '\.validateAggregateType$' "$inventory"
grep -Eq '\.supportedGCHeap$' "$inventory"
grep -Eq "$gc_source_type_metadata" "$inventory"
grep -Eq "$gc_artifact_type_metadata" "$inventory"
grep -Eq "$gc_wasm_type_metadata" "$inventory"
grep -Eq "$memory64_width_scan" "$inventory"
grep -Eq "$collector_type_admission" "$inventory"
grep -Eq "$rv64_public_function_types" "$inventory"
for arch in amd64 arm64 riscv64; do
  name="linux/$arch runtime without reference types"
  symbols "$arch" ./cli/wago wago_runtime,wago_no_reference_types > "$inventory"
  assert_absent "$name" "$inventory" "$table_reference_validation|$table_reference_runtime|$table_bulk_runtime"
  grep -Eq '\.dispatchCollectorTableSetLocked$' "$inventory"
  printf '%s passed\n' "$name"
done

for arch in amd64 arm64 riscv64; do
  name="linux/$arch runtime without typed function references"
  symbols "$arch" ./cli/wago wago_runtime,wago_no_typed_function_references > "$inventory"
  grep -Eq '\.dispatchCollectorTableSetLocked$' "$inventory"
  assert_absent "$name" "$inventory" "$typed_reference_ops"
  printf '%s passed\n' "$name"
done

for check in \
  amd64/simd amd64/relaxed_simd amd64/gc amd64/tail_call amd64/exception_handling amd64/threads amd64/bulk_memory_operations amd64/memory64 amd64/table64 amd64/nontrapping_float_to_int_conversion amd64/wide_arithmetic \
  arm64/simd arm64/relaxed_simd arm64/gc arm64/tail_call arm64/exception_handling arm64/threads arm64/bulk_memory_operations arm64/memory64 arm64/table64 arm64/nontrapping_float_to_int_conversion arm64/wide_arithmetic \
  riscv64/simd riscv64/relaxed_simd riscv64/gc riscv64/memory64 riscv64/table64 riscv64/tail_call riscv64/exception_handling riscv64/threads riscv64/bulk_memory_operations riscv64/nontrapping_float_to_int_conversion riscv64/wide_arithmetic; do
  arch=${check%/*}
  feature=${check#*/}
  case "$feature" in
    simd) pattern="$simd|$relaxed|$simd_knobs" ;;
    relaxed_simd) pattern=$relaxed ;;
    gc) pattern="$gc_lowering|$gc_compiled_products|$gc_prepared_inductions|$gc_prepared_scalar|$gc_const_expr|$gc_runtime_dispatch|$rv64_gc_bridge_catalog|$gc_legacy_aggregate|$gc_legacy_reference|$gc_knobs|$gc_names|$gc_extern_setup|$gc_type_admission|$gc_source_type_metadata|$gc_artifact_type_metadata|$gc_wire_types|$rv64_aggregate_initializers|$gc_wasm_type_metadata|$gc_call_boundaries|$gc_host_and_heap_binding|$gc_heap_ref_matching|$gc_compiled_initializers|$gc_host_result_journal|$gc_clone_roots"
      if [[ "$arch" == arm64 ]]; then
        pattern+="|$gc_cli|$collector_object_token_index|$collector_argument_roots|$exact_collector_frames"
      else
        pattern+="|$rv64_immediate_collector_type"
      fi
      ;;
    memory64) pattern=$memory64_width_scan ;;
    table64) pattern="$oversized_table64|$table64_admission" ;;
    tail_call) pattern="$tailcalls|$tail_inlining|$tail_knobs|$rv64_tail_storage" ;;
    exception_handling) pattern="$exceptions|$exception_tag_lifetime|$exception_compiled_metadata" ;;
    threads)
      pattern=$threads
      if [[ "$arch" == arm64 ]]; then pattern+="|$thread_knobs"; fi
      ;;
    bulk_memory_operations)
      pattern="$bulk_validation|$table_bulk_runtime"
      if [[ "$arch" == arm64 ]]; then
        pattern+="|$bulk_lowering|$bulk_knobs"
      else
        pattern+="|$rv64_bulk_lowering"
      fi
      ;;
    nontrapping_float_to_int_conversion) pattern=$saturating_float ;;
    wide_arithmetic) pattern=$wide_arithmetic ;;
  esac
  name="linux/$arch runtime without $feature"
  symbols "$arch" ./cli/wago "wago_runtime,wago_no_$feature" > "$inventory"
  assert_absent "$name" "$inventory" "$pattern"
  if [[ "$feature" == threads ]]; then
    assert_names_absent "$name" "$thread_wait_closed"
    if [[ "$arch" != riscv64 ]]; then
      grep -Eq "$native_indexed_memory" "$inventory"
    fi
  fi
  if [[ "$feature" == gc ]]; then
    assert_absent "$name" "$inventory" "$gc_staged_object_cleanup|$gc_constructor_capacity"
    if [[ "$arch" != arm64 ]]; then
      assert_absent "$name" "$inventory" "$rv64_collector_tail_carriers|$rv64_legacy_gc_module"
    fi
    if [[ "$arch" != riscv64 ]]; then
      assert_absent "$name" "$inventory" "$collector_store_topology|$collector_release_batch|$collector_instance_registration|$collector_instance_metadata|$collector_invocation_records|$collector_public_state|$gc_metadata_validation|$gc_helper_recovery"
      assert_names_absent "$name" "$gc_heap_help"
      assert_names_absent "$name" "$gc_helper_diagnostic"
    fi
  fi
  case "$check" in
    amd64/simd) grep -Eq "$simd_host_probe" "$inventory" ;;
    riscv64/simd) assert_absent "$name" "$inventory" "$simd_host_probe" ;;
    arm64/simd)
      assert_absent "$name" "$inventory" "$simd_host_probe"
      assert_names_absent "$name" "$simd_names"
      assert_names_absent "$name" "$relaxed_names"
      assert_names_absent "$name" "$simd_help"
      ;;
    */relaxed_simd)
      grep -Eq "$simd" "$inventory"
      LC_ALL=C grep -aqF "$simd_names" "$check_dir/artifact"
      assert_names_absent "$name" "$relaxed_names"
      ;;
    amd64/gc)
      assert_absent "$name" "$inventory" "$gc_cli|$gc_carrier_elision"
      ;;
    arm64/gc) assert_names_absent "$name" "$gc_help" ;;
    riscv64/gc)
      assert_absent "$name" "$inventory" "$rv64_gc_table"
      grep -Eq '\.gcRefMatchesValueType$' "$inventory"
      ;;
    riscv64/bulk_memory_operations)
      grep -Eq '\.dispatchCompactExternTableWrite$' "$inventory"
      grep -Eq '/compiler/wasm\.\(\*funcValidator\)\.stepReferenceTableInstruction$' "$inventory"
      grep -Eq '/riscv64\..*\.privateTableGrowAt$' "$inventory"
      grep -Eq '\.rv64OwnedTwoTable32Size$' "$inventory"
      assert_absent "$name" "$inventory" "$rv64_bulk_table32"
      ;;
    */nontrapping_float_to_int_conversion)
      grep -Eq '\.fcWide$' "$inventory"
      if [[ "$arch" == riscv64 ]]; then
        # The SIMD lane conversions also use this emitter.
        grep -Eq '\.floatTruncSaturating$' "$inventory"
      fi
      ;;
    */wide_arithmetic) grep -Eq '\.fcSaturating$' "$inventory" ;;
  esac
  if [[ $check == riscv64/gc || $check == riscv64/exception_handling ]]; then
    grep -Eq '\.newGCFrameRootPlanOwned$' "$inventory"
    grep -Eq '\.rv64FreshRootedGC$' "$inventory"
  fi
  printf '%s passed\n' "$name"
done

for arch in amd64 arm64 riscv64; do
  for feature in extended_constant_expressions extended_const_expressions; do
    name="linux/$arch runtime arithmetic with $feature disabled"
    symbols "$arch" ./cli/wago "wago_runtime,wago_no_$feature" > "$inventory"
    grep -Eq '\.evalScalarConstExprProgram$' "$inventory"
    printf '%s passed\n' "$name"
  done
  name="linux/$arch runtime without extended constant arithmetic"
  symbols "$arch" ./cli/wago wago_runtime,wago_no_extended_constant_expressions,wago_no_extended_const_expressions > "$inventory"
  if [[ "$arch" == riscv64 ]]; then
    grep -Eq '\.evalCompiledI31ConstExpr$' "$inventory"
    grep -Eq '\.evalCompiledGCConstExpr$' "$inventory"
    grep -Eq '\.validateGCConstExpr$' "$inventory"
    grep -Eq '\.evalCompiledIntegerOperand$' "$inventory"
  fi
  assert_absent "$name" "$inventory" "$extended_const_arithmetic"
  printf '%s passed\n' "$name"
done

symbols riscv64 ./cli/wago wago_runtime,wago_no_gc,wago_no_exception_handling > "$inventory"
grep -Eq '\.rv64PublicHostReferenceBoundaryWithPlan$' "$inventory"
grep -Eq '\.rv64MutableFuncrefGlobalLocalCall$' "$inventory"
grep -Eq '\.rv64FreezeGlobalReferences$' "$inventory"
grep -Eq '/frontend\.lowerGCTypeMetadata$' "$inventory"
grep -Eq '/gc/native\.ValidateFunctionTypeDescs$' "$inventory"
grep -Eq '\.referenceCall$' "$inventory"
grep -Eq '\.storePrivateReference$' "$inventory"
grep -Eq '\.dispatchSyncHostReference$' "$inventory"
grep -Eq '\.validateReferenceImportWithCollector$' "$inventory"
grep -Eq "$rv64_reference_initializers" "$inventory"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$gc_cli"
assert_names_absent 'linux/riscv64 externref-only runtime' "$gc_heap_help"
assert_absent 'linux/riscv64 runtime with reference types but without exception handling' "$inventory" "$exceptions|$exception_tag_lifetime|$exception_compiled_metadata|$rv64_aggregate_initializers|$gc_const_expr|$gc_runtime_dispatch|$rv64_gc_bridge_catalog|$gc_legacy_aggregate|$gc_legacy_reference|$exact_collector_frames|$rv64_rooted_receipt_type|\.dispatchCollectorTable(Set|Ops)Locked$"
assert_absent 'linux/riscv64 runtime with reference types but without GC or exception handling' "$inventory" "$collector_object_tokens|$collector_object_token_index|$collector_argument_roots|$gc_clone_roots|$parked_host_object_roots|$gc_call_boundaries|$gc_object_global_roots"
assert_absent 'linux/riscv64 runtime with reference types but without GC or exception handling' "$inventory" "$gc_heap_ref_matching|$collector_ref_matching"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" '/src/wago\.(releaseGCResultRootMetadata(With)?|clearGCResultRootSlot|releaseRootedExternref|\(\*gcResultReleaseBatch\)\.releaseGCResultRoots)([.-].*)?$'
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$gc_host_result_journal|$collector_host_token_journal|/src/wago\.\(\*gcResultSlots\)\.(root|publish|nextResultSlot|appendResultRootSlot)([.-].*)?$"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$gc_staged_object_cleanup|$gc_constructor_capacity|$gc_compiled_initializers|$gc_compiled_products"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$rv64_backend_frame_roots|$rv64_collector_frame_masks|$gc_host_activations"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$rv64_collector_tail_carriers"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$rv64_legacy_gc_module"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" '/compiler/wasm\.(AnyRef|EqRef|I31Ref|StringRef)$'
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" '\.rv64RootedInternalFunctionContract([.-].*)?$'
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$collector_public_state|\.(compactExternHostAccessRoots|compactExternHost(Global|Table)Row|railshot(Set|Attach|TryClose)CompactExternHost(Global|Table))([.-].*)?$"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$collector_construction|$rv64_collector_construction|$collector_instance_metadata|\.(cachedGCTypeMapping|rememberGCTypeMapping)([.-].*)?$"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$collector_release_batch|$collector_teardown"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$gc_metadata_validation"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$rv64_collector_reference_lifetime"
assert_absent 'linux/riscv64 externref-only runtime' "$inventory" "$collector_import_domains|$collector_store_topology|$collector_instance_registration|$collector_invocation_records|$collector_instance_lifecycle"
printf '%s\n' 'linux/riscv64 managed references with reference types passed'
symbols riscv64 ./cli/wago wago_runtime,wago_no_gc,wago_no_exception_handling,wago_no_typed_function_references > "$inventory"
grep -Eq '/compiler/wasm\.\(\*funcValidator\)\.stepReferenceInstruction$' "$inventory"
assert_absent 'linux/riscv64 reference types without typed function references' "$inventory" '/compiler/wasm\.(AnyRef|EqRef|I31Ref|StringRef|\(\*funcValidator\)\.(stepGC|stepCallRef|stepTypedReferenceInstruction))([.-].*)?$'
printf '%s\n' 'linux/riscv64 reference types without typed function references passed'
symbols riscv64 ./cli/wago wago_runtime,wago_no_gc,wago_no_exception_handling,wago_no_reference_types > "$inventory"
grep -Eq '\.finishLogicalCloseCollectionHandoff$' "$inventory"
grep -Eq '/compiler/wasm\.\(\*funcValidator\)\.stepBulkMemoryInstruction$' "$inventory"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$table_reference_validation|$table_reference_runtime|$table_bulk_runtime"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$gc_metadata_validation"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$rv64_compact_extern|$rv64_compact_extern_cleanup"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$rv64_gc_roots|$rv64_collector_frame_masks|$exact_collector_frames"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$collector_import_domains|$collector_store_topology|$collector_release_batch|$collector_instance_registration|$collector_invocation_records"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$collector_instance_lifecycle|$collector_instance_metadata|$collector_public_state|$gc_extern_setup"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$opaque_extern_planning|$gc_funcref_topology"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$collector_type_admission"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$rv64_function_environments"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$rv64_indexed_table32|$rv64_core3_references"
assert_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$inventory" "$gc_cli"
assert_names_absent 'linux/riscv64 runtime without reference types, GC or exception handling' "$gc_heap_help"
grep -Eq '\.rv64ValidatePlainReferenceTableDirectory$' "$inventory"
printf '%s\n' 'linux/riscv64 managed references disabled passed'

tail_tags=${mvp_tags/,wago_no_tail_call/}
test "$tail_tags" != "$mvp_tags"
symbols riscv64 ./cli/wago "wago_runtime,$tail_tags" > "$inventory"
grep -Eq '\.rv64ValidateNativeTailPlansWithCodeBytes$' "$inventory"
grep -Eq '\.dynamicIndirectTailCall$' "$inventory"
grep -Eq '\.indirectCallKeys$' "$inventory"
grep -Eq "$rv64_function_carriers" "$inventory"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$rv64_reference_validation|$rv64_primitive_declarations|$rv64_reference_boundaries|$rv64_reference_runtime"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$rv64_indexed_table32|$rv64_core3_references|$rv64_gc_table|\.rv64ValidatePlainReferenceTableDirectory$"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$rv64_reference_globals|$rv64_reference_controls|$gc_frame_planning|$exact_collector_frames|$rv64_rooted_receipt_type"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$rv64_reference_emitters|$reference_const_expr"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$reference_import_dispatch"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$rv64_reference_initializers|$rv64_collector_frame_masks|$exceptions"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$rv64_reference_type_resolution|$collector_import_domains|$collector_store_topology|$collector_release_batch|$collector_instance_registration|$collector_invocation_records"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$collector_instance_lifecycle|$collector_instance_metadata|$collector_public_state|$gc_extern_setup|$rv64_frozen_global_cleanup"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$reference_table_init|$rv64_reference_lowering|$rv64_immediate_collector_type"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$opaque_extern_planning|$reference_argument_scopes"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$gc_funcref_topology"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$gc_metadata_lowering|$gc_compiled_initializers|$gc_metadata_validation|$gc_source_type_metadata|$gc_artifact_type_metadata|$gc_wire_types|$rv64_aggregate_initializers|$gc_wasm_type_metadata"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$indexed_type_graph|$reference_value_parser|$reference_subtype_bridge|$gc_helper_recovery"
assert_names_absent 'linux/riscv64 Core 1.0 with tail calls' "$gc_helper_diagnostic"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$gc_type_admission|$collector_type_admission|$rv64_public_function_types"
assert_absent 'linux/riscv64 Core 1.0 with tail calls' "$inventory" "$gc_cli"
printf '%s\n' 'linux/riscv64 Core 1.0 with tail calls passed'

symbols riscv64 ./cli/wago wago_runtime,wago_no_reference_types,wago_no_typed_function_references,wago_no_gc,wago_no_exception_handling,wago_no_tail_call > "$inventory"
grep -Eq "$simd" "$inventory"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$rv64_primitive_declarations|$rv64_function_carriers|$rv64_reference_boundaries|$rv64_reference_runtime"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$rv64_indexed_table32|$rv64_core3_references|$rv64_gc_table"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$rv64_reference_globals|$rv64_reference_controls|$gc_frame_planning|$exact_collector_frames|$rv64_rooted_receipt_type"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$rv64_reference_emitters|$reference_const_expr"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$reference_import_dispatch"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$rv64_reference_initializers|$rv64_collector_frame_masks|$exceptions"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$rv64_reference_type_resolution|$collector_import_domains|$collector_store_topology|$collector_release_batch|$collector_instance_registration|$collector_invocation_records"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$collector_instance_lifecycle|$collector_instance_metadata|$collector_public_state|$gc_extern_setup|$rv64_frozen_global_cleanup"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$reference_table_init|$rv64_reference_lowering|$rv64_immediate_collector_type"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$opaque_extern_planning|$reference_argument_scopes"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$gc_funcref_topology"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$rv64_tail_storage"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$gc_metadata_lowering|$gc_compiled_initializers|$gc_metadata_validation|$gc_source_type_metadata|$gc_artifact_type_metadata|$gc_wire_types|$rv64_aggregate_initializers|$gc_wasm_type_metadata"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$indexed_type_graph|$reference_value_parser|$reference_subtype_bridge|$rv64_extended_host_signature|$gc_helper_recovery"
assert_names_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$gc_helper_diagnostic"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$gc_type_admission|$collector_type_admission|$rv64_public_function_types"
assert_absent 'linux/riscv64 with SIMD and without reference signatures or tail calls' "$inventory" "$gc_cli"
printf '%s\n' 'linux/riscv64 SIMD without reference signatures or tail calls passed'

# TinyGo uses the selected host toolchain. Run this check with a Go version
# supported by the installed TinyGo release.
if [[ ${1:-} == --tinygo ]]; then
  tinygo_command=(tinygo)
  if [[ -n ${WAGO_TINYGO_GOROOT:-} ]]; then
    tinygo_command=(env "GOROOT=$WAGO_TINYGO_GOROOT" GOTOOLCHAIN=local "PATH=$WAGO_TINYGO_GOROOT/bin:$PATH" tinygo)
  fi
  tinygo_flags=(-scheduler=tasks -stack-size=256KB -no-debug -opt=z -gc=conservative)
  tinygo_tags=wago_lean,wago_minimal
  tinygo_symbols() {
    local tags=$1 package=$2
    "${tinygo_command[@]}" build "${tinygo_flags[@]}" -tags "$tinygo_tags,$tags" -o "$check_dir/artifact" "$package"
    artifact_symbols
  }
  tinygo_symbols wago_runtime ./cli/wago > "$inventory"
  grep -Eq '\.fcSaturating$' "$inventory"
  grep -Eq "$collector" "$inventory"
  LC_ALL=C grep -aqF "$simd_names" "$check_dir/artifact"

  for feature in tail_call typed_function_references compact_imports; do
    case "$feature" in
      tail_call) pattern="$tailcalls|$tail_inlining|$tail_knobs|$rv64_tail_storage" ;;
      typed_function_references) pattern=$typed_reference_ops ;;
      compact_imports) pattern=$compact_imports ;;
    esac
    name="TinyGo runtime without $feature"
    tinygo_symbols "wago_runtime,wago_no_$feature" ./cli/wago > "$inventory"
    grep -Eq "$collector" "$inventory"
    LC_ALL=C grep -aqF "$simd_names" "$check_dir/artifact"
    assert_absent "$name" "$inventory" "$pattern"
    printf '%s passed\n' "$name"
  done

  for flavor in runtime embed; do
    package=./cli/wago
    tags="wago_runtime,$mvp_tags"
    if [[ "$flavor" == embed ]]; then
      package=./scripts/testdata/diagnostic-dce
      tags="$mvp_tags"
    fi
    tinygo_symbols "$tags" "$package" > "$inventory"
    assert_core1 "TinyGo $flavor Core 1.0" "$inventory"
  done
fi
