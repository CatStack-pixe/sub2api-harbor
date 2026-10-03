package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepository_PublicGroupRestrictionMaskPersistence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before bool
		value  bool
		mask   bool
		want   bool
	}{
		{name: "enable", value: true, mask: true, want: true},
		{name: "disable", before: true, mask: true},
		{name: "unmasked false keeps enabled", before: true, want: true},
		{name: "unmasked true keeps disabled", value: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, client := newUserEntRepo(t)
			ctx := context.Background()
			group, err := client.Group.Create().SetName("permission-group").Save(ctx)
			require.NoError(t, err)
			user := &service.User{
				Email: "permission@example.com", Username: "before", PasswordHash: "local-hash",
				Role: service.RoleUser, Status: service.StatusActive, Balance: 12,
				AllowedGroups: []int64{group.ID}, RestrictPublicGroups: tc.before,
			}
			require.NoError(t, repo.Create(ctx, user))

			snapshot, err := repo.GetByID(ctx, user.ID)
			require.NoError(t, err)
			snapshot.Username = "after"
			snapshot.RestrictPublicGroups = tc.value
			snapshot.AllowedGroups = nil
			snapshot.Balance = 999
			require.NoError(t, repo.Update(ctx, snapshot, service.UserUpdateFields{
				Username: true, RestrictPublicGroups: tc.mask,
			}))

			got, err := repo.GetByID(ctx, user.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.RestrictPublicGroups)
			require.Equal(t, "after", got.Username)
			require.Equal(t, []int64{group.ID}, got.AllowedGroups)
			require.Equal(t, 12.0, got.Balance)
		})
	}
}

func TestUserRepository_PublicGroupRestrictionSurvivesStaleProfile(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()
	user := &service.User{
		Email: "stale-permission@example.com", Username: "before", PasswordHash: "local-hash",
		Role: service.RoleUser, Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, user))
	stale, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	current, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)

	current.RestrictPublicGroups = true
	require.NoError(t, repo.Update(ctx, current, service.UserUpdateFields{RestrictPublicGroups: true}))
	stale.Username = "after"
	require.NoError(t, repo.Update(ctx, stale, service.UserUpdateFields{Username: true}))

	got, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, got.RestrictPublicGroups, "an unrelated stale write must not reopen public groups")
	require.Equal(t, "after", got.Username)
}
