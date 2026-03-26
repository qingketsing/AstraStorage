package client

// Group identifies one logical Redis replication group.
type Group string

const (
	GroupCache Group = "cache"
	GroupCoord Group = "coord"
)
