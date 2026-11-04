//go:build wago_standalone

package wago

func (c *Compiled) installCodeProfile(cfg *RuntimeConfig, _ []byte, _ *railshotModuleStats) error {
	return c.AttachCodeProfile(cfg.codeProfile)
}
