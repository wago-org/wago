package wago

func cachedSIMDHostFeatures() bool { return detectSIMDHostFeatures() }

func hostSupportsSIMD() bool { return simdHostFeaturesSupported() }

func cachedBitCountHostFeatures() uint8 { return architectureAMD64BitCountFeatures() }

func detectSIMDHostFeatures() bool { return architectureSupportsSIMD() }
