//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

type duplicateGroupRepoStub struct {
	GroupRepository
	nextID             int64
	groups             map[int64]*Group
	names              map[string]struct{}
	byOperation        map[string]int64
	sourceBindings     map[int64][]AccountGroup
	createdBindings    map[int64][]AccountGroup
	createdFromSources []int64
	atomicCreateErr    error
}

func newDuplicateGroupRepoStub(source *Group) *duplicateGroupRepoStub {
	repo := &duplicateGroupRepoStub{
		nextID:          100,
		groups:          make(map[int64]*Group),
		names:           make(map[string]struct{}),
		byOperation:     make(map[string]int64),
		sourceBindings:  make(map[int64][]AccountGroup),
		createdBindings: make(map[int64][]AccountGroup),
	}
	if source != nil {
		repo.groups[source.ID] = source
		repo.names[source.Name] = struct{}{}
	}
	return repo
}

func cloneGroupForDuplicateTest(group *Group) *Group {
	if group == nil {
		return nil
	}
	cloned := *group
	cloned.DailyLimitUSD = cloneGroupValuePointer(group.DailyLimitUSD)
	cloned.WeeklyLimitUSD = cloneGroupValuePointer(group.WeeklyLimitUSD)
	cloned.MonthlyLimitUSD = cloneGroupValuePointer(group.MonthlyLimitUSD)
	cloned.ModelPricing = cloneGroupModelPricing(group.ModelPricing)
	cloned.ImagePrice1K = cloneGroupValuePointer(group.ImagePrice1K)
	cloned.ImagePrice2K = cloneGroupValuePointer(group.ImagePrice2K)
	cloned.ImagePrice4K = cloneGroupValuePointer(group.ImagePrice4K)
	cloned.VideoPrice480P = cloneGroupValuePointer(group.VideoPrice480P)
	cloned.VideoPrice720P = cloneGroupValuePointer(group.VideoPrice720P)
	cloned.VideoPrice1080P = cloneGroupValuePointer(group.VideoPrice1080P)
	cloned.WebSearchPricePerCall = cloneGroupValuePointer(group.WebSearchPricePerCall)
	cloned.FallbackGroupID = cloneGroupValuePointer(group.FallbackGroupID)
	cloned.FallbackGroupIDOnInvalidRequest = cloneGroupValuePointer(group.FallbackGroupIDOnInvalidRequest)
	cloned.ModelRouting = cloneGroupModelRouting(group.ModelRouting)
	cloned.SupportedModelScopes = append([]string(nil), group.SupportedModelScopes...)
	cloned.MessagesDispatchModelConfig = cloneGroupMessagesDispatchModelConfig(group.MessagesDispatchModelConfig)
	cloned.ModelsListConfig.Models = append([]string(nil), group.ModelsListConfig.Models...)
	cloned.AccountGroups = append([]AccountGroup(nil), group.AccountGroups...)
	return &cloned
}

func (r *duplicateGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	cloned := cloneGroupForDuplicateTest(group)
	cloned.Hydrated = true
	return cloned, nil
}

func (r *duplicateGroupRepoStub) FindByDuplicateOperationID(_ context.Context, operationID string) (*Group, error) {
	id := r.byOperation[operationID]
	if id == 0 {
		return nil, nil
	}
	return cloneGroupForDuplicateTest(r.groups[id]), nil
}

func (r *duplicateGroupRepoStub) CreateFromSource(_ context.Context, group *Group, sourceGroupID int64) error {
	if r.atomicCreateErr != nil {
		return r.atomicCreateErr
	}
	if group.DuplicateOperationID != "" {
		if _, exists := r.byOperation[group.DuplicateOperationID]; exists {
			return ErrGroupExists
		}
	}
	if _, exists := r.names[group.Name]; exists {
		return ErrGroupExists
	}
	r.nextID++
	group.ID = r.nextID
	group.CreatedAt = time.Now().UTC()
	group.UpdatedAt = group.CreatedAt
	bindings := append([]AccountGroup(nil), r.sourceBindings[sourceGroupID]...)
	for i := range bindings {
		bindings[i].GroupID = group.ID
	}
	group.AccountCount = int64(len(bindings))
	group.ActiveAccountCount = int64(len(bindings))
	r.createdBindings[group.ID] = bindings
	r.createdFromSources = append(r.createdFromSources, sourceGroupID)
	r.names[group.Name] = struct{}{}
	r.groups[group.ID] = cloneGroupForDuplicateTest(group)
	if group.DuplicateOperationID != "" {
		r.byOperation[group.DuplicateOperationID] = group.ID
	}
	return nil
}

func groupDuplicateTestPointer[T any](value T) *T { return &value }

func TestCloneGroupForDuplicateDeepCopiesGroupModelPricing(t *testing.T) {
	source := &Group{
		Name:                      "priced group",
		LongContextPricingEnabled: true,
		ModelPricing: []ChannelModelPricing{
			{
				Models:           []string{"gpt-5.4"},
				InputPrice:       groupDuplicateTestPointer(1.0),
				OutputPrice:      groupDuplicateTestPointer(2.0),
				CacheWritePrice:  groupDuplicateTestPointer(3.0),
				CacheReadPrice:   groupDuplicateTestPointer(4.0),
				ImageInputPrice:  groupDuplicateTestPointer(5.0),
				ImageOutputPrice: groupDuplicateTestPointer(6.0),
				PerRequestPrice:  groupDuplicateTestPointer(7.0),
				Intervals: []PricingInterval{
					{
						MaxTokens:       groupDuplicateTestPointer(200000),
						InputPrice:      groupDuplicateTestPointer(8.0),
						OutputPrice:     groupDuplicateTestPointer(9.0),
						CacheWritePrice: groupDuplicateTestPointer(10.0),
						CacheReadPrice:  groupDuplicateTestPointer(11.0),
						PerRequestPrice: groupDuplicateTestPointer(12.0),
					},
				},
			},
		},
	}

	duplicate := cloneGroupForDuplicate(source, "operation")

	require.True(t, duplicate.LongContextPricingEnabled)
	require.Equal(t, source.ModelPricing, duplicate.ModelPricing)
	duplicate.LongContextPricingEnabled = false
	duplicate.ModelPricing[0].Models[0] = "changed"
	*duplicate.ModelPricing[0].InputPrice = 101
	*duplicate.ModelPricing[0].OutputPrice = 102
	*duplicate.ModelPricing[0].CacheWritePrice = 103
	*duplicate.ModelPricing[0].CacheReadPrice = 104
	*duplicate.ModelPricing[0].ImageInputPrice = 105
	*duplicate.ModelPricing[0].ImageOutputPrice = 106
	*duplicate.ModelPricing[0].PerRequestPrice = 107
	*duplicate.ModelPricing[0].Intervals[0].MaxTokens = 300000
	*duplicate.ModelPricing[0].Intervals[0].InputPrice = 108
	*duplicate.ModelPricing[0].Intervals[0].OutputPrice = 109
	*duplicate.ModelPricing[0].Intervals[0].CacheWritePrice = 110
	*duplicate.ModelPricing[0].Intervals[0].CacheReadPrice = 111
	*duplicate.ModelPricing[0].Intervals[0].PerRequestPrice = 112

	require.True(t, source.LongContextPricingEnabled)
	require.Equal(t, "gpt-5.4", source.ModelPricing[0].Models[0])
	require.Equal(t, 1.0, *source.ModelPricing[0].InputPrice)
	require.Equal(t, 2.0, *source.ModelPricing[0].OutputPrice)
	require.Equal(t, 3.0, *source.ModelPricing[0].CacheWritePrice)
	require.Equal(t, 4.0, *source.ModelPricing[0].CacheReadPrice)
	require.Equal(t, 5.0, *source.ModelPricing[0].ImageInputPrice)
	require.Equal(t, 6.0, *source.ModelPricing[0].ImageOutputPrice)
	require.Equal(t, 7.0, *source.ModelPricing[0].PerRequestPrice)
	require.Equal(t, 200000, *source.ModelPricing[0].Intervals[0].MaxTokens)
	require.Equal(t, 8.0, *source.ModelPricing[0].Intervals[0].InputPrice)
	require.Equal(t, 9.0, *source.ModelPricing[0].Intervals[0].OutputPrice)
	require.Equal(t, 10.0, *source.ModelPricing[0].Intervals[0].CacheWritePrice)
	require.Equal(t, 11.0, *source.ModelPricing[0].Intervals[0].CacheReadPrice)
	require.Equal(t, 12.0, *source.ModelPricing[0].Intervals[0].PerRequestPrice)
}

func TestDuplicateGroupCopiesConfigurationDeeplyAndResetsRuntimeState(t *testing.T) {
	createdAt := time.Date(2026, time.July, 1, 2, 3, 4, 0, time.UTC)
	source := &Group{
		ID:                           41,
		Name:                         "高级订阅",
		Description:                  "configuration",
		Platform:                     PlatformOpenAI,
		RateMultiplier:               1.75,
		PeakRateEnabled:              true,
		PeakStart:                    "09:00",
		PeakEnd:                      "18:00",
		PeakRateMultiplier:           1.2,
		IsExclusive:                  true,
		Status:                       StatusActive,
		Hydrated:                     true,
		SubscriptionType:             SubscriptionTypeSubscription,
		DailyLimitUSD:                groupDuplicateTestPointer(11.0),
		WeeklyLimitUSD:               groupDuplicateTestPointer(22.0),
		MonthlyLimitUSD:              groupDuplicateTestPointer(33.0),
		LongContextPricingEnabled:    true,
		GlobalPromptEnabled:          true,
		GlobalPrompt:                 "Follow the group policy.",
		ModelPricing: []ChannelModelPricing{
			{
				Platform:    PlatformOpenAI,
				Models:      []string{"gpt-5.4", "gpt-5-mini"},
				BillingMode: BillingModeToken,
				InputPrice:  groupDuplicateTestPointer(1.25e-6),
				OutputPrice: groupDuplicateTestPointer(5e-6),
				Intervals: []PricingInterval{
					{
						MinTokens:   0,
						MaxTokens:   groupDuplicateTestPointer(200000),
						InputPrice:  groupDuplicateTestPointer(1.25e-6),
						OutputPrice: groupDuplicateTestPointer(5e-6),
					},
				},
			},
		},
		DefaultValidityDays:          91,
		AllowImageGeneration:         true,
		AllowBatchImageGeneration:    true,
		ImageRateIndependent:         true,
		ImageRateMultiplier:          1.4,
		ImagePrice1K:                 groupDuplicateTestPointer(0.01),
		ImagePrice2K:                 groupDuplicateTestPointer(0.02),
		ImagePrice4K:                 groupDuplicateTestPointer(0.04),
		BatchImageDiscountMultiplier: 0.4,
		BatchImageHoldMultiplier:     0.7,
		VideoRateIndependent:         true,
		VideoRateMultiplier:          2.1,
		VideoPrice480P:               groupDuplicateTestPointer(0.1),
		VideoPrice720P:               groupDuplicateTestPointer(0.2),
		VideoPrice1080P:              groupDuplicateTestPointer(0.3),
		VideoModelPrices: map[string]map[string]float64{
			VideoPriceFamilyGrokImagineVideo15: {VideoBillingResolution720P: 0.14},
		},
		WebSearchPricePerCall:           groupDuplicateTestPointer(0.005),
		ClaudeCodeOnly:                  true,
		FallbackGroupID:                 groupDuplicateTestPointer(int64(7)),
		FallbackGroupIDOnInvalidRequest: groupDuplicateTestPointer(int64(8)),
		ModelRouting:                    map[string][]int64{"gpt-*": {13, 17}},
		ModelRoutingEnabled:             true,
		MCPXMLInject:                    true,
		SupportedModelScopes:            []string{"claude", "gemini_text"},
		SortOrder:                       9,
		AllowMessagesDispatch:           true,
		AllowLive:                       true,
		ForceOpenAIFast:                 true,
		FreeOpenAIFast:                  true,
		RequireOAuthOnly:                true,
		RequirePrivacySet:               true,
		DefaultMappedModel:              "gpt-5.4",
		MessagesDispatchModelConfig: OpenAIMessagesDispatchModelConfig{
			OpusMappedModel:    "gpt-5.4",
			SonnetMappedModel:  "gpt-5.3",
			HaikuMappedModel:   "gpt-5-mini",
			ExactModelMappings: map[string]string{"claude-special": "gpt-special"},
		},
		ModelsListConfig: GroupModelsListConfig{
			Enabled:             true,
			Models:              []string{"gpt-5.4", "gpt-5-mini"},
			ModelMappingEnabled: true,
			ModelMapping:        map[string]string{"deepseek-v4-pro": "agnes-2.5-pro-alpha"},
		},
		RPMLimit:                99,
		MaxReasoningEffort:      "medium",
		ReasoningEffortMappings: []ReasoningEffortMapping{{From: "max", To: "xhigh"}},
		CreatedAt:               createdAt,
		UpdatedAt:               createdAt,
		AccountCount:            12,
		ActiveAccountCount:      8,
		RateLimitedAccountCount: 2,
		DuplicateOperationID:    "old-operation-must-not-copy",
		AccountGroups:           []AccountGroup{{AccountID: 13, GroupID: 41, Priority: 37}},
	}
	repo := newDuplicateGroupRepoStub(source)
	repo.sourceBindings[source.ID] = []AccountGroup{
		{AccountID: 13, GroupID: source.ID, Priority: 37},
		{AccountID: 17, GroupID: source.ID, Priority: 8},
	}
	svc := &adminServiceImpl{groupRepo: repo, groupDuplicateRepo: repo}

	duplicate, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:7", "stable-key")

	require.NoError(t, err)
	require.NotEqual(t, source.ID, duplicate.ID)
	require.Equal(t, "高级订阅 (Copy)", duplicate.Name)
	require.Equal(t, duplicateGroupInactiveStatus, duplicate.Status)
	require.True(t, duplicate.Hydrated, "the duplicate response is reloaded with derived counts")
	require.Equal(t, source.Description, duplicate.Description)
	require.Equal(t, source.Platform, duplicate.Platform)
	require.Equal(t, source.RateMultiplier, duplicate.RateMultiplier)
	require.Equal(t, source.PeakRateMultiplier, duplicate.PeakRateMultiplier)
	require.Equal(t, source.LongContextPricingEnabled, duplicate.LongContextPricingEnabled)
	require.Equal(t, source.GlobalPromptEnabled, duplicate.GlobalPromptEnabled)
	require.Equal(t, source.GlobalPrompt, duplicate.GlobalPrompt)
	require.Equal(t, source.ModelPricing, duplicate.ModelPricing)
	require.Equal(t, source.DefaultValidityDays, duplicate.DefaultValidityDays)
	require.Equal(t, source.ImagePrice4K, duplicate.ImagePrice4K)
	require.Equal(t, source.VideoModelPrices, duplicate.VideoModelPrices)
	require.Equal(t, source.WebSearchPricePerCall, duplicate.WebSearchPricePerCall)
	require.Equal(t, source.FallbackGroupID, duplicate.FallbackGroupID)
	require.Equal(t, source.ModelRouting, duplicate.ModelRouting)
	require.Equal(t, source.MessagesDispatchModelConfig, duplicate.MessagesDispatchModelConfig)
	require.Equal(t, source.ForceOpenAIFast, duplicate.ForceOpenAIFast)
	require.Equal(t, source.FreeOpenAIFast, duplicate.FreeOpenAIFast)
	require.Equal(t, source.ModelsListConfig, duplicate.ModelsListConfig)
	require.Equal(t, source.RPMLimit, duplicate.RPMLimit)
	require.Equal(t, source.MaxReasoningEffort, duplicate.MaxReasoningEffort)
	require.Equal(t, source.MaxReasoningEffortOverLimit, duplicate.MaxReasoningEffortOverLimit)
	require.Equal(t, source.ReasoningEffortMappings, duplicate.ReasoningEffortMappings)
	require.EqualValues(t, 2, duplicate.AccountCount)
	require.EqualValues(t, 2, duplicate.ActiveAccountCount)
	require.NotEmpty(t, duplicate.DuplicateOperationID)
	require.Equal(t, []int64{source.ID}, repo.createdFromSources)
	require.Equal(t, []AccountGroup{
		{AccountID: 13, GroupID: duplicate.ID, Priority: 37},
		{AccountID: 17, GroupID: duplicate.ID, Priority: 8},
	}, repo.createdBindings[duplicate.ID])

	duplicate.ModelRouting["gpt-*"][0] = 999
	duplicate.VideoModelPrices[VideoPriceFamilyGrokImagineVideo15][VideoBillingResolution720P] = 999
	duplicate.SupportedModelScopes[0] = "changed"
	duplicate.MessagesDispatchModelConfig.ExactModelMappings["claude-special"] = "changed"
	duplicate.ModelsListConfig.Models[0] = "changed"
	duplicate.ModelsListConfig.ModelMapping["deepseek-v4-pro"] = "changed"
	duplicate.ReasoningEffortMappings[0].To = "changed"
	duplicate.LongContextPricingEnabled = false
	duplicate.GlobalPrompt = "changed"
	duplicate.ModelPricing[0].Models[0] = "changed"
	*duplicate.ModelPricing[0].InputPrice = 999
	*duplicate.ModelPricing[0].Intervals[0].MaxTokens = 999
	*duplicate.ModelPricing[0].Intervals[0].OutputPrice = 999
	*duplicate.DailyLimitUSD = 999
	require.Equal(t, int64(13), source.ModelRouting["gpt-*"][0])
	require.Equal(t, 0.14, source.VideoModelPrices[VideoPriceFamilyGrokImagineVideo15][VideoBillingResolution720P])
	require.Equal(t, "claude", source.SupportedModelScopes[0])
	require.Equal(t, "gpt-special", source.MessagesDispatchModelConfig.ExactModelMappings["claude-special"])
	require.Equal(t, "gpt-5.4", source.ModelsListConfig.Models[0])
	require.Equal(t, "agnes-2.5-pro-alpha", source.ModelsListConfig.ModelMapping["deepseek-v4-pro"])
	require.Equal(t, "xhigh", source.ReasoningEffortMappings[0].To)
	require.True(t, source.LongContextPricingEnabled)
	require.True(t, source.GlobalPromptEnabled)
	require.Equal(t, "Follow the group policy.", source.GlobalPrompt)
	require.Equal(t, "gpt-5.4", source.ModelPricing[0].Models[0])
	require.Equal(t, 1.25e-6, *source.ModelPricing[0].InputPrice)
	require.Equal(t, 200000, *source.ModelPricing[0].Intervals[0].MaxTokens)
	require.Equal(t, 5e-6, *source.ModelPricing[0].Intervals[0].OutputPrice)
	require.Equal(t, 11.0, *source.DailyLimitUSD)
}

func TestDuplicateGroupRecoversSameOperationAndScopesByAdmin(t *testing.T) {
	source := &Group{ID: 9, Name: "team", Platform: PlatformAnthropic, Status: StatusActive}
	repo := newDuplicateGroupRepoStub(source)
	svc := &adminServiceImpl{groupRepo: repo, groupDuplicateRepo: repo}
	ctx := context.Background()

	first, err := svc.DuplicateGroup(ctx, source.ID, "admin:7", "same-key")
	require.NoError(t, err)
	retry, err := svc.DuplicateGroup(ctx, source.ID, "admin:7", "same-key")
	require.NoError(t, err)
	recovered, err := svc.RecoverDuplicateGroup(ctx, source.ID, "admin:7", "same-key")
	require.NoError(t, err)
	otherAdmin, err := svc.DuplicateGroup(ctx, source.ID, "admin:8", "same-key")
	require.NoError(t, err)

	require.Equal(t, first.ID, retry.ID)
	require.Equal(t, first.ID, recovered.ID)
	require.NotEqual(t, first.ID, otherAdmin.ID)
	require.Equal(t, "team (Copy 2)", otherAdmin.Name)
}

func TestDuplicateGroupAdvancesNameAndTruncatesUnicodeByRunes(t *testing.T) {
	source := &Group{ID: 12, Name: "team", Platform: PlatformAnthropic, Status: StatusActive}
	repo := newDuplicateGroupRepoStub(source)
	repo.names["team (Copy)"] = struct{}{}
	svc := &adminServiceImpl{groupRepo: repo, groupDuplicateRepo: repo}

	duplicate, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:1", "")
	require.NoError(t, err)
	require.Equal(t, "team (Copy 2)", duplicate.Name)

	unicodeName := duplicateGroupName(strings.Repeat("组", 100), 23)
	require.Equal(t, maxGroupNameRunes, utf8.RuneCountInString(unicodeName))
	require.True(t, strings.HasSuffix(unicodeName, " (Copy 23)"))
}

func TestDuplicateGroupAtomicCreateFailureReturnsNoCopy(t *testing.T) {
	source := &Group{ID: 15, Name: "team", Platform: PlatformAnthropic, Status: StatusActive}
	repo := newDuplicateGroupRepoStub(source)
	repo.atomicCreateErr = errors.New("binding insert failed")
	svc := &adminServiceImpl{groupRepo: repo, groupDuplicateRepo: repo}

	duplicate, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:1", "key")

	require.ErrorContains(t, err, "binding insert failed")
	require.Nil(t, duplicate)
	require.Len(t, repo.groups, 1)
	require.Empty(t, repo.byOperation)
}
