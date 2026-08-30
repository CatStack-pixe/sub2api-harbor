import { describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import HeartbeatView from "../HeartbeatView.vue";

vi.mock("@/api", () => ({
  adminAPI: {
    heartbeat: {
      getLogs: vi.fn().mockResolvedValue({
        items: [],
        total: 0,
        page: 1,
        page_size: 25,
        pages: 0,
      }),
    },
  },
}));

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  };
});

describe("admin HeartbeatView", () => {
  it("renders the standalone heartbeat page and reusable settings panel", async () => {
    const wrapper = mount(HeartbeatView, {
      global: {
        stubs: {
          AppLayout: { template: "<div><slot /></div>" },
          HeartbeatSettingsPanel: {
            template: '<div data-testid="heartbeat-settings-panel-stub" />',
          },
        },
      },
    });
    await flushPromises();

    expect(wrapper.find('[data-testid="heartbeat-view"]').exists()).toBe(true);
    expect(wrapper.find("h1").text()).toBe("admin.settings.heartbeat.title");
    expect(
      wrapper.find('[data-testid="heartbeat-settings-panel-stub"]').exists(),
    ).toBe(true);
  });
});
