package client

// WithDefaults exposes default filling for other Redis subpackages while keeping
// the actual default rules centralized in the client package.
func (c ReplicationGroupConfig) WithDefaults() ReplicationGroupConfig {
	return c.withDefaults()
}

// WithDefaults exposes warmup defaults for other packages that schedule Redis
// warmup work without needing to duplicate the defaulting rules.
func (c WarmupConfig) WithDefaults() WarmupConfig {
	return c.withDefaults()
}
