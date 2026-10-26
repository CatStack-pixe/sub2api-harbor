//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Both gateways receive the same exclusive token buckets; OpenAI reports caches
// inside input_tokens while the general gateway reports them separately.
func v18HarborImageUsageCost(t *testing.T, pipeline string, resolver *ModelPricingResolver, apiKey *APIKey, model string, count int, endpoint bool, tokens UsageTokens, pricingAt time.Time) *CostBreakdown {
	t.Helper()
	effort := "high"
	if pipeline == "gateway" {
		svc := &GatewayService{billingService: resolver.billingService, resolver: resolver}
		return svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
			Model: model, ImageCount: count, ImageSize: "1K", ImageEndpoint: endpoint, ReasoningEffort: &effort,
			Usage: ClaudeUsage{
				InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens,
				CacheCreationInputTokens: tokens.CacheCreationTokens, CacheReadInputTokens: tokens.CacheReadTokens,
				ImageOutputTokens: tokens.ImageOutputTokens,
			},
		}, apiKey, model, 0.5, 0.25, pricingAt, &recordUsageOpts{
			LongContextThreshold: 100, LongContextMultiplier: 3,
		})
	}
	svc := &OpenAIGatewayService{billingService: resolver.billingService, resolver: resolver}
	cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), &OpenAIForwardResult{
		Model: model, ImageCount: count, ImageSize: "1K", ImageEndpoint: endpoint, ReasoningEffort: &effort,
		Usage: OpenAIUsage{
			InputTokens:              tokens.InputTokens + tokens.CacheCreationTokens + tokens.CacheReadTokens,
			OutputTokens:             tokens.OutputTokens,
			CacheCreationInputTokens: tokens.CacheCreationTokens, CacheReadInputTokens: tokens.CacheReadTokens,
			ImageOutputTokens: tokens.ImageOutputTokens,
		},
	}, apiKey, []string{model}, 0.5, 0.25, 1, 1, tokens, "", nil, false, pricingAt)
	require.NoError(t, err)
	return cost
}

func TestV18HarborBilling_ImageEndpointGate(t *testing.T) {
	for _, pipeline := range []string{"gateway", "openai"} {
		for _, source := range []string{PricingSourceChannel, PricingSourceGroup} {
			for _, enabled := range []bool{false, true} {
				for _, endpoint := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/enabled=%t/endpoint=%t", pipeline, source, enabled, endpoint), func(t *testing.T) {
						const model = "gpt-image-2"
						groupID := int64(918)
						resolver := newOpenAITokenImageChannelPricingResolverForTest(t, groupID, model)
						cache := resolver.channelService.cache.Load().(*channelCache)
						card := cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}]
						card.ReasoningEffortMultipliers = map[string]float64{"high": 2}
						group := &Group{
							ID: groupID, Platform: PlatformComposite, Hydrated: true, AllowImageGeneration: true,
							ImageCountBilling: enabled, ImagePrice1K: f64p(0.4),
						}
						wantTotal := (1000*3e-6 + 100*15e-6) * 2
						if source == PricingSourceGroup {
							group.ModelPricing = []ChannelModelPricing{{
								Models: []string{model}, BillingMode: BillingModeToken,
								InputPrice: f64p(4e-6), OutputPrice: f64p(10e-6), ImageOutputPrice: f64p(10e-6),
								ReasoningEffortMultipliers: map[string]float64{"high": 1.5},
							}}
							wantTotal = (1000*4e-6 + 100*10e-6) * 1.5
						}
						apiKey := &APIKey{GroupID: &groupID, Group: group}
						cost := v18HarborImageUsageCost(t, pipeline, resolver, apiKey, model, 3, endpoint,
							UsageTokens{InputTokens: 1000, OutputTokens: 100, ImageOutputTokens: 20}, time.Time{})
						require.NotNil(t, cost)
						wantMode, wantRate := BillingModeToken, 0.5
						if enabled && endpoint {
							wantMode, wantRate, wantTotal = BillingModeImage, 0.25, 3*0.4
						}
						require.Equal(t, string(wantMode), cost.BillingMode)
						require.InDelta(t, wantTotal, cost.TotalCost, 1e-12)
						require.InDelta(t, wantTotal*wantRate, cost.ActualCost, 1e-12)
						if wantMode == BillingModeToken {
							require.Positive(t, cost.OutputCost, "mixed requests retain text output")
							require.Positive(t, cost.ImageOutputCost, "mixed requests retain image tokens")
						}
					})
				}
			}
		}
	}
}

func TestV18HarborBilling_ForcedImagesUseDefaultPriceWithoutImageOverrides(t *testing.T) {
	for _, pipeline := range []string{"gateway", "openai"} {
		for _, source := range []string{PricingSourceChannel, PricingSourceGroup} {
			t.Run(pipeline+"/"+source, func(t *testing.T) {
				const model = "gpt-image-2"
				groupID := int64(922)
				resolver := newOpenAITokenImageChannelPricingResolverForTest(t, groupID, model)
				group := &Group{
					ID: groupID, Platform: PlatformComposite, Hydrated: true,
					AllowImageGeneration: true, ImageCountBilling: true,
				}
				if source == PricingSourceGroup {
					group.ModelPricing = []ChannelModelPricing{{
						Models: []string{model}, BillingMode: BillingModeToken,
						InputPrice: f64p(3e-6), OutputPrice: f64p(15e-6),
					}}
				}
				cost := v18HarborImageUsageCost(t, pipeline, resolver, &APIKey{GroupID: &groupID, Group: group},
					model, 3, true, UsageTokens{InputTokens: 1000}, time.Time{})
				require.NotNil(t, cost)
				require.Equal(t, string(BillingModeImage), cost.BillingMode)
				require.InDelta(t, 3*0.134, cost.TotalCost, 1e-12)
				require.InDelta(t, 3*0.134*0.25, cost.ActualCost, 1e-12)
			})
		}
	}
}

func TestV18HarborBilling_ImageContextTiersRetainCacheTokens(t *testing.T) {
	for _, pipeline := range []string{"gateway", "openai"} {
		for _, source := range []string{PricingSourceChannel, PricingSourceGroup} {
			for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage} {
				for _, enabled := range []bool{false, true} {
					for _, extra := range []int{0, 1} {
						name := fmt.Sprintf("%s/%s/%s/enabled=%t/extra=%d", pipeline, source, mode, enabled, extra)
						t.Run(name, func(t *testing.T) {
							const model = "gemini-3-pro-image"
							groupID := int64(919)
							resolver := newOpenAITokenImageChannelPricingResolverForTest(t, groupID, model)
							cache := resolver.channelService.cache.Load().(*channelCache)
							maxTokens := 1000
							card := ChannelModelPricing{
								Models: []string{model}, BillingMode: mode,
								Intervals: []PricingInterval{
									{MaxTokens: &maxTokens, PerRequestPrice: f64p(0.1)},
									{MinTokens: 1000, PerRequestPrice: f64p(0.3)},
								},
								ReasoningEffortMultipliers: map[string]float64{"high": 2},
							}
							cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = &card
							group := &Group{
								ID: groupID, Platform: PlatformComposite, Hydrated: true, AllowImageGeneration: true,
								ImageCountBilling: enabled,
							}
							if source == PricingSourceGroup {
								group.ModelPricing = []ChannelModelPricing{card.Clone()}
								// Neither the channel card nor the legacy image price owns this request.
								card.ReasoningEffortMultipliers = map[string]float64{"high": 9}
								group.ImagePrice1K = f64p(7)
							}
							apiKey := &APIKey{GroupID: &groupID, Group: group}
							cost := v18HarborImageUsageCost(t, pipeline, resolver, apiKey, model, 3, true,
								UsageTokens{InputTokens: 400 + extra, CacheReadTokens: 300, CacheCreationTokens: 300,
									OutputTokens: 9000, ImageOutputTokens: 500}, time.Time{})
							wantPrice := 0.1
							if extra > 0 {
								wantPrice = 0.3
							}
							require.NotNil(t, cost)
							require.Equal(t, string(mode), cost.BillingMode)
							require.InDelta(t, 3*wantPrice*2, cost.TotalCost, 1e-12)
							require.InDelta(t, 3*wantPrice*2*0.25, cost.ActualCost, 1e-12)
						})
					}
				}
			}
		}
	}
}

func TestV18HarborBilling_MixedImageTokenPricingAt(t *testing.T) {
	for _, pipeline := range []string{"gateway", "openai"} {
		for _, slot := range []struct {
			hour      int
			wantTotal float64
		}{
			{hour: 8, wantTotal: 1000 * 6e-6 * 2 * 1.5},
			{hour: 16, wantTotal: 1000 * 3e-6 * 1.5},
		} {
			t.Run(fmt.Sprintf("%s/hour=%d", pipeline, slot.hour), func(t *testing.T) {
				const model = "gpt-image-2"
				groupID := int64(920)
				resolver := newOpenAITokenImageChannelPricingResolverForTest(t, groupID, model)
				cache := resolver.channelService.cache.Load().(*channelCache)
				card := cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}]
				card.TimeWindows = []PricingTimeWindow{{
					StartMinute: 16 * 60, EndMinute: 17 * 60, InputPrice: f64p(6e-6),
				}}
				card.TimePricing = &ChannelTimePricing{
					Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{
						StartTime: "08:00", EndTime: "09:00", Multiplier: 2,
					}},
				}
				card.ReasoningEffortMultipliers = map[string]float64{"high": 1.5}
				apiKey := &APIKey{GroupID: &groupID, Group: &Group{
					ID: groupID, Hydrated: true, ImageCountBilling: true, ImagePrice1K: f64p(7),
				}}
				pricingAt := time.Date(2026, 9, 18, slot.hour, 30, 0, 0, time.UTC)
				cost := v18HarborImageUsageCost(t, pipeline, resolver, apiKey, model, 3, false,
					UsageTokens{InputTokens: 1000}, pricingAt)
				require.NotNil(t, cost)
				require.Equal(t, string(BillingModeToken), cost.BillingMode)
				require.InDelta(t, slot.wantTotal, cost.TotalCost, 1e-12)
				require.InDelta(t, slot.wantTotal*0.5, cost.ActualCost, 1e-12)
				require.InDelta(t, 3e-6, *card.InputPrice, 1e-12, "frozen pricing must not mutate the cache")
			})
		}
	}
}

func TestV18HarborBilling_ReturnedImageCountAndPricingPriority(t *testing.T) {
	body := []byte(`{"data":[{"url":"https://images.example/a.png"},{"url":"https://images.example/b.png"},{"url":"https://images.example/c.png"}]}`)
	for _, pipeline := range []string{"gateway", "openai"} {
		for _, enabled := range []bool{false, true} {
			for _, requested := range []int{1, 5} {
				for _, source := range []string{"channel", "group_image", "group_free", "group_model"} {
					name := fmt.Sprintf("%s/enabled=%t/requested=%d/%s", pipeline, enabled, requested, source)
					t.Run(name, func(t *testing.T) {
						const model = "gemini-3-pro-image"
						groupID := int64(921)
						resolver := newOpenAIImageChannelPricingResolverForTest(t, groupID, model, 0.4)
						cache := resolver.channelService.cache.Load().(*channelCache)
						cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}].ReasoningEffortMultipliers = map[string]float64{"high": 4}
						group := &Group{
							ID: groupID, Platform: PlatformComposite, Hydrated: true, AllowImageGeneration: true,
							ImageCountBilling: enabled, RateMultiplier: 0.5,
							ImageRateIndependent: true, ImageRateMultiplier: 0.25,
						}
						wantTotal, wantMode := 3*0.4*4, BillingModeImage
						switch source {
						case "group_image":
							group.ImagePrice1K, wantTotal = f64p(0.2), 3*0.2
						case "group_free":
							group.ImagePrice1K, wantTotal = f64p(0), 0
						case "group_model":
							group.ImagePrice1K = f64p(7)
							group.ModelPricing = []ChannelModelPricing{{
								Models: []string{model}, BillingMode: BillingModePerRequest, PerRequestPrice: f64p(0.1),
								ReasoningEffortMultipliers: map[string]float64{"high": 2},
							}}
							wantTotal, wantMode = 3*0.1*2, BillingModePerRequest
						}
						apiKey := &APIKey{ID: 1, GroupID: &groupID, Group: group}
						count := resolveBillableImageCount(countOpenAIResponseImageOutputsFromJSONBytes(body), requested, enabled)
						usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
						userRepo := &openAIRecordUsageUserRepoStub{}
						user, account := &User{ID: 2}, &Account{ID: 3}
						effort := "high"
						if pipeline == "gateway" {
							svc := newGatewayRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{})
							svc.resolver = resolver
							require.NoError(t, svc.RecordUsage(context.Background(), &RecordUsageInput{
								Result: &ForwardResult{
									RequestID: name, Model: model, ImageCount: count, ImageSize: "1K",
									ImageEndpoint: true, ReasoningEffort: &effort,
								},
								APIKey: apiKey, User: user, Account: account,
							}))
						} else {
							svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
							svc.resolver = resolver
							require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
								Result: &OpenAIForwardResult{
									RequestID: name, Model: model, ImageCount: count, ImageSize: "1K",
									ImageEndpoint: true, ReasoningEffort: &effort,
								},
								APIKey: apiKey, User: user, Account: account,
							}))
						}
						require.NotNil(t, usageRepo.lastLog)
						require.Equal(t, 3, usageRepo.lastLog.ImageCount)
						require.InDelta(t, wantTotal, usageRepo.lastLog.TotalCost, 1e-12)
						require.InDelta(t, wantTotal*0.25, usageRepo.lastLog.ActualCost, 1e-12)
						require.InDelta(t, wantTotal*0.25, userRepo.lastAmount, 1e-12)
						require.Equal(t, &effort, usageRepo.lastLog.ReasoningEffort)
						require.NotNil(t, usageRepo.lastLog.BillingMode)
						require.Equal(t, string(wantMode), *usageRepo.lastLog.BillingMode)
					})
				}
			}
		}
	}
}

func TestV18HarborBilling_LegacyLongContextOpts(t *testing.T) {
	bs, resolver := newTokenCostTestEnv(t, PlatformGemini, nil, geminiCatalogStub())
	groupID := int64(100)
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{
		ID: groupID, Platform: PlatformGemini, LongContextPricingEnabled: true,
	}}
	svc := &GatewayService{billingService: bs, resolver: resolver}
	result := &ForwardResult{
		Model: "gemini-2.5-pro", Usage: ClaudeUsage{InputTokens: 300000, OutputTokens: 1000},
	}
	for _, tc := range []struct {
		name    string
		opts    recordUsageOpts
		want    float64
		applied bool
	}{
		{name: "ordinary", want: 0.385},
		{name: "legacy", opts: recordUsageOpts{LongContextThreshold: 200000, LongContextMultiplier: 2}, want: 0.51, applied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost := svc.calculateRecordUsageCost(context.Background(), result, apiKey, result.Model, 1, 1,
				time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC), &tc.opts)
			require.NotNil(t, cost)
			require.InDelta(t, tc.want, cost.ActualCost, 1e-12)
			require.Equal(t, tc.applied, cost.LongContextBillingApplied)
		})
	}
}

func TestV18HarborBilling_MixedImagesKeepFreeFastAndSolExemption(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			const model = "gpt-6.1-sol"
			groupID := int64(1)
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			userRepo := &openAIRecordUsageUserRepoStub{}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = newOpenAITokenImageChannelPricingResolverForTest(t, groupID, model)
			svc.channelService = newTestChannelServiceForStats(t, &Channel{ID: 1, Status: StatusActive}, groupID, PlatformOpenAI)
			group := &Group{
				ID: groupID, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 0.5,
				AllowImageGeneration: true, ImageCountBilling: true, ImagePrice1K: f64p(7),
				FreeOpenAIFast: true, LongContextPricingEnabled: true,
				ModelPricing: []ChannelModelPricing{{
					Models: []string{model}, BillingMode: BillingModeToken,
					InputPrice: f64p(2e-6), OutputPrice: f64p(10e-6), ImageOutputPrice: f64p(10e-6),
					CacheReadPrice: f64p(0.1e-6), CacheWritePrice: f64p(2.5e-6), FastMultiplier: f64p(2),
					ReasoningEffortMultipliers: map[string]float64{"high": 1.5},
				}},
			}
			tier, effort := "priority", "high"
			require.NoError(t, svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "v18_harbor_mixed_sol_" + accountType, Model: model, UpstreamModel: model,
					ImageCount: 3, ImageEndpoint: false, ServiceTier: &tier, ReasoningEffort: &effort,
					Usage: OpenAIUsage{
						InputTokens: 300000, OutputTokens: 2000, ImageOutputTokens: 400,
						CacheReadInputTokens: 80000, CacheCreationInputTokens: 20000,
					},
				},
				APIKey: &APIKey{ID: 1, GroupID: &groupID, Group: group}, User: &User{ID: 2},
				Account:   &Account{ID: 3, Platform: PlatformOpenAI, Type: accountType},
				PricingAt: time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC),
			}))
			standardCost := 0.478
			if accountType == AccountTypeAPIKey {
				// The explicit image-output rate is independent of the text ladder.
				standardCost = 0.458*2 + 1600*10e-6*1.5 + 400*10e-6
			}
			log := usageRepo.lastLog
			require.NotNil(t, log)
			require.InDelta(t, standardCost*1.5*2, log.TotalCost, 1e-12, "upstream priority cost remains intact")
			require.InDelta(t, standardCost*1.5*0.5, log.ActualCost, 1e-12, "Free Fast charges Standard")
			require.InDelta(t, log.ActualCost, userRepo.lastAmount, 1e-12)
			require.Equal(t, accountType == AccountTypeAPIKey, log.LongContextBillingApplied)
			require.NotNil(t, log.AccountStatsCost)
			require.InDelta(t, 0.478*2, *log.AccountStatsCost, 1e-12, "account statistics retain their own long-context gate")
			require.Equal(t, string(BillingModeToken), *log.BillingMode)
			require.Equal(t, 3, log.ImageCount)
			require.Equal(t, &effort, log.ReasoningEffort)
		})
	}
}
