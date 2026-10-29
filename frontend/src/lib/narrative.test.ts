// 叙案统一契约测试（P3 WBS T2.1/§0.2）：上限/下限一组词只在 i18n 单源。
// 验收①五落点渲染同一组词；②grep 断言无散装变体；③双语齐。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { createElement } from "react";
import { render, screen, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { join, relative, dirname, resolve } from "node:path";
import { createInstance, type i18n as I18n } from "i18next";
import zhCN from "@/i18n/locales/zh-CN.json";
import en from "@/i18n/locales/en.json";
import InitView from "@/views/InitView";
import StartupOverlay from "@/components/startup/StartupOverlay";
import AboutTab from "@/components/settings/AboutTab";
import ActivityBar from "@/components/shell/ActivityBar";
import JourneyAbsorb from "@/components/journey/JourneyAbsorb";
import JourneyCreate from "@/components/journey/JourneyCreate";
import JourneyQuality from "@/components/journey/JourneyQuality";
import { usePanelStore } from "@/stores/usePanelStore";

// 真实 i18next 实例渲染（断言渲染文案=单源原文，而非 key）
vi.mock("react-i18next", async () => {
  const { createInstance } = await import("i18next");
  const { default: zhCN } = await import("@/i18n/locales/zh-CN.json");
  const i18n = createInstance();
  await i18n.init({
    lng: "zh-CN",
    resources: { "zh-CN": { translation: zhCN } },
    interpolation: { escapeValue: false },
  });
  return {
    useTranslation: () => ({ t: i18n.t.bind(i18n), i18n }),
    initReactI18next: { type: "3rdParty", init: vi.fn() },
  };
});

vi.mock("@/lib/wailsjs/go/app/App", () => ({
  GetPlatform: vi.fn().mockResolvedValue({ defaultPath: "/tmp/novelgo" }),
  Initialize: vi.fn().mockResolvedValue(undefined),
  GetVersion: vi.fn().mockResolvedValue("0.0.0-test"),
  GetBuildHash: vi.fn().mockResolvedValue("deadbeef"),
  // T3.1：JourneyQuality 挂载 WritingConfigTab（useWritingConfig 经此绑定取数）
  GetWritingConfig: vi.fn().mockResolvedValue({
    auto_score_enabled: true,
    score_threshold: 60,
    score_max_retries: 3,
  }),
  SaveWritingConfig: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@/lib/wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
  EventsEmit: vi.fn(),
  BrowserOpenURL: vi.fn(),
  WindowMinimise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  Quit: vi.fn(),
}));

// 解析层实例：断言嵌套（$t(narrative.*)）组词解析结果
const i18n: I18n = createInstance();
await i18n.init({
  lng: "zh-CN",
  resources: { "zh-CN": { translation: zhCN } },
  interpolation: { escapeValue: false },
});
const TAGLINE = i18n.t("narrative.tagline");
const UPPER = i18n.t("narrative.upper");
const LOWER = i18n.t("narrative.lower");

/** 收集 JSON 全部字符串叶子（key 为点路径）。 */
function leaves(obj: object, prefix = ""): Map<string, string> {
  const out = new Map<string, string>();
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === "string") out.set(key, v);
    else if (v && typeof v === "object") {
      for (const [sub, val] of leaves(v as object, key)) out.set(sub, val);
    }
  }
  return out;
}

// ── grep 断言辅助：src 源码剥离注释后查散装组词（禁变体） ──
// 定位 frontend/src：vitest 下 import.meta 形态不稳，按候选逐一探测（防真空扫描）
function findSrcDir(): string {
  const selfDir = (import.meta as { dirname?: string }).dirname ?? ".";
  const candidates = [
    resolve(process.cwd(), "src"),
    resolve(process.cwd(), "frontend/src"),
    resolve(dirname(selfDir), ".."),
    resolve(dirname(selfDir), "..", ".."),
  ];
  for (const dir of candidates) {
    if (existsSync(resolve(dir, "components/chat/ChatPanel.tsx"))) return dir;
  }
  throw new Error(`src dir not found in candidates: ${candidates.join(", ")}`);
}
const SRC_DIR = findSrcDir();
// 与 i18n-check.mjs 同一口径：业务文案数据文件非 UI 文案，不入叙案断言
const ALLOWLIST = new Set([
  "genreTemplates.ts",
  "styleTemplates.ts",
  "platformTemplates.ts",
  "polarStarData.ts",
  "preferenceCategories.ts",
  "bookPitchParse.ts",
]);
const VARIANT_PATTERN = /上限|下限|style ceiling|signability floor/i;

function collectSourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "node_modules" || entry.name === ".git") continue;
      out.push(...collectSourceFiles(full));
    } else if (
      (entry.name.endsWith(".ts") || entry.name.endsWith(".tsx")) &&
      !/\.(test|spec)\.(ts|tsx)$/.test(entry.name) &&
      !ALLOWLIST.has(entry.name)
    ) {
      out.push(full);
    }
  }
  return out;
}

/** 剥离块注释（保行号）与行注释（防 `://` 协议误伤），返回逐行代码部分。 */
function codeLines(content: string): string[] {
  return content
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .split("\n")
    .map((line) => line.replace(/(^|[^:])\/\/.*$/, "$1"));
}

function scanForGroupWordViolations(): string[] {
  const violations: string[] = [];
  for (const file of collectSourceFiles(SRC_DIR)) {
    const rel = relative(SRC_DIR, file).replaceAll("\\", "/");
    codeLines(readFileSync(file, "utf-8")).forEach((code, i) => {
      if (VARIANT_PATTERN.test(code)) {
        violations.push(`${rel}:${i + 1}: ${code.trim()}`);
      }
    });
  }
  return violations;
}

describe("叙案单源（WBS T2.1 §0.2）", () => {
  it("narrative.* 三键与 emptyHint.* 双语齐且非空（验收③）", () => {
    for (const [label, res] of [
      ["zh-CN", zhCN],
      ["en", en],
    ] as const) {
      expect(res.narrative?.tagline?.trim(), label).toBeTruthy();
      expect(res.narrative?.upper?.trim(), label).toBeTruthy();
      expect(res.narrative?.lower?.trim(), label).toBeTruthy();
      expect(res.emptyHint?.voiceExtraction?.trim(), label).toBeTruthy();
    }
    // 双语键集一致
    expect(Object.keys(zhCN.narrative).sort()).toEqual(
      Object.keys(en.narrative).sort(),
    );
    expect(Object.keys(zhCN.emptyHint).sort()).toEqual(
      Object.keys(en.emptyHint).sort(),
    );
  });

  it("zh 叙案逐字=北极星原文（plan §0）", () => {
    expect(zhCN.narrative.tagline).toBe(
      "检测核心守住签约下限，心智提取抬高文风上限",
    );
    expect(zhCN.narrative.upper).toBe("文风上限");
    expect(zhCN.narrative.lower).toBe("签约下限");
  });

  it("工厂组副标题经 $t(narrative.*) 嵌套引单源，渲染保持原文", () => {
    expect(zhCN.shell.factory.absorb.feeds).toBe("喂给$t(narrative.upper)");
    expect(zhCN.shell.factory.quality.feeds).toBe("喂给$t(narrative.lower)");
    expect(en.shell.factory.absorb.feeds).toBe("Feeds the $t(narrative.upper)");
    expect(en.shell.factory.quality.feeds).toBe(
      "Feeds the $t(narrative.lower)",
    );
    expect(i18n.t("shell.factory.absorb.feeds")).toBe(`喂给${UPPER}`);
    expect(i18n.t("shell.factory.quality.feeds")).toBe(`喂给${LOWER}`);
  });

  it("locale 层组词只出现在 narrative.*（单源禁变体）", () => {
    const zhViolations = [...leaves(zhCN)]
      .filter(
        ([key, val]) =>
          (val.includes("文风上限") || val.includes("签约下限")) &&
          !key.startsWith("narrative."),
      )
      .map(([key]) => key);
    expect(zhViolations).toEqual([]);

    const enViolations = [...leaves(en)]
      .filter(
        ([key, val]) =>
          (val.toLowerCase().includes("style ceiling") ||
            val.toLowerCase().includes("signability floor")) &&
          !key.startsWith("narrative."),
      )
      .map(([key]) => key);
    expect(enViolations).toEqual([]);

    // 空态提示（T2.2 挂点）组词同样走嵌套引单源
    expect(zhCN.emptyHint.voiceExtraction).toContain("$t(narrative.upper)");
    expect(en.emptyHint.voiceExtraction).toContain("$t(narrative.upper)");
  });
});

describe("五落点渲染同一组词（WBS T2.1 验收①）", () => {
  beforeEach(() => {
    localStorage.clear();
    usePanelStore.setState({ activePanel: "chapters", sidebarPanel: null });
  });

  it("「一组词」语义锁定：tagline 同时包含 upper/lower 两短语", () => {
    expect(TAGLINE).toContain(UPPER);
    expect(TAGLINE).toContain(LOWER);
  });

  it("落点① onboarding：InitView 首启叙事渲染 tagline", async () => {
    // GetPlatform 异步 effect 在 act 内冲刷，避免 act 告警
    await act(async () => {
      render(createElement(InitView));
    });
    expect(screen.getByTestId("narrative-tagline").textContent).toBe(TAGLINE);
  });

  it("落点① onboarding：StartupOverlay 启动叙事渲染 tagline", () => {
    render(createElement(StartupOverlay, { phase: "initializing" }));
    expect(screen.getByTestId("narrative-tagline").textContent).toBe(TAGLINE);
  });

  it("落点② 三工厂组副标题引用同一组词", () => {
    render(createElement(ActivityBar, { onSelect: () => {} }));
    expect(screen.getByText(`喂给${UPPER}`)).toBeTruthy();
    expect(screen.getByText(`喂给${LOWER}`)).toBeTruthy();
  });

  it("落点③ 三旅程视图头部渲染 tagline", () => {
    // T3.1：JourneyQuality 挂载 WritingConfigTab（useQuery），需 QueryClientProvider
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    for (const Journey of [
      JourneyAbsorb,
      JourneyCreate,
      JourneyQuality,
    ] as const) {
      const { unmount } = render(
        createElement(
          QueryClientProvider,
          { client: qc },
          createElement(Journey),
        ),
      );
      expect(screen.getByTestId("journey-subtitle").textContent).toBe(TAGLINE);
      unmount();
    }
  });

  it("落点④ 空态提示文案（T2.2 挂点）引用同一组词", () => {
    // 挂载由 T2.2 落地；本验收锁定文案单源=固定提示句 + narrative.upper 组词
    const hint = i18n.t("emptyHint.voiceExtraction");
    expect(hint).toContain("可提取参考书心智以保持文风");
    expect(hint).toContain(UPPER);
  });

  it("落点⑤ About 页一句话定位渲染 tagline", async () => {
    // GetVersion/GetBuildHash 异步 effect 在 act 内冲刷，避免 act 告警
    await act(async () => {
      render(createElement(AboutTab));
    });
    expect(screen.getByTestId("narrative-tagline").textContent).toBe(TAGLINE);
  });
});

describe("grep 断言：UI 代码无散装组词变体（WBS T2.1 验收②）", () => {
  it("src 非注释代码不含「上限/下限」及英文组词散装句子", () => {
    const files = collectSourceFiles(SRC_DIR);
    // 覆盖度防真空扫描：已知关键文件必须在扫描清单内
    const rels = files.map((f) => relative(SRC_DIR, f).replaceAll("\\", "/"));
    expect(rels).toContain("components/chat/ChatPanel.tsx");
    expect(rels).toContain("lib/factory.ts");
    expect(rels.length).toBeGreaterThan(50);
    expect(scanForGroupWordViolations()).toEqual([]);
  });
});
