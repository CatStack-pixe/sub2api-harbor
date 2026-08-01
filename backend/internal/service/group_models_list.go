package service

import "strings"

func normalizeGroupModelsListConfig(cfg GroupModelsListConfig) GroupModelsListConfig {
	out := GroupModelsListConfig{
		Enabled:             cfg.Enabled,
		ModelMappingEnabled: cfg.ModelMappingEnabled,
	}

	if len(cfg.Models) > 0 {
		seen := make(map[string]struct{}, len(cfg.Models))
		out.Models = make([]string, 0, len(cfg.Models))
		for _, model := range cfg.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			out.Models = append(out.Models, model)
		}
		if len(out.Models) == 0 {
			out.Models = nil
		}
	}

	if len(cfg.ModelMapping) > 0 {
		out.ModelMapping = make(map[string]string, len(cfg.ModelMapping))
		for requestedModel, upstreamModel := range cfg.ModelMapping {
			requestedModel = strings.TrimSpace(requestedModel)
			upstreamModel = strings.TrimSpace(upstreamModel)
			if requestedModel == "" || upstreamModel == "" {
				continue
			}
			out.ModelMapping[requestedModel] = upstreamModel
		}
		if len(out.ModelMapping) == 0 {
			out.ModelMapping = nil
		}
	}
	return out
}

func (g *Group) CustomModelsListEnabled() bool {
	return g != nil && g.ModelsListConfig.Enabled && len(g.ModelsListConfig.Models) > 0
}

// RequestModelMappingEnabled reports whether this Agnes group rewrites public model aliases.
func (g *Group) RequestModelMappingEnabled() bool {
	return g != nil &&
		g.Platform == PlatformAgnes &&
		g.ModelsListConfig.ModelMappingEnabled &&
		len(g.ModelsListConfig.ModelMapping) > 0
}

// ResolveRequestModel maps a group-level public alias to its Agnes upstream model.
func (g *Group) ResolveRequestModel(requestedModel string) (string, bool) {
	if !g.RequestModelMappingEnabled() {
		return requestedModel, false
	}
	mappedModel, matched := resolveRequestedModelInMapping(g.ModelsListConfig.ModelMapping, strings.TrimSpace(requestedModel))
	if !matched || strings.TrimSpace(mappedModel) == "" {
		return requestedModel, false
	}
	return strings.TrimSpace(mappedModel), true
}
