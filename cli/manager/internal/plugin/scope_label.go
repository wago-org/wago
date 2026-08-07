package plugin

func ScopeLabel() string {
	return selectedPluginScopeLabel()
}

func selectedPluginScopeLabel() string {
	environment, err := resolvePluginEnvironment()
	if err != nil {
		return "active scope"
	}
	if environment.scope == "plain" {
		return "global"
	}
	return environment.scope
}
