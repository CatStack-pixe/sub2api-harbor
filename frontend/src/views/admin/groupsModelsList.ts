export interface ModelsListConfig {
  enabled: boolean
  models: string[]
  model_mapping_enabled?: boolean
  model_mapping?: Record<string, string>
}

export interface ModelsListItem {
  id: string
  selected: boolean
}

export interface ModelsListState {
  enabled: boolean
  savedModels: string[]
  items: ModelsListItem[]
  modelMappingEnabled: boolean
  modelMappingRows: ModelMappingRow[]
}

export interface ModelMappingRow {
  id: string
  requestedModel: string
  upstreamModel: string
}

let nextModelMappingRowID = 0

const createModelMappingRow = (
  requestedModel = "",
  upstreamModel = "",
): ModelMappingRow => ({
  id: `model-mapping-${++nextModelMappingRowID}`,
  requestedModel,
  upstreamModel,
})

export const createModelsListState = (
  config?: Partial<ModelsListConfig> | null,
): ModelsListState => ({
  enabled: config?.enabled ?? false,
  savedModels: normalizeModels(config?.models ?? []),
  items: [],
  modelMappingEnabled: config?.model_mapping_enabled ?? false,
  modelMappingRows: Object.entries(config?.model_mapping ?? {}).map(
    ([requestedModel, upstreamModel]) =>
      createModelMappingRow(requestedModel, upstreamModel),
  ),
})

export const hydrateModelsListState = (
  config: Partial<ModelsListConfig> | null | undefined,
  candidates: string[],
): ModelsListState => {
  const state = createModelsListState(config)
  setModelsListCandidates(state, candidates)
  return state
}

export const setModelsListCandidates = (
  state: ModelsListState,
  candidates: string[],
) => {
  const normalizedCandidates = normalizeModels(candidates)
  const currentSelected = new Set(
    state.items.filter(item => item.selected).map(item => item.id),
  )
  const currentKnown = new Set(state.items.map(item => item.id))
  const savedSelected = new Set(state.savedModels)
  const hasExistingItems = state.items.length > 0
  const selectionOrder = normalizeModels([
    ...state.items.map(item => item.id),
    ...state.savedModels,
    ...normalizedCandidates,
  ])

  state.items = selectionOrder.map(id => {
    const selected = hasExistingItems
      ? currentSelected.has(id)
      : state.savedModels.length > 0
        ? savedSelected.has(id)
        : normalizedCandidates.includes(id)

    return {
      id,
      selected: selected && (currentKnown.has(id) || savedSelected.has(id) || state.savedModels.length === 0),
    }
  })
}

export const toggleModelsListItem = (state: ModelsListState, modelID: string) => {
  const item = state.items.find(item => item.id === modelID)
  if (item) {
    item.selected = !item.selected
  }
}

export const selectAllModelsListItems = (state: ModelsListState) => {
  state.items.forEach(item => {
    item.selected = true
  })
}

export const invertModelsListSelection = (state: ModelsListState) => {
  state.items.forEach(item => {
    item.selected = !item.selected
  })
}

export const moveModelsListItem = (
  state: ModelsListState,
  fromIndex: number,
  toIndex: number,
) => {
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= state.items.length ||
    toIndex >= state.items.length
  ) {
    return
  }
  const [item] = state.items.splice(fromIndex, 1)
  state.items.splice(toIndex, 0, item)
}

export const buildModelsListConfig = (state: ModelsListState): ModelsListConfig => {
  const config: ModelsListConfig = {
    enabled: state.enabled,
    models: state.items.length > 0
      ? state.items.filter(item => item.selected).map(item => item.id)
      : [...state.savedModels],
  }
  const modelMapping = normalizeModelMappingRows(state.modelMappingRows)
  if (state.modelMappingEnabled || Object.keys(modelMapping).length > 0) {
    config.model_mapping_enabled = state.modelMappingEnabled
    config.model_mapping = modelMapping
  }
  return config
}

export const addModelMappingRow = (state: ModelsListState) => {
  state.modelMappingRows.push(createModelMappingRow())
}

export const removeModelMappingRow = (
  state: ModelsListState,
  rowID: string,
) => {
  const index = state.modelMappingRows.findIndex(row => row.id === rowID)
  if (index !== -1) {
    state.modelMappingRows.splice(index, 1)
  }
}

const normalizeModelMappingRows = (
  rows: ModelMappingRow[],
): Record<string, string> => {
  const mapping: Record<string, string> = {}
  for (const row of rows) {
    const requestedModel = row.requestedModel.trim()
    const upstreamModel = row.upstreamModel.trim()
    if (requestedModel && upstreamModel) {
      mapping[requestedModel] = upstreamModel
    }
  }
  return mapping
}

const normalizeModels = (models: string[]): string[] => {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of models) {
    const model = raw.trim()
    if (!model || seen.has(model)) {
      continue
    }
    seen.add(model)
    out.push(model)
  }
  return out
}
