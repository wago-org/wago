package plugin

type pluginEnvironment struct {
	scope        string
	manifestDir  string
	buildDir     string
	dependencies []string
}
