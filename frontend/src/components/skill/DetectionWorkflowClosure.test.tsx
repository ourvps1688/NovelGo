// #113 章节检测闭环契约：质量档案卡片必须提供「勾选 → 发到 chat → 重新检测」
// 的人工入口，且零自动修复；生效字数口径须与检测 L0 同源。
import { describe, it, expect } from "vitest";
import { existsSync } from "node:fs";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

/** 定位 frontend/src：vitest 下 import.meta.dirname 形态不稳，按候选逐一探测。 */
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
    if (
      existsSync(resolve(dir, "components/skill/DetectionWorkflowView.tsx"))
    ) {
      return dir;
    }
  }
  throw new Error("frontend/src not found");
}

const SRC = findSrcDir();
const read = (rel: string) =>
  readFileSync(resolve(SRC, rel), "utf-8").replace(/\r\n/g, "\n");

describe("#113 章节质量档案闭环", () => {
  it("卡片有勾选框、发到 chat、重新检测三个可点元素", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    expect(c).toContain('type="checkbox"');
    expect(c).toContain("onSendToChat");
    expect(c).toContain("onRerun");
    expect(c).toContain('RunDetectionTask(novelId, "chapter", chapterId)');
  });

  it("复用工单面板唯一 chat 通道，不新增工单、不派发后台自动修复", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    expect(c).toContain("useChatLaunchStore.getState().send");
    // 零自动修复：不得出现自动派发入口或退役符号。
    for (const banned of [
      "StartIssueFixTickets",
      "StartManualIssueFixTickets",
      "onAutoFix",
      "autoFix",
    ]) {
      expect(c, ` banned: ${banned}`).not.toContain(banned);
    }
  });

  it("档案卡片不依赖「已手动检测过一次」即可展示", () => {
    // 写后链路 L0 已产出档案；若仍以 last_run_at 为门槛，首章写完看不到发现项。
    const c = read("components/skill/DetectionWorkflowView.tsx");
    expect(c).toContain("showChapterArchive");
    expect(c).toContain("st?.last_run_at || showChapterArchive");
    expect(c).not.toContain(
      "!!st?.last_run_at && !!effChapterId && archiveOpen",
    );
    // st 可缺省：章节结果区内的取数一律可选链，避免白屏级 TypeError。
    expect(c).toContain("st?.last_verdict");
  });

  it("档案卡片展示与检测 L0 同源的生效字数口径", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    expect(c).toContain("archive.words_target_label");
    expect(c).toContain("journey.quality.detect.wordsTarget");
  });

  it("双语键齐全：闭环操作与修复处方模板", () => {
    const zh = JSON.parse(read("i18n/locales/zh-CN.json"));
    const en = JSON.parse(read("i18n/locales/en.json"));
    const keys = [
      "selectFinding",
      "selectFindingsHint",
      "sendToChat",
      "rerunChapter",
      "rerunSent",
      "closureHint",
      "chatNotReady",
      "fixSent",
      "currentChapter",
      "wordsTarget",
      "wordsTargetFallback",
      "fixPromptTitle",
      "fixPromptLedger",
      "fixPromptWords",
      "fixPromptFooter",
    ];
    for (const key of keys) {
      expect(zh.journey.quality.detect[key], `zh ${key}`).toBeTruthy();
      expect(en.journey.quality.detect[key], `en ${key}`).toBeTruthy();
    }
  });

  it("Wails 生成物透出 words_target_label 字段", () => {
    const models = read("lib/wailsjs/go/models.ts");
    expect(models).toContain("words_target_label: string;");
    expect(models).toContain(
      'this.words_target_label = source["words_target_label"]',
    );
  });

  it("#114 验收处方随 chat 消息发出（含确定性标记），DTO 与双语键齐备", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    expect(c).toContain("archive.prescriptions");
    expect(c).toContain("acceptanceTitle");
    expect(c).toContain("criteriaBlock");
    expect(c).toContain("startsWith(`[${f.check}]`)");
    const zh = JSON.parse(read("i18n/locales/zh-CN.json"));
    const en = JSON.parse(read("i18n/locales/en.json"));
    expect(zh.journey.quality.detect.acceptanceTitle).toContain(
      "【本章验收条件】",
    );
    expect(en.journey.quality.detect.acceptanceTitle).toContain(
      "【本章验收条件】",
    );
    const models = read("lib/wailsjs/go/models.ts");
    expect(models).toContain("prescriptions: string[]");
    expect(models).toContain('this.prescriptions = source["prescriptions"]');
  });
});
