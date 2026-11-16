// #385 曲线口径来源标注契约：第 10 章同屏出现「L0 beat_gap 爽点 0 处」与
// 「曲线卡情绪峰 1 处」，根因是两套输入曲线（L0 确定性基线 vs 生效曲线 L1 优先）。
// 统一的是**表述**，不是数字：文案必须明示口径差异，判定式/阈值/数据源逐字不动。
import { describe, it, expect } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

/** 定位 frontend/src；仓库根为 SRC/../..（用于只读核验 Go 判定源码）。 */
function findSrcDir(): string {
  const candidates = [
    resolve(process.cwd(), "src"),
    resolve(process.cwd(), "frontend/src"),
    resolve(
      dirname((import.meta as { dirname?: string }).dirname ?? "."),
      "..",
      "..",
    ),
    resolve(
      dirname((import.meta as { dirname?: string }).dirname ?? "."),
      "..",
      "..",
      "..",
    ),
  ];
  for (const dir of candidates) {
    if (existsSync(resolve(dir, "components/skill/TensionCurveCard.tsx"))) {
      return dir;
    }
  }
  throw new Error("frontend/src not found");
}

const SRC = findSrcDir();
const ROOT = resolve(SRC, "..", "..");
const read = (rel: string, base: string = SRC) =>
  readFileSync(resolve(base, rel), "utf-8").replace(/\r\n/g, "\n");
const curveI18n = (file: string) =>
  JSON.parse(readFileSync(resolve(SRC, "i18n/locales", file), "utf-8")).journey
    .quality.detect.tensionCurve;

describe("#385 张力曲线 L0/L1 口径来源标注", () => {
  it("中英 chartHint 均写明 L1 优先、L0 不认 L1 标注、两者允许不一致", () => {
    const zh = curveI18n("zh-CN.json");
    const en = curveI18n("en.json");

    expect(zh.chartHint).toContain("L1");
    expect(zh.chartHint).toContain("L0");
    expect(zh.chartHint).toContain("不认 L1 标注");
    expect(zh.chartHint).toContain("允许不一致");

    expect(en.chartHint).toContain("L1");
    expect(en.chartHint).toContain("L0");
    expect(en.chartHint).toContain("ignores L1 labels");
    expect(en.chartHint).toContain("may disagree");
  });

  it("中英 desc 同步标注两套口径不同源", () => {
    expect(curveI18n("zh-CN.json").desc).toContain("不同源");
    expect(curveI18n("en.json").desc).toContain("not the same source");
  });

  it("计数 chip 不写死「L1 标注」（只有 L0 曲线时该措辞会误标）", () => {
    const zh = curveI18n("zh-CN.json");
    const en = curveI18n("en.json");
    expect(zh.climaxSome).toBe("情绪峰 {{count}} 处");
    expect(zh.climaxNone).toBe("无情绪峰");
    expect(en.climaxSome).toBe("{{count}} climax beats");
    expect(en.climaxNone).toBe("No climax beats");
  });

  it("曲线卡仍渲染 chartHint 与 climax_segs，接线不回退", () => {
    const c = read("components/skill/TensionCurveCard.tsx");
    expect(c).toContain("tensionCurve.chartHint");
    expect(c).toContain("markers?.climax_segs");
    expect(c).toContain("GetChapterTensionCurve");
    expect(c).toContain("L1 标注优先于 L0 基线");
  });

  it("只统一表述：Go 判定式 / 结构峰阈值 / 生效曲线合并策略逐字不动", () => {
    const tension = read("internal/quality/tension.go", ROOT);
    const checks = read("internal/quality/tension_checks.go", ROOT);
    const view = read("internal/pipeline/tension_view.go", ROOT);

    expect(tension).toContain("func beatPeakCount(");
    expect(checks).toContain("beatPeakCount(curve)");
    expect(tension).toMatch(/structuralPeakAt\s*=\s*0\.45/);
    expect(view).toContain("EffectiveTensionCurve(rows)");
    expect(view).toContain("MarkTensionCurve(pts)");
    // L0 检测必须仍吃本地确定性基线，不得改成认 L1 标注。
    expect(tension).toContain("BuildTensionCurve(text)");
    expect(tension).toContain("func EffectiveTensionCurve(");
  });
});
