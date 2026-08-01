package domain

// GroupModelsListConfig controls the optional custom /v1/models response list.
type GroupModelsListConfig struct {
	Enabled             bool              `json:"enabled"`
	Models              []string          `json:"models,omitempty"`
	ModelMappingEnabled bool              `json:"model_mapping_enabled,omitempty"`
	ModelMapping        map[string]string `json:"model_mapping,omitempty"`
}
