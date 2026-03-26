package client

func newHealthSummary(group Group, masterSet string, sentinels []string) HealthSummary {
	return HealthSummary{
		Group:             group,
		MasterSetName:     masterSet,
		SentinelEndpoints: append([]string(nil), sentinels...),
	}
}
