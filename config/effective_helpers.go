package config

// ApplyDefaults normalizes a config using the same defaulting rules as file
// parsing. It is intended for effective-config resolution paths that construct
// configs programmatically instead of loading YAML directly.
func (c *Config) ApplyDefaults() {
	if c == nil {
		return
	}
	c.applyDefaults()
}
