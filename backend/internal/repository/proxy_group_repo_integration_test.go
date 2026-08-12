//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type ProxyGroupRepoSuite struct {
	suite.Suite
	ctx       context.Context
	tx        *dbent.Tx
	groupRepo *proxyGroupRepository
	proxyRepo *proxyRepository
}

func (s *ProxyGroupRepoSuite) SetupTest() {
	s.ctx = context.Background()
	s.tx = testEntTx(s.T())
	s.groupRepo = &proxyGroupRepository{client: s.tx.Client()}
	s.proxyRepo = newProxyRepositoryWithSQL(s.tx.Client(), s.tx)
}

func TestProxyGroupRepoSuite(t *testing.T) {
	suite.Run(t, new(ProxyGroupRepoSuite))
}

func (s *ProxyGroupRepoSuite) TestCRUDCountsAndCaseInsensitiveUniqueName() {
	group := s.createGroup("Primary")

	s.createProxy("active", "active.example.test", service.StatusActive, &group.ID)
	s.createProxy("inactive", "inactive.example.test", "inactive", &group.ID)
	s.createProxy("expired", "expired.example.test", service.StatusExpired, &group.ID)

	groups, err := s.groupRepo.List(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(groups, 1)
	s.Require().Equal(int64(3), groups[0].TotalCount)
	s.Require().Equal(int64(1), groups[0].ActiveCount)
	s.Require().Equal(int64(1), groups[0].InactiveCount)

	group.Name = "Renamed"
	s.Require().NoError(s.groupRepo.Update(s.ctx, group))
	byName, err := s.groupRepo.GetByName(s.ctx, "renamed")
	s.Require().NoError(err)
	s.Require().Equal(group.ID, byName.ID)

	// A unique-constraint error aborts the suite transaction, so assert it last.
	duplicate := &service.ProxyGroup{Name: " renamed "}
	err = s.groupRepo.Create(s.ctx, duplicate)
	s.Require().ErrorIs(err, service.ErrProxyGroupExists)
}

func (s *ProxyGroupRepoSuite) TestListFiltersByGroupUngroupedAndHost() {
	group := s.createGroup("Primary")
	grouped := s.createProxy("grouped", "edge-primary.example.test", service.StatusActive, &group.ID)
	ungrouped := s.createProxy("ungrouped", "edge-secondary.example.test", service.StatusActive, nil)

	items, result, err := s.proxyRepo.ListWithFilters(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, service.ProxyListFilters{
		ProxyGroupID: &group.ID,
		Search:       "PRIMARY.EXAMPLE",
	})
	s.Require().NoError(err)
	s.Require().Equal(int64(1), result.Total)
	s.Require().Equal(grouped.ID, items[0].ID)
	s.Require().Equal(group.Name, items[0].ProxyGroupName)

	items, result, err = s.proxyRepo.ListWithFilters(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, service.ProxyListFilters{Ungrouped: true})
	s.Require().NoError(err)
	s.Require().Equal(int64(1), result.Total)
	s.Require().Equal(ungrouped.ID, items[0].ID)
}

func (s *ProxyGroupRepoSuite) TestDeleteUnassignsWithoutDeletingProxies() {
	group := s.createGroup("Delete Me")
	proxy := s.createProxy("proxy", "delete.example.test", service.StatusActive, &group.ID)

	s.Require().NoError(s.groupRepo.DeleteAndUnassign(s.ctx, group.ID))
	_, err := s.groupRepo.GetByID(s.ctx, group.ID)
	s.Require().ErrorIs(err, service.ErrProxyGroupNotFound)

	stored, err := s.proxyRepo.GetByID(s.ctx, proxy.ID)
	s.Require().NoError(err)
	s.Require().Nil(stored.ProxyGroupID)
}

func (s *ProxyGroupRepoSuite) TestBatchAssignIsAtomicAndCanClear() {
	group := s.createGroup("Batch")
	first := s.createProxy("first", "first.example.test", service.StatusActive, nil)
	second := s.createProxy("second", "second.example.test", service.StatusActive, nil)
	missingGroupID := int64(999999)

	_, err := s.groupRepo.BatchAssign(s.ctx, []int64{first.ID}, &missingGroupID)
	s.Require().ErrorIs(err, service.ErrProxyGroupNotFound)
	stored, getErr := s.proxyRepo.GetByID(s.ctx, first.ID)
	s.Require().NoError(getErr)
	s.Require().Nil(stored.ProxyGroupID)

	_, err = s.groupRepo.BatchAssign(s.ctx, []int64{first.ID, 999999}, &group.ID)
	s.Require().ErrorIs(err, service.ErrProxyNotFound)
	stored, getErr = s.proxyRepo.GetByID(s.ctx, first.ID)
	s.Require().NoError(getErr)
	s.Require().Nil(stored.ProxyGroupID)

	updated, err := s.groupRepo.BatchAssign(s.ctx, []int64{first.ID, second.ID}, &group.ID)
	s.Require().NoError(err)
	s.Require().Equal(int64(2), updated)
	for _, id := range []int64{first.ID, second.ID} {
		stored, getErr = s.proxyRepo.GetByID(s.ctx, id)
		s.Require().NoError(getErr)
		s.Require().Equal(group.ID, *stored.ProxyGroupID)
	}

	updated, err = s.groupRepo.BatchAssign(s.ctx, []int64{first.ID, second.ID}, nil)
	s.Require().NoError(err)
	s.Require().Equal(int64(2), updated)
	stored, err = s.proxyRepo.GetByID(s.ctx, first.ID)
	s.Require().NoError(err)
	s.Require().Nil(stored.ProxyGroupID)
}

func (s *ProxyGroupRepoSuite) TestMigrationCreatesNullableForeignKeyAndIndexes() {
	var nullable string
	s.Require().NoError(integrationDB.QueryRowContext(s.ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'proxies' AND column_name = 'proxy_group_id'
	`).Scan(&nullable))
	s.Require().Equal("YES", nullable)

	var deleteRule string
	s.Require().NoError(integrationDB.QueryRowContext(s.ctx, `
		SELECT rc.delete_rule
		FROM information_schema.referential_constraints rc
		WHERE rc.constraint_schema = 'public' AND rc.constraint_name = 'proxies_proxy_group_id_fkey'
	`).Scan(&deleteRule))
	s.Require().Equal("SET NULL", deleteRule)

	var groupNameIndex, proxyGroupIndex bool
	s.Require().NoError(integrationDB.QueryRowContext(s.ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'proxy_groups_name_unique_ci')
	`).Scan(&groupNameIndex))
	s.Require().NoError(integrationDB.QueryRowContext(s.ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'proxies_proxy_group_id_idx')
	`).Scan(&proxyGroupIndex))
	s.Require().True(groupNameIndex)
	s.Require().True(proxyGroupIndex)
}

func (s *ProxyGroupRepoSuite) createGroup(name string) *service.ProxyGroup {
	s.T().Helper()
	group := &service.ProxyGroup{Name: name}
	s.Require().NoError(s.groupRepo.Create(s.ctx, group))
	return group
}

func (s *ProxyGroupRepoSuite) createProxy(name, host, status string, groupID *int64) *service.Proxy {
	s.T().Helper()
	proxy := &service.Proxy{
		Name: name, Protocol: "http", Host: host, Port: 8080, Status: status,
		FallbackMode: service.FallbackModeNone, ExpiryWarnDays: 7, ProxyGroupID: groupID,
	}
	s.Require().NoError(s.proxyRepo.Create(s.ctx, proxy))
	return proxy
}
