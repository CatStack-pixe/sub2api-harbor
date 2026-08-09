package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type proxyGroupRepositoryStub struct {
	groups               []ProxyGroup
	getByID              *ProxyGroup
	getByName            *ProxyGroup
	getByNameAfterCreate *ProxyGroup
	createErr            error
	updateErr            error
	deleteErr            error
	batchErr             error
	created              *ProxyGroup
	updated              *ProxyGroup
	deletedID            int64
	batchIDs             []int64
	batchGroupID         *int64
	batchUpdated         int64
	batchCallCount       int
	getByNameCalls       int
	createCallCount      int
}

func (s *proxyGroupRepositoryStub) List(context.Context) ([]ProxyGroup, error) {
	return s.groups, nil
}

func (s *proxyGroupRepositoryStub) GetByID(context.Context, int64) (*ProxyGroup, error) {
	if s.getByID == nil {
		return nil, ErrProxyGroupNotFound
	}
	return s.getByID, nil
}

func (s *proxyGroupRepositoryStub) GetByName(context.Context, string) (*ProxyGroup, error) {
	s.getByNameCalls++
	if s.getByNameCalls > 1 && s.getByNameAfterCreate != nil {
		return s.getByNameAfterCreate, nil
	}
	return s.getByName, nil
}

func (s *proxyGroupRepositoryStub) Create(_ context.Context, group *ProxyGroup) error {
	s.createCallCount++
	s.created = group
	if s.createErr != nil {
		return s.createErr
	}
	group.ID = 42
	return nil
}

func (s *proxyGroupRepositoryStub) Update(_ context.Context, group *ProxyGroup) error {
	s.updated = group
	return s.updateErr
}

func (s *proxyGroupRepositoryStub) DeleteAndUnassign(_ context.Context, id int64) error {
	s.deletedID = id
	return s.deleteErr
}

func (s *proxyGroupRepositoryStub) BatchAssign(_ context.Context, ids []int64, groupID *int64) (int64, error) {
	s.batchCallCount++
	s.batchIDs = append([]int64(nil), ids...)
	if groupID != nil {
		value := *groupID
		s.batchGroupID = &value
	}
	return s.batchUpdated, s.batchErr
}

func TestAdminProxyGroupCreateNormalizesName(t *testing.T) {
	t.Parallel()

	repo := &proxyGroupRepositoryStub{}
	service := &adminServiceImpl{proxyGroupRepo: repo}
	group, err := service.CreateProxyGroup(context.Background(), "  Primary  ")
	require.NoError(t, err)
	require.Equal(t, int64(42), group.ID)
	require.Equal(t, "Primary", group.Name)
	require.Same(t, group, repo.created)
}

func TestAdminProxyGroupRejectsInvalidNames(t *testing.T) {
	t.Parallel()

	service := &adminServiceImpl{proxyGroupRepo: &proxyGroupRepositoryStub{}}
	_, err := service.CreateProxyGroup(context.Background(), "   ")
	require.Error(t, err)

	_, err = service.CreateProxyGroup(context.Background(), string(make([]rune, 101)))
	require.Error(t, err)
}

func TestAdminProxyGroupGetOrCreateRetriesCaseInsensitiveConflict(t *testing.T) {
	t.Parallel()

	existing := &ProxyGroup{ID: 7, Name: "Primary"}
	repo := &proxyGroupRepositoryStub{
		createErr:            ErrProxyGroupExists,
		getByNameAfterCreate: existing,
	}
	service := &adminServiceImpl{proxyGroupRepo: repo}

	group, err := service.GetOrCreateProxyGroupByName(context.Background(), " primary ")
	require.NoError(t, err)
	require.Same(t, existing, group)
	require.Equal(t, 2, repo.getByNameCalls)
	require.Equal(t, 1, repo.createCallCount)
}

func TestAdminProxyGroupBatchDeduplicatesIDs(t *testing.T) {
	t.Parallel()

	groupID := int64(9)
	repo := &proxyGroupRepositoryStub{batchUpdated: 2}
	service := &adminServiceImpl{proxyGroupRepo: repo}

	updated, err := service.BatchGroupProxies(context.Background(), []int64{3, 2, 3, 2}, &groupID)
	require.NoError(t, err)
	require.Equal(t, int64(2), updated)
	require.Equal(t, []int64{3, 2}, repo.batchIDs)
	require.NotNil(t, repo.batchGroupID)
	require.Equal(t, groupID, *repo.batchGroupID)
}

func TestAdminProxyGroupBatchValidatesBeforeRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ids     []int64
		groupID *int64
	}{
		{name: "empty", ids: nil},
		{name: "invalid proxy id", ids: []int64{1, 0}},
		{name: "invalid group id", ids: []int64{1}, groupID: pointerToInt64(0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &proxyGroupRepositoryStub{}
			service := &adminServiceImpl{proxyGroupRepo: repo}
			_, err := service.BatchGroupProxies(context.Background(), tc.ids, tc.groupID)
			require.Error(t, err)
			require.Zero(t, repo.batchCallCount)
		})
	}

	ids := make([]int64, maxBatchGroupProxyIDs+1)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	repo := &proxyGroupRepositoryStub{}
	service := &adminServiceImpl{proxyGroupRepo: repo}
	_, err := service.BatchGroupProxies(context.Background(), ids, nil)
	require.Error(t, err)
	require.Zero(t, repo.batchCallCount)
}

func pointerToInt64(value int64) *int64 {
	return &value
}
