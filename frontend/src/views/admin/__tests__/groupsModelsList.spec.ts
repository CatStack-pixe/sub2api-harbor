import { describe, expect, it } from "vitest";

import {
  addModelMappingRow,
  buildModelsListConfig,
  createModelsListState,
  hydrateModelsListState,
  invertModelsListSelection,
  moveModelsListItem,
  removeModelMappingRow,
  selectAllModelsListItems,
  setModelsListCandidates,
  toggleModelsListItem,
} from "../groupsModelsList";

describe("groupsModelsList", () => {
  it("selects all default candidates for a new disabled config", () => {
    const state = createModelsListState();

    setModelsListCandidates(state, ["gpt-5.5", "gpt-5.4"]);

    expect(state.enabled).toBe(false);
    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true },
      { id: "gpt-5.4", selected: true },
    ]);
  });

  it("builds and preserves a disabled Agnes group model mapping", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["deepseek-v4-pro", "deepseek-v4-flash"],
      model_mapping_enabled: false,
      model_mapping: {
        "deepseek-v4-pro": "agnes-2.5-pro-alpha",
        "deepseek-v4-flash": "agnes-2.5-flash",
      },
    });

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["deepseek-v4-pro", "deepseek-v4-flash"],
      model_mapping_enabled: false,
      model_mapping: {
        "deepseek-v4-pro": "agnes-2.5-pro-alpha",
        "deepseek-v4-flash": "agnes-2.5-flash",
      },
    });
  });

  it("adds, trims, and removes Agnes mapping rows", () => {
    const state = createModelsListState();
    state.modelMappingEnabled = true;
    addModelMappingRow(state);
    state.modelMappingRows[0].requestedModel = " deepseek-v4-pro ";
    state.modelMappingRows[0].upstreamModel = " agnes-2.5-pro-alpha ";
    addModelMappingRow(state);
    const emptyRowID = state.modelMappingRows[1].id;

    expect(buildModelsListConfig(state).model_mapping).toEqual({
      "deepseek-v4-pro": "agnes-2.5-pro-alpha",
    });

    removeModelMappingRow(state, emptyRowID);
    expect(state.modelMappingRows).toHaveLength(1);
  });

  it("keeps saved selections and marks new candidates as unselected when editing", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    });

    setModelsListCandidates(state, ["gpt-5.4", "legacy-gpt", "gpt-5.5"]);

    expect(state.enabled).toBe(true);
    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true },
      { id: "gpt-5.4", selected: true },
      { id: "legacy-gpt", selected: false },
    ]);
  });

  it("preserves explicitly unselected saved candidates when candidates refresh", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    });

    setModelsListCandidates(state, ["gpt-5.5", "gpt-5.4"]);

    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true },
      { id: "gpt-5.4", selected: false },
    ]);
  });

  it("builds config with selected models in current display order", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4", "legacy-gpt"],
    }, ["gpt-5.5", "gpt-5.4", "legacy-gpt"]);

    toggleModelsListItem(state, "legacy-gpt");
    moveModelsListItem(state, 1, 0);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.4", "gpt-5.5"],
    });
  });

  it("keeps selected models in payload even when disabled so reopening can restore choices", () => {
    const state = hydrateModelsListState({
      enabled: false,
      models: ["gpt-5.5"],
    }, ["gpt-5.5", "gpt-5.4"]);

    expect(buildModelsListConfig(state)).toEqual({
      enabled: false,
      models: ["gpt-5.5"],
    });
  });

  it("preserves saved models when candidates have not loaded yet", () => {
    const state = createModelsListState({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    });

    expect(buildModelsListConfig(state)).toEqual({
      enabled: true,
      models: ["gpt-5.5", "gpt-5.4"],
    });
  });

  it("selects all candidate models from the toolbar action", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    }, ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"]);

    selectAllModelsListItems(state);

    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: true },
      { id: "gpt-5.4", selected: true },
      { id: "gpt-5.4-mini", selected: true },
    ]);
  });

  it("inverts selected models from the toolbar action", () => {
    const state = hydrateModelsListState({
      enabled: true,
      models: ["gpt-5.5"],
    }, ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"]);

    invertModelsListSelection(state);

    expect(state.items).toEqual([
      { id: "gpt-5.5", selected: false },
      { id: "gpt-5.4", selected: true },
      { id: "gpt-5.4-mini", selected: true },
    ]);
  });
});
