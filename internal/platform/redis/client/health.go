package client

// HealthSummary captures a lightweight view of Redis client wiring state.
type HealthSummary struct {
	Group             Group
	MasterSetName     string
	SentinelEndpoints []string
}
