package handler

import "github.com/Wei-Shaw/sub2api/internal/service"

func resolveGroupRequestModel(apiKey *service.APIKey, requestedModel string) (string, bool) {
	if apiKey == nil || apiKey.Group == nil {
		return requestedModel, false
	}
	return apiKey.Group.ResolveRequestModel(requestedModel)
}

func effectiveOpenAIForwardModel(routingModel string, groupMapped bool, channelMapping service.ChannelMappingResult) (string, bool) {
	if channelMapping.Mapped {
		return channelMapping.MappedModel, true
	}
	return routingModel, groupMapped
}
