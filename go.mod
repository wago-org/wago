module github.com/wago-org/wago

go 1.22

require golang.org/x/sys v0.30.0

// Legacy tagged Canary builds. Canary is now distributed only as
// commit-addressed GitHub Actions artifacts.
retract (
	v0.1.0-canary.g014a2fe
	v0.1.0-canary.g1087001
	v0.1.0-canary.g12ccfd4
	v0.1.0-canary.g2025c38
	v0.1.0-canary.g215b022
	v0.1.0-canary.g363fadf
	v0.1.0-canary.g3c96977
	v0.1.0-canary.g3ca7525
	v0.1.0-canary.g4d6c627
	v0.1.0-canary.g470442d792991f50c397d62ac6f7dfc06700a0b8
	v0.1.0-canary.g666cd54
	v0.1.0-canary.g709494c
	v0.1.0-canary.g7b3efce
	v0.1.0-canary.g81feb65
	v0.1.0-canary.g8ae8468
	v0.1.0-canary.gbd742cd
	v0.1.0-canary.gdd58578
	v0.1.0-canary.ge844da4
)
