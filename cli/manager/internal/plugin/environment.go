package plugin

type pluginEnvironment struct {
	selection    pluginRuntimeSelection
	scope        string
	manifestDir  string
	buildDir     string
	dependencies []string
}
