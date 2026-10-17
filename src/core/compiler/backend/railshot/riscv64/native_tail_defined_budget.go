//go:build linux && !tinygo

package riscv64

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// NativeTailDefinedFactoryBudget checks already constructed opaque snapshots.
// Runtime admission uses the equivalent count-only helper before constructing
// snapshots or per-site slices. Charges cover staging and factory freeze, and
// remain reserved until successful physical cleanup of a generation.
func NativeTailDefinedFactoryBudget(sites, values, continuations int, owners []*NativeTailDefinedOwner, guestCodeBytes int) (metadata, code int, err error) {
	if len(owners) == 0 || len(owners) > tailRAMaxRanges {
		return 0, 0, unsupported("native defined factory owner count")
	}
	var retained [tailRAMaxRanges]tailRADynamicOwner
	for i, owner := range owners {
		if owner == nil {
			return 0, 0, unsupported("native defined factory missing owner")
		}
		retained[i] = owner.owner
	}
	d := tailRADynamicSite{owners: retained[:len(owners)]}
	dynamicMetadata, dynamicCode, err := d.budget()
	if err != nil {
		return 0, 0, err
	}
	return nativeTailDefinedFactoryBytes(sites, values, continuations, len(owners), dynamicMetadata, dynamicCode, guestCodeBytes)
}

// NativeTailDefinedFactoryCountsBudget charges genuine defined owners whose
// signatures and parallel keys will be frozen by NewNativeTailDefinedOwner.
// The runtime separately charges their initial snapshots through
// NativeTailDefinedOwnerBytes. No signature conversion or allocation is needed.
func NativeTailDefinedFactoryCountsBudget(sites, values, continuations, owners, functions, ownerValues, guestCodeBytes int) (metadata, code int, err error) {
	return NativeTailFactoryCountsBudget(sites, values, continuations, NativeTailOwnerCounts{Owners: owners, Functions: functions, Values: ownerValues}, guestCodeBytes)
}

// NativeTailOwnerCounts separates canonical rows from optional native proxy
// targets. Counts describe the exact arrays frozen into each factory site.
type NativeTailOwnerCounts struct {
	Owners, Functions, Values int
	Targets, TargetValues     int
	ProxySlots, Proxies       int
	HostSlots                 int
	MaskSlots, NonCallable    int // physical source rows; signature storage excludes holes
	TargetNonCallable         int // paired target holes exist only on imported owners
}

func NativeTailFactoryCountsBudget(sites, values, continuations int, counts NativeTailOwnerCounts, guestCodeBytes int) (metadata, code int, err error) {
	c := counts
	if c.Owners <= 0 || c.Owners > tailRAMaxRanges || c.Functions < c.Owners || c.Functions > MaxFunctions || c.MaskSlots < 0 || c.MaskSlots > c.Functions || c.NonCallable < 0 || c.NonCallable > c.MaskSlots || c.Values < 0 || c.Values > 512*(c.Functions-c.NonCallable) || c.Targets < 0 || c.Targets > c.Functions || c.TargetNonCallable < 0 || c.TargetNonCallable > c.Targets || c.TargetNonCallable > c.NonCallable || c.TargetValues < 0 || c.TargetValues > 512*(c.Targets-c.TargetNonCallable) || c.ProxySlots < 0 || c.ProxySlots > c.Targets || c.Proxies < 0 || c.Proxies > c.ProxySlots || c.Proxies > c.Targets-c.TargetNonCallable || c.HostSlots < 0 || c.HostSlots > c.Functions {
		return 0, 0, unsupported("native factory aggregate owner counts")
	}
	dynamicMetadata := int(unsafe.Sizeof(tailRADynamicSite{})) + c.Owners*int(unsafe.Sizeof(tailRADynamicOwner{})) + (c.Functions+c.Targets)*int(unsafe.Sizeof(tailRAFunction{})) + (c.Functions-c.NonCallable+c.Targets-c.TargetNonCallable)*int(unsafe.Sizeof(wasm.CompType{})) + c.Functions*8 + c.MaskSlots*int(unsafe.Sizeof(false)) + (c.Values+c.TargetValues)*int(unsafe.Sizeof(wasm.ValType{})) + c.ProxySlots*int(unsafe.Sizeof((*tailRADynamicProxy)(nil))) + c.Proxies*int(unsafe.Sizeof(tailRADynamicProxy{})) + c.HostSlots*int(unsafe.Sizeof((*NativeTailHostFunction)(nil)))
	dynamicCode := 2048 + 1024*(c.Owners+c.Functions)
	if dynamicMetadata > MaxModuleBytes || dynamicCode > MaxCodeBytes {
		return 0, 0, unsupported("native factory aggregate owner bytes")
	}
	return nativeTailDefinedFactoryBytes(sites, values, continuations, c.Owners, dynamicMetadata, dynamicCode, guestCodeBytes)
}

func nativeTailDefinedFactoryBytes(sites, values, continuations, owners, dynamicMetadata, dynamicCode, guestCodeBytes int) (metadata, code int, err error) {
	if sites <= 0 || sites > tailRAMaxSites || values < 0 || values > 512*sites || continuations <= 0 || continuations > tailRAMaxRanges || guestCodeBytes <= 0 || guestCodeBytes > MaxCodeBytes {
		return 0, 0, unsupported("native defined factory aggregate count")
	}
	// Check using division before products, including on 32-bit host encoders.
	base := 2 * tailRACapHeader
	if values > (MaxModuleBytes-base)/32 {
		return 0, 0, unsupported("native defined factory aggregate signature bytes")
	}
	base += 32 * values
	// Every site borrows the same frozen owner graph. Charge its payload once,
	// and retain the per-site staging owner headers and site-local metadata.
	if dynamicMetadata > (MaxModuleBytes-base)/2 {
		return 0, 0, unsupported("native defined factory shared owner byte bound")
	}
	base += 2 * dynamicMetadata
	ownerHeaders := int(unsafe.Sizeof(tailRADynamicSite{})) + owners*int(unsafe.Sizeof(tailRADynamicOwner{}))
	perSite := 2*(tailRASiteRow+128+16*continuations+ownerHeaders) + int(unsafe.Sizeof(NativeTailSite{})) + 2*int(unsafe.Sizeof(nativeTailDynamicLease{}))
	codePerSite := tailRADispatchBound + 128*continuations + dynamicCode
	if perSite > (MaxModuleBytes-base)/sites || codePerSite > (MaxCodeBytes-guestCodeBytes)/sites {
		return 0, 0, unsupported("native defined factory aggregate byte bound")
	}
	return base + sites*perSite, sites * codePerSite, nil
}
