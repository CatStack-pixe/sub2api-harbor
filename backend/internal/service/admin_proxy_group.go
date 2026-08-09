package service

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const maxBatchGroupProxyIDs = 5000

func (s *adminServiceImpl) ListProxyGroups(ctx context.Context) ([]ProxyGroup, error) {
	return s.proxyGroupRepo.List(ctx)
}

func (s *adminServiceImpl) CreateProxyGroup(ctx context.Context, name string) (*ProxyGroup, error) {
	name, err := normalizeProxyGroupName(name)
	if err != nil {
		return nil, err
	}
	group := &ProxyGroup{Name: name}
	if err := s.proxyGroupRepo.Create(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (s *adminServiceImpl) UpdateProxyGroup(ctx context.Context, id int64, name string) (*ProxyGroup, error) {
	if id <= 0 {
		return nil, ErrProxyGroupNotFound
	}
	name, err := normalizeProxyGroupName(name)
	if err != nil {
		return nil, err
	}
	group := &ProxyGroup{ID: id, Name: name}
	if err := s.proxyGroupRepo.Update(ctx, group); err != nil {
		return nil, err
	}
	return group, nil
}

func (s *adminServiceImpl) DeleteProxyGroup(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrProxyGroupNotFound
	}
	return s.proxyGroupRepo.DeleteAndUnassign(ctx, id)
}

func (s *adminServiceImpl) GetOrCreateProxyGroupByName(ctx context.Context, name string) (*ProxyGroup, error) {
	name, err := normalizeProxyGroupName(name)
	if err != nil {
		return nil, err
	}
	group, err := s.proxyGroupRepo.GetByName(ctx, name)
	if err != nil || group != nil {
		return group, err
	}
	group = &ProxyGroup{Name: name}
	if err := s.proxyGroupRepo.Create(ctx, group); err != nil {
		if errors.Is(err, ErrProxyGroupExists) {
			return s.proxyGroupRepo.GetByName(ctx, name)
		}
		return nil, err
	}
	return group, nil
}

func (s *adminServiceImpl) BatchGroupProxies(ctx context.Context, ids []int64, proxyGroupID *int64) (int64, error) {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return 0, infraerrors.BadRequest("PROXY_IDS_INVALID", "proxy ids must be positive")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return 0, infraerrors.BadRequest("PROXY_IDS_REQUIRED", "at least one proxy id is required")
	}
	if len(unique) > maxBatchGroupProxyIDs {
		return 0, infraerrors.BadRequest("PROXY_IDS_LIMIT", "at most 5000 unique proxy ids are allowed")
	}
	if proxyGroupID != nil && *proxyGroupID <= 0 {
		return 0, ErrProxyGroupNotFound
	}
	return s.proxyGroupRepo.BatchAssign(ctx, unique, proxyGroupID)
}

func normalizeProxyGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", infraerrors.BadRequest("PROXY_GROUP_NAME_REQUIRED", "proxy group name is required")
	}
	if utf8.RuneCountInString(name) > 100 {
		return "", infraerrors.BadRequest("PROXY_GROUP_NAME_TOO_LONG", "proxy group name must be at most 100 characters")
	}
	return name, nil
}
