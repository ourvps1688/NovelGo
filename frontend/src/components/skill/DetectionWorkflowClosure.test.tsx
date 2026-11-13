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

describe("#114 P1 跨轮对齐与前后对比", () => {
  it("档案卡片展示 resolved/persisted/new 三态与轮次保护提示，仍零自动修复", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    for (const sym of [
      "archive.resolved_count",
      "archive.persisted_count",
      "archive.new_count",
      "archive.guard_keys",
      "roundGuardHint",
      "fixPromptRoundGuard",
    ]) {
      expect(c, `missing: ${sym}`).toContain(sym);
    }
    for (const banned of [
      "StartIssueFixTickets",
      "StartManualIssueFixTickets",
      "onAutoFix",
      "autoFix",
    ]) {
      expect(c, ` banned: ${banned}`).not.toContain(banned);
    }
  });

  it("评分趋势/前后对比两个 queryKey 有失效通道（改稿后不再停在旧数据）", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    expect(c).toContain('["score-trend"]');
    expect(c).toContain('["score-history"]');
  });

  it("前后对比按相邻事件 finding key 快照做集合差，旧数据降级不反推", () => {
    const c = read("components/skill/ScoreTrendPanel.tsx");
    expect(c).toContain("e.finding_keys");
    expect(c).toContain("prev?.finding_keys");
    expect(c).toContain("hasSnapshot");
    expect(c).toContain("findingDiff.resolved");
    expect(c).toContain("findingDiff.noSnapshot");
  });

  it("Wails 生成物透出三态计数、guard_keys 与 finding_keys 字段", () => {
    const models = read("lib/wailsjs/go/models.ts");
    for (const sym of [
      "resolved_count: number;",
      "persisted_count: number;",
      "new_count: number;",
      "guard_keys: string[];",
      "finding_keys: string[];",
    ]) {
      expect(models, `missing: ${sym}`).toContain(sym);
    }
    expect(models).toContain('this.resolved_count = source["resolved_count"]');
    expect(models).toContain('this.guard_keys = source["guard_keys"]');
    expect(models).toContain('this.finding_keys = source["finding_keys"]');
  });

  it("双语键齐全：三态计数、轮次保护、快照差", () => {
    const zh = JSON.parse(read("i18n/locales/zh-CN.json"));
    const en = JSON.parse(read("i18n/locales/en.json"));
    for (const locale of [zh, en]) {
      const d = locale.journey.quality.detect;
      for (const k of ["resolved", "persisted", "new"]) {
        expect(d.findingCounts[k]).toBeTruthy();
        expect(d.scoreTrend.findingDiff[k]).toBeTruthy();
      }
      expect(d.roundGuardHint).toBeTruthy();
      expect(d.fixPromptRoundGuard).toBeTruthy();
      expect(d.scoreTrend.findingDiff.noSnapshot).toBeTruthy();
    }
  });

  it("#114 P2 改稿消息透出 P1 单源稳定 key，不自行造 key", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    expect(c).toContain("archive.finding_keys");
    expect(c).toContain("findingKeysTitle");
    expect(c).toContain("targetKeys");
    expect(c).toContain("keysBlock");
    expect(c).toContain('queryKey: ["feature-usage"]');
    const zh = JSON.parse(read("i18n/locales/zh-CN.json"));
    const en = JSON.parse(read("i18n/locales/en.json"));
    for (const locale of [zh, en]) {
      expect(locale.journey.quality.detect.findingKeysTitle).toContain(
        "【本章目标发现项】",
      );
    }
  });

  it("#114 P2 技能账本与供应商指标都有可见消费面", () => {
    const view = read("components/skill/SkillMatrixView.tsx");
    expect(view).toContain("LLMSignalPanel");
    const panel = read("components/skill/LLMSignalPanel.tsx");
    expect(panel).toContain("GetLLMSignalStats");
    expect(panel).toContain('["feature-usage"');
    expect(panel).toContain("llm-signal-panel");
    expect(panel).toContain("staleTime: 0");
    const usage = read("components/skill/SkillUsagePanel.tsx");
    expect(usage).toContain("last_turn_id");
    expect(usage).toContain("finding_keys");
    const models = read("lib/wailsjs/go/models.ts");
    expect(models).toContain("export class LLMSignalStat");
    expect(models).toContain("last_turn_id: number;");
    expect(models).toContain("finding_keys: string;");
    expect(read("lib/wailsjs/go/app/App.d.ts")).toContain("GetLLMSignalStats");
    const zh = JSON.parse(read("i18n/locales/zh-CN.json"));
    const en = JSON.parse(read("i18n/locales/en.json"));
    for (const locale of [zh, en]) {
      for (const k of [
        "title",
        "hint",
        "provider",
        "model",
        "taskKind",
        "signal",
        "count",
        "lastDate",
        "times",
      ]) {
        expect(locale.skillMatrix.signals[k], `missing: ${k}`).toBeTruthy();
      }
      expect(locale.skillMatrix.usage.lastTurn).toBeTruthy();
      expect(locale.skillMatrix.usage.findingKeys).toBeTruthy();
    }
  });

  it("#114 P2 可观测面板零自动修复、零渲染期时钟", () => {
    const panel = read("components/skill/LLMSignalPanel.tsx");
    for (const banned of [
      "StartIssueFixTickets",
      "StartManualIssueFixTickets",
      "autoFix",
      "onAutoFix",
      "Date.now()",
      "new Date()",
    ]) {
      expect(panel, `must not contain: ${banned}`).not.toContain(banned);
    }
  });

  it("#114 P3 启动检测点一次生效：档案失效、完成事件与防重回执", () => {
    const view = read("components/skill/DetectionWorkflowView.tsx");
    for (const want of [
      "invalidateChapterViews",
      "pipeline:chapter_detect_done",
      "chapterDoneToast",
      "chapterDoneFailed",
      "already_running",
      'queryKey: ["chapter-quality-archive"]',
      'queryKey: ["score-trend"]',
      'queryKey: ["feature-usage"]',
    ]) {
      expect(view, `missing: ${want}`).toContain(want);
    }
    const models = read("lib/wailsjs/go/models.ts");
    expect(models).toContain("already_running");
    const zh = JSON.parse(read("i18n/locales/zh-CN.json"));
    const en = JSON.parse(read("i18n/locales/en.json"));
    for (const locale of [zh, en]) {
      for (const k of [
        "alreadyRunning",
        "chapterDoneToast",
        "chapterDoneFailed",
      ]) {
        expect(locale.journey.quality.detect[k], `missing: ${k}`).toBeTruthy();
      }
    }
    for (const banned of [
      "StartIssueFixTickets",
      "StartManualIssueFixTickets",
      "autoFix",
      "onAutoFix",
    ]) {
      expect(view, `must not contain: ${banned}`).not.toContain(banned);
    }
  });
});

describe("#115 P0 章节检测失败反馈可理解、不叠加", () => {
  it("失败走告警态并按 reason 给可执行文案，完成/发起提示共用同一 toast id", () => {
    const view = read("components/skill/DetectionWorkflowView.tsx");
    for (const want of [
      "CHAPTER_DETECT_TOAST_ID",
      "chapterDoneFailedModel",
      "chapterDoneFailedLLM",
      "chapterDoneFailedReceipt",
      "p.reason",
      "toastError(msg, CHAPTER_DETECT_TOAST_ID)",
      "toastSuccess(",
    ]) {
      expect(view, `missing: ${want}`).toContain(want);
    }
    const toast = read("utils/toast.ts");
    expect(toast).toContain("id?: string");
    expect(toast).toContain("...(id ? { id } : {})");
  });

  it("中英文案不再指向日志，且四类失败文案齐全", () => {
    const zh = JSON.parse(read("i18n/locales/zh-CN.json"));
    const en = JSON.parse(read("i18n/locales/en.json"));
    for (const locale of [zh, en]) {
      const d = locale.journey.quality.detect;
      for (const k of [
        "chapterDoneFailed",
        "chapterDoneFailedModel",
        "chapterDoneFailedLLM",
        "chapterDoneFailedReceipt",
      ]) {
        expect(d[k], `missing: ${k}`).toBeTruthy();
      }
      expect(d.chapterDoneFailed).not.toMatch(/日志|logs/);
      expect(d.chapterDoneFailed).toMatch(/L0/);
    }
  });
});

// #115：判官 verdict 是质量结论（pass/fail/revise），不是程序错误。
// 面板曾缺 fail/revise 映射而裸显英文，用户误以为「代码失败」；锁住本地化与提示。
describe("#115 判官 verdict 本地化与非错误语义", () => {
  it("fail/revise 有映射、中英双语文案，徽章带非程序错误说明", () => {
    const keys = read("components/skill/verdictKeys.ts");
    for (const v of ["fail", "revise"]) {
      expect(keys).toContain(`${v}: "journey.quality.detect.verdict.${v}"`);
    }
    for (const f of ["zh-CN", "en"]) {
      const d = JSON.parse(read(`i18n/locales/${f}.json`)).journey.quality
        .detect;
      expect(d.verdict.fail, `missing verdict.fail in ${f}`).toBeTruthy();
      expect(d.verdict.revise, `missing verdict.revise in ${f}`).toBeTruthy();
      expect(
        d.scoreTrend.verdictHint,
        `missing verdictHint in ${f}`,
      ).toBeTruthy();
    }
    expect(read("components/skill/ScoreTrendPanel.tsx")).toContain(
      "scoreTrend.verdictHint",
    );
  });
});

// #116 轮次保护必须给真实人工出口：连续 3 轮未消除时不能再只显示一句提示，
// 让用户原地「再改一次」空转 21 轮。豁免是纯展示层、换思路仍走中央 chat。
describe("#116 轮次保护的人工出口与零自动修复", () => {
  it("档案卡片提供豁免/撤销/换思路三个可点入口并过滤豁免项", () => {
    const c = read("components/skill/DetectionWorkflowView.tsx");
    for (const want of [
      "onExemptGuard",
      "onUndoExempt",
      "onChangeApproach",
      "GUARD_EXEMPT_STORAGE_KEY",
      "readGuardExemptions",
      "!exemptedChecks.has(f.check)",
      "roundGuardExempt",
      "roundGuardChangeApproach",
    ]) {
      expect(c, `missing: ${want}`).toContain(want);
    }
    expect(c).toContain("useChatLaunchStore.getState().send");
    for (const banned of [
      "StartIssueFixTickets",
      "StartManualIssueFixTickets",
      "onAutoFix",
      "autoFix",
      "UpdateIssueStatus",
    ]) {
      expect(c, `不得复活自动修复: ${banned}`).not.toContain(banned);
    }
  });

  it("中英双语文案齐全，换思路消息不误触发验收硬约束", () => {
    const keys = [
      "roundGuardExempt",
      "roundGuardExemptDone",
      "roundGuardUndo",
      "roundGuardExemptNote",
      "roundGuardChangeApproach",
      "roundGuardChangeTitle",
      "roundGuardChangeBody",
      "roundGuardChangeSent",
    ];
    for (const f of ["zh-CN", "en"]) {
      const d = JSON.parse(read(`i18n/locales/${f}.json`)).journey.quality
        .detect;
      for (const k of keys) {
        expect(d[k], `missing ${k} in ${f}`).toBeTruthy();
      }
      expect(d.roundGuardChangeTitle).not.toContain("【本章验收条件】");
      expect(d.roundGuardChangeBody).toMatch(/阈值|threshold/);
    }
  });
});
