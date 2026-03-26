package cache

import "fmt"

func FileMetaKey(fileID string) string {
	return "astra:cache:file:meta:" + fileID
}

func FileChunksKey(fileID string) string {
	return "astra:cache:file:chunks:" + fileID
}

func DownloadPlanKey(fileID string) string {
	return "astra:cache:download:plan:" + fileID
}

func DirectoryListKey(inodeID string, offset, limit int) string {
	return fmt.Sprintf("astra:cache:dir:list:%s:%d:%d", inodeID, offset, limit)
}

func DirectoryListVersionKey(inodeID string) string {
	return "astra:cache:dir:list:version:" + inodeID
}

func NodeHealthKey(nodeID string) string {
	return "astra:cache:node:health:" + nodeID
}

func HealthyNodesKey() string {
	return "astra:cache:nodes:healthy"
}

func NullFileKey(fileID string) string {
	return "astra:cache:null:file:" + fileID
}

func FileBloomKey() string {
	return "astra:cache:bf:file"
}

func HotFileSetKey() string {
	return "astra:cache:hot:file"
}

func HotDirectorySetKey() string {
	return "astra:cache:hot:dir"
}

func HotNodeSetKey() string {
	return "astra:cache:hot:node"
}
