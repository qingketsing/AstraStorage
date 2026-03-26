package mds

import (
	"errors"

	"AstraStorage/internal/mds/store"
)

// Service 把多个 store 操作组织成更高层的业务用例。
// 当前先提供最小闭环：建目录、建文件、启动上传和基础查询。
type Service struct {
	repo      store.Repository
	readCache ReadCache
}

// NewService 创建一个基于 Repository 的 MDS 业务服务。
func NewService(repo store.Repository) (*Service, error) {
	if repo == nil {
		return nil, errors.New("mds: repository is nil")
	}
	return &Service{repo: repo}, nil
}

func (s *Service) SetReadCache(cache ReadCache) {
	if s == nil {
		return
	}
	s.readCache = cache
}

// Repository 暴露底层仓储引用，便于测试和上层组装时复用。
func (s *Service) Repository() store.Repository {
	return s.repo
}
