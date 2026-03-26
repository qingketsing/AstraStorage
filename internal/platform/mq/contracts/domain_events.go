package contracts

type FileChangedEvent struct {
	FileID string `json:"file_id"`
	Action string `json:"action"`
}

type NodeChangedEvent struct {
	NodeID string `json:"node_id"`
	Action string `json:"action"`
}
