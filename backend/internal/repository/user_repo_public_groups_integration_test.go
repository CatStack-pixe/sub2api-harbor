//go:build integration

package repository

import "github.com/Wei-Shaw/sub2api/internal/service"

func (s *UserRepoSuite) TestUpdate_PublicGroupRestrictionMaskPersistence() {
	group := s.mustCreateGroup("public-permission-group")
	user := s.mustCreateUser(&service.User{
		Email: "public-permission@example.com", Username: "before", Balance: 12,
		AllowedGroups: []int64{group.ID},
	})
	stale, err := s.repo.GetByID(s.ctx, user.ID)
	s.Require().NoError(err)

	user.RestrictPublicGroups = true
	s.Require().NoError(s.repo.Update(s.ctx, user, service.UserUpdateFields{RestrictPublicGroups: true}))
	got, err := s.repo.GetByID(s.ctx, user.ID)
	s.Require().NoError(err)
	s.Require().True(got.RestrictPublicGroups)

	stale.Username = "after"
	stale.AllowedGroups = nil
	stale.Balance = 999
	s.Require().NoError(s.repo.Update(s.ctx, stale, service.UserUpdateFields{Username: true}))
	got, err = s.repo.GetByID(s.ctx, user.ID)
	s.Require().NoError(err)
	s.Require().True(got.RestrictPublicGroups, "an unmasked stale profile must preserve the restriction")
	s.Require().Equal([]int64{group.ID}, got.AllowedGroups)
	s.Require().Equal(12.0, got.Balance)

	got.RestrictPublicGroups = false
	s.Require().NoError(s.repo.Update(s.ctx, got, service.UserUpdateFields{RestrictPublicGroups: true}))
	got, err = s.repo.GetByID(s.ctx, user.ID)
	s.Require().NoError(err)
	s.Require().False(got.RestrictPublicGroups, "explicit false must also persist")
	s.Require().Equal([]int64{group.ID}, got.AllowedGroups)
}
