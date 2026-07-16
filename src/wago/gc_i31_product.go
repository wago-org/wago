package wago

import (
	"crypto/sha256"
	"fmt"
)

// stagedGCI31Product identifies exact compile-only gc/i31 products. i31 values
// share the compact gc.Ref word internally, but this product marker is separate
// from collector-backed struct/array ownership and is never serialized.
type stagedGCI31Product uint8

const (
	stagedGCI31ProductCore stagedGCI31Product = iota + 1
	stagedGCI31ProductTable
	stagedGCI31ProductTableGlobalInitializer
	stagedGCI31ProductGlobalInitializer
	stagedGCI31ProductAnyGlobal
	stagedGCI31ProductAnyTable
)

func (p stagedGCI31Product) String() string {
	switch p {
	case stagedGCI31ProductCore:
		return "core"
	case stagedGCI31ProductTable:
		return "i31-table"
	case stagedGCI31ProductTableGlobalInitializer:
		return "table-global-initializer"
	case stagedGCI31ProductGlobalInitializer:
		return "global-global-initializer"
	case stagedGCI31ProductAnyGlobal:
		return "anyref-global"
	case stagedGCI31ProductAnyTable:
		return "anyref-table"
	default:
		return "unknown"
	}
}

func stagedGCI31PinnedProduct(data []byte) (stagedGCI31Product, bool) {
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	switch {
	case len(data) == 252 && digest == "4bdd4d0f186a2fd617b41ad4940e17f2c0415514ebc636a56e41496e8c392aea":
		return stagedGCI31ProductCore, true
	case len(data) == 259 && digest == "c2a2062022d3b99a27aada76d1fe14cdacf3387e7497943d17d3681f65ed7329":
		return stagedGCI31ProductTable, true
	case len(data) == 96 && digest == "0a26e50d6ec8ccbaf1cc3a59fc2e1be6dca2c22219ba70ca80a01451882bf0e4":
		return stagedGCI31ProductTableGlobalInitializer, true
	case len(data) == 88 && digest == "024b9a334c9a7bb6933243fa1eaf60ed653338034bfaf9e54fb23ccc13c9ad87":
		return stagedGCI31ProductGlobalInitializer, true
	case len(data) == 131 && digest == "757d3266617ea901221facbda9b660d6d1fd52adf492ffa82ba0658dc846b26d":
		return stagedGCI31ProductAnyGlobal, true
	case len(data) == 262 && digest == "572387a2c9d7ea9112f3940025b7c57041cd9478185ed7e32bb93a01fbfa5a69":
		return stagedGCI31ProductAnyTable, true
	default:
		return 0, false
	}
}

func stagedGCI31ExecutionProduct(data []byte) (stagedGCI31Product, bool) {
	product, ok := stagedGCI31PinnedProduct(data)
	return product, ok && product == stagedGCI31ProductCore
}
