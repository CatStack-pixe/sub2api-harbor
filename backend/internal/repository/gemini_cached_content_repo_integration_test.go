//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGeminiCachedContentRepository_Lifecycle(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := &geminiCachedContentRepository{sql: tx}
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := time.Now().Format("150405.000000")
	apiKeyID := int64(910001)

	newRecord := func(publicID string, keyID, groupID int64, expire time.Time) *service.GeminiCachedContent {
		return &service.GeminiCachedContent{
			PublicID: publicID + suffix, UserID: 1, APIKeyID: keyID, GroupID: groupID, AccountID: 99,
			UpstreamName: "cachedContents/up" + publicID, Model: "gemini-3.8-flash", UpstreamModel: "models/gemini-3.8-flash",
			DisplayName: "label", TotalTokenCount: 2730, ExpireTime: expire,
			ChannelUsage: service.ChannelUsageFields{ChannelID: 7, OriginalModel: "gemini-pro", ChannelMappedModel: "gemini-3.8-flash", BillingModelSource: "requested"},
		}
	}
	first := newRecord("a", apiKeyID, 42, now.Add(time.Hour))
	require.NoError(t, repo.Create(ctx, first))
	require.Positive(t, first.ID)
	require.False(t, first.CreatedAt.IsZero())
	_, err := tx.ExecContext(ctx, `SAVEPOINT gemini_cached_content_duplicate`)
	require.NoError(t, err)
	require.ErrorIs(t, repo.Create(ctx, newRecord("a", apiKeyID, 42, now.Add(time.Hour))), service.ErrGeminiCachedContentExists)
	_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT gemini_cached_content_duplicate`)
	require.NoError(t, err)

	second := newRecord("b", apiKeyID, 42, now.Add(time.Hour))
	require.NoError(t, repo.Create(ctx, second))
	require.NoError(t, repo.Create(ctx, newRecord("expired", apiKeyID, 42, now.Add(-time.Minute))))
	require.NoError(t, repo.Create(ctx, newRecord("othergroup", apiKeyID, 43, now.Add(time.Hour))))
	require.NoError(t, repo.Create(ctx, newRecord("otherkey", apiKeyID+1, 42, now.Add(time.Hour))))

	got, err := repo.GetForOwner(ctx, apiKeyID, first.PublicID)
	require.NoError(t, err)
	require.Equal(t, "cachedContents/upa", got.UpstreamName)
	require.Equal(t, int64(2730), got.TotalTokenCount)
	require.True(t, got.ExpireTime.Equal(first.ExpireTime))
	require.Equal(t, first.ChannelUsage, got.ChannelUsage, "创建时的渠道计费口径随记录持久化")
	_, err = repo.GetForOwner(ctx, apiKeyID+1, first.PublicID)
	require.ErrorIs(t, err, service.ErrGeminiCachedContentNotFound)

	list, err := repo.ListForOwner(ctx, apiKeyID, 42, now, 0, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, second.PublicID, list[0].PublicID)
	page, err := repo.ListForOwner(ctx, apiKeyID, 42, now, list[0].ID, 10)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, first.PublicID, page[0].PublicID)

	lease := 2 * time.Minute
	acquired, err := repo.BeginMutation(ctx, first.ID, first.ExpireTime, "t1", lease)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = repo.BeginMutation(ctx, first.ID, first.ExpireTime, "t2", lease)
	require.NoError(t, err)
	require.False(t, acquired, "有进行中的修改时不能再登记")
	taken, err := repo.TakeOverStaleMutation(ctx, first.ID, "t2", lease)
	require.NoError(t, err)
	require.Nil(t, taken, "租约未过期不能接管")
	require.ErrorIs(t, repo.CommitMutation(ctx, first.ID, "t2", now), service.ErrGeminiCachedContentMutationConflict)

	newExpire := now.Add(3 * time.Hour)
	require.NoError(t, repo.CommitMutation(ctx, first.ID, "t1", newExpire))
	got, err = repo.GetForOwner(ctx, apiKeyID, first.PublicID)
	require.NoError(t, err)
	require.True(t, got.ExpireTime.Equal(newExpire))
	require.ErrorIs(t, repo.CommitMutation(ctx, first.ID, "t1", now), service.ErrGeminiCachedContentMutationConflict, "提交后修改意图已清除")

	acquired, err = repo.BeginMutation(ctx, first.ID, first.ExpireTime, "t3", lease)
	require.NoError(t, err)
	require.False(t, acquired, "到期时间已被其他修改改变")
	acquired, err = repo.BeginMutation(ctx, first.ID, newExpire, "t3", lease)
	require.NoError(t, err)
	require.True(t, acquired)
	stale, err := repo.ListStaleMutations(ctx, 10)
	require.NoError(t, err)
	require.NotContains(t, stale, first.ID)

	_, err = tx.ExecContext(ctx, `UPDATE gemini_cached_contents SET mutation_lease_until = NOW() - INTERVAL '1 second' WHERE id = $1`, first.ID)
	require.NoError(t, err)
	stale, err = repo.ListStaleMutations(ctx, 10)
	require.NoError(t, err)
	require.Contains(t, stale, first.ID)
	taken, err = repo.TakeOverStaleMutation(ctx, first.ID, "t4", lease)
	require.NoError(t, err)
	require.NotNil(t, taken)
	require.True(t, taken.ExpireTime.Equal(newExpire), "接管返回修改前的到期时间")
	require.ErrorIs(t, repo.CommitMutation(ctx, first.ID, "t3", now), service.ErrGeminiCachedContentMutationConflict, "被接管后原持有者不能提交")
	require.NoError(t, repo.ReleaseMutation(ctx, first.ID, "t3"))
	acquired, err = repo.BeginMutation(ctx, first.ID, newExpire, "t5", lease)
	require.NoError(t, err)
	require.False(t, acquired, "原持有者释放不影响接管者")
	require.NoError(t, repo.ReleaseMutation(ctx, first.ID, "t4"))
	stale, err = repo.ListStaleMutations(ctx, 10)
	require.NoError(t, err)
	require.NotContains(t, stale, first.ID)

	require.NoError(t, repo.SoftDelete(ctx, first.ID))
	_, err = repo.GetForOwner(ctx, apiKeyID, first.PublicID)
	require.ErrorIs(t, err, service.ErrGeminiCachedContentNotFound)
	_, err = repo.BeginMutation(ctx, first.ID, newExpire, "t6", lease)
	require.ErrorIs(t, err, service.ErrGeminiCachedContentNotFound)
	list, err = repo.ListForOwner(ctx, apiKeyID, 42, now, 0, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestGeminiCachedContentRepository_OwnerAndGroupPaginationBoundaries(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := &geminiCachedContentRepository{sql: tx}
	now := time.Now().UTC().Truncate(time.Microsecond)
	const owner, groupID = int64(920001), int64(920042)
	create := func(label string, key, group int64, expires time.Time) *service.GeminiCachedContent {
		record := geminiCacheRegressionRecord(label, key, group, expires)
		require.NoError(t, repo.Create(ctx, record))
		return record
	}
	first := create("scope-first", owner, groupID, now.Add(time.Hour))
	second := create("scope-second", owner, groupID, now.Add(time.Hour))
	boundary := create("scope-boundary", owner, groupID, now)
	expired := create("scope-expired", owner, groupID, now.Add(-time.Microsecond))
	deleted := create("scope-deleted", owner, groupID, now.Add(time.Hour))
	otherGroup := create("scope-other-group", owner, groupID+1, now.Add(time.Hour))
	otherOwner := create("scope-other-owner", owner+1, groupID, now.Add(time.Hour))
	require.NoError(t, repo.SoftDelete(ctx, deleted.ID))

	for _, scenario := range []struct {
		name   string
		key    int64
		group  int64
		before int64
		limit  int
		want   []*service.GeminiCachedContent
	}{
		{"first page", owner, groupID, 0, 1, []*service.GeminiCachedContent{second}},
		{"exclusive cursor", owner, groupID, second.ID, 1, []*service.GeminiCachedContent{first}},
		{"exhausted cursor", owner, groupID, first.ID, 1, nil},
		{"default limit", owner, groupID, 0, 0, []*service.GeminiCachedContent{second, first}},
		{"other group", owner, groupID + 1, 0, 10, []*service.GeminiCachedContent{otherGroup}},
		{"other owner", owner + 1, groupID, 0, 10, []*service.GeminiCachedContent{otherOwner}},
		{"missing group", owner, groupID + 2, 0, 10, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			got, err := repo.ListForOwner(ctx, scenario.key, scenario.group, now, scenario.before, scenario.limit)
			require.NoError(t, err)
			require.Len(t, got, len(scenario.want))
			for i, expected := range scenario.want {
				require.Equal(t, expected.ID, got[i].ID)
				require.Equal(t, expected.UserID, got[i].UserID)
				require.Equal(t, expected.AccountID, got[i].AccountID)
				require.Equal(t, expected.GroupID, got[i].GroupID)
				require.Equal(t, expected.ChannelUsage, got[i].ChannelUsage)
			}
		})
	}
	for _, record := range []*service.GeminiCachedContent{first, second, boundary, expired, otherGroup} {
		got, err := repo.GetForOwner(ctx, owner, record.PublicID)
		require.NoError(t, err, "owners can inspect expired records for lifecycle management")
		require.Equal(t, record.ID, got.ID)
		_, err = repo.GetForOwner(ctx, owner+1, record.PublicID)
		require.ErrorIs(t, err, service.ErrGeminiCachedContentNotFound)
	}
	_, err := repo.GetForOwner(ctx, owner, deleted.PublicID)
	require.ErrorIs(t, err, service.ErrGeminiCachedContentNotFound)

	// Public identifiers remain reserved even after deletion, including across
	// owners; an old cachedContents identifier must never be rebound.
	_, err = tx.ExecContext(ctx, "SAVEPOINT gemini_cache_tombstone")
	require.NoError(t, err)
	reused := geminiCacheRegressionRecord("scope-reused", owner+1, groupID, now.Add(time.Hour))
	reused.PublicID = deleted.PublicID
	require.ErrorIs(t, repo.Create(ctx, reused), service.ErrGeminiCachedContentExists)
	_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT gemini_cache_tombstone")
	require.NoError(t, err)
}

func TestGeminiCachedContentRepository_StaleLeaseRecoveryAndDeletion(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	repo := &geminiCachedContentRepository{sql: tx}
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	create := func(label string, ageMinutes int, deleted bool) *service.GeminiCachedContent {
		record := geminiCacheRegressionRecord(label, 930001, 930042, expires)
		require.NoError(t, repo.Create(ctx, record))
		acquired, err := repo.BeginMutation(ctx, record.ID, expires, "original-holder", time.Minute)
		require.NoError(t, err)
		require.True(t, acquired)
		_, err = tx.ExecContext(ctx, `
UPDATE gemini_cached_contents
SET mutation_lease_until = NOW() + make_interval(mins => $2::int)
WHERE id = $1`, record.ID, ageMinutes)
		require.NoError(t, err)
		if deleted {
			require.NoError(t, repo.SoftDelete(ctx, record.ID))
		}
		return record
	}
	first := create("lease-oldest", -5, false)
	second := create("lease-next", -4, false)
	deleted := create("lease-deleted", -6, true)
	fresh := create("lease-fresh", 1, false)
	boundary := create("lease-exact-now", 0, false)
	unleased := geminiCacheRegressionRecord("lease-no-intent", 930001, 930042, expires)
	require.NoError(t, repo.Create(ctx, unleased))
	_, err := tx.ExecContext(ctx, "UPDATE gemini_cached_contents SET mutation_lease_until = NOW() - INTERVAL '10 minutes' WHERE id = $1", unleased.ID)
	require.NoError(t, err)

	stale, err := repo.ListStaleMutations(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID}, stale, "recovery is bounded and ordered by lease expiry")
	stale, err = repo.ListStaleMutations(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{first.ID, second.ID}, stale, "deleted, current, equal-to-now and unleased rows are excluded")
	got, err := repo.GetForOwner(ctx, first.APIKeyID, first.PublicID)
	require.NoError(t, err)
	require.True(t, got.ExpireTime.Equal(expires), "an uncommitted intent never advances expiry")

	before := v16RowsSnapshot(t, ctx, tx, "gemini_cached_contents")
	require.ErrorIs(t, repo.CommitMutation(ctx, first.ID, "wrong-holder", expires.Add(time.Hour)), service.ErrGeminiCachedContentMutationConflict)
	require.NoError(t, repo.ReleaseMutation(ctx, first.ID, "wrong-holder"))
	require.Equal(t, before, v16RowsSnapshot(t, ctx, tx, "gemini_cached_contents"), "wrong tokens leave all lease, billing and expiry fields intact")
	acquired, err := repo.BeginMutation(ctx, first.ID, expires, "new-writer", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "an expired intent still requires explicit recovery")

	recovered, err := repo.TakeOverStaleMutation(ctx, first.ID, "recovery-holder", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	require.True(t, recovered.ExpireTime.Equal(expires))
	require.Equal(t, first.ChannelUsage, recovered.ChannelUsage)
	recoveredAgain, err := repo.TakeOverStaleMutation(ctx, first.ID, "second-recovery", time.Minute)
	require.NoError(t, err)
	require.Nil(t, recoveredAgain, "renewed recovery leases cannot be taken twice")
	require.ErrorIs(t, repo.CommitMutation(ctx, first.ID, "original-holder", expires.Add(time.Hour)), service.ErrGeminiCachedContentMutationConflict)
	require.NoError(t, repo.ReleaseMutation(ctx, first.ID, "original-holder"))
	require.NoError(t, repo.CommitMutation(ctx, first.ID, "recovery-holder", expires.Add(2*time.Hour)))
	got, err = repo.GetForOwner(ctx, first.APIKeyID, first.PublicID)
	require.NoError(t, err)
	require.True(t, got.ExpireTime.Equal(expires.Add(2*time.Hour)))
	require.Equal(t, first.ChannelUsage, got.ChannelUsage, "TTL updates retain the original channel billing scope")
	var clear bool
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT mutation_token IS NULL AND mutation_lease_until IS NULL FROM gemini_cached_contents WHERE id = $1`, first.ID).Scan(&clear))
	require.True(t, clear, "successful commits clear both intent fields")

	for _, record := range []*service.GeminiCachedContent{deleted, fresh, boundary, unleased} {
		taken, err := repo.TakeOverStaleMutation(ctx, record.ID, "unexpected-recovery", time.Minute)
		require.NoError(t, err)
		require.Nil(t, taken)
	}
	require.ErrorIs(t, repo.CommitMutation(ctx, deleted.ID, "original-holder", expires), service.ErrGeminiCachedContentMutationConflict)
	require.NoError(t, repo.ReleaseMutation(ctx, deleted.ID, "original-holder"), "tombstones may release their pending intent")
	require.NoError(t, repo.SoftDelete(ctx, second.ID))
	stale, err = repo.ListStaleMutations(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, stale)
}

func TestGeminiCachedContentRepository_ConcurrentMutationHasSingleHolder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := &geminiCachedContentRepository{sql: integrationDB}
	record := geminiCacheRegressionRecord("concurrent-lease", 940001, 940042, time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond))
	require.NoError(t, repo.Create(ctx, record))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM gemini_cached_contents WHERE id = $1", record.ID)
		require.NoError(t, err)
	})

	type result struct {
		token    string
		acquired bool
		err      error
	}
	const writers = 8
	results := make(chan result, writers)
	start := make(chan struct{})
	for i := range writers {
		go func(i int) {
			<-start
			token := fmt.Sprintf("concurrent-holder-%d", i)
			acquired, err := repo.BeginMutation(ctx, record.ID, record.ExpireTime, token, time.Minute)
			results <- result{token: token, acquired: acquired, err: err}
		}(i)
	}
	close(start)
	var winner string
	var successes int
	var losers []string
	outcomes := make([]result, 0, writers)
	for range writers {
		outcomes = append(outcomes, <-results)
	}
	for _, outcome := range outcomes {
		require.NoError(t, outcome.err)
		if outcome.acquired {
			winner = outcome.token
			successes++
		} else {
			losers = append(losers, outcome.token)
		}
	}
	require.Equal(t, 1, successes, "competing connections must atomically acquire exactly one lease")
	got, err := repo.GetForOwner(ctx, record.APIKeyID, record.PublicID)
	require.NoError(t, err)
	require.True(t, got.ExpireTime.Equal(record.ExpireTime))
	for _, loser := range losers {
		require.ErrorIs(t, repo.CommitMutation(ctx, record.ID, loser, record.ExpireTime.Add(time.Hour)), service.ErrGeminiCachedContentMutationConflict)
		require.NoError(t, repo.ReleaseMutation(ctx, record.ID, loser))
	}
	require.NoError(t, repo.CommitMutation(ctx, record.ID, winner, record.ExpireTime.Add(2*time.Hour)))
	got, err = repo.GetForOwner(ctx, record.APIKeyID, record.PublicID)
	require.NoError(t, err)
	require.True(t, got.ExpireTime.Equal(record.ExpireTime.Add(2*time.Hour)))
	require.Equal(t, record.ChannelUsage, got.ChannelUsage)
}

func geminiCacheRegressionRecord(label string, apiKeyID, groupID int64, expires time.Time) *service.GeminiCachedContent {
	return &service.GeminiCachedContent{
		PublicID: fmt.Sprintf("%s-%d", label, time.Now().UnixNano()),
		UserID:   41, APIKeyID: apiKeyID, GroupID: groupID, AccountID: 99,
		UpstreamName: "cachedContents/" + label, Model: "gemini-public", UpstreamModel: "models/gemini-upstream",
		DisplayName: label, TotalTokenCount: 8192, ExpireTime: expires,
		ChannelUsage: service.ChannelUsageFields{ChannelID: 73, OriginalModel: "gemini-public", ChannelMappedModel: "gemini-upstream", BillingModelSource: "requested"},
	}
}
