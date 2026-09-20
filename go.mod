module github.com/wago-org/wago

go 1.22

require golang.org/x/sys v0.30.0

retract (
	// Canary tags are discontinued. Use commit hashes for development builds.
	// Includes the one-time cleanup version carrying these retractions.
	[v0.1.0-canary, v0.1.0-canary.retracted]

	// Temporary test release; not intended for use.
	v0.0.0-test.1
)
