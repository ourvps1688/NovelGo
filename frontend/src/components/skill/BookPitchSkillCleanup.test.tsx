// book-pitch 常驻治理契约：孵化期注入必需、转正后移除、绝不误删用户其他勾选、
// 绝不批量改写历史小说的启用清单。等价于 always 的 .enabled.json 会进每轮
// system 消息，转正后仍留着 book-pitch 会持续稀释注意力并占用常驻 rune 帽。
import { describe, it, expect } from "vitest";
import { existsSync, readFileSync } from "node:fs";
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
      existsSync(resolve(dir, "components/skill/BookPitchWorkflowView.tsx"))
    ) {
      return dir;
    }
  }
  throw new Error("frontend/src not found");
}

const SRC = findSrcDir();
const read = (rel: string) =>
  readFileSync(resolve(SRC, rel), "utf-8").replace(/\r\n/g, "\n");
const view = () => read("components/skill/BookPitchWorkflowView.tsx");

/** 只看采纳/转正这一段，避免命中文件里同名的其他分支。 */
function adoptBlock(): string {
  const c = view();
  const start = c.indexOf("async function handleAdopt()");
  const end = c.indexOf("const buttonDisabled", start);
  expect(start).toBeGreaterThan(-1);
  return c.slice(start, end === -1 ? undefined : end);
}

describe("book-pitch 孵化期启用与转正后移除", () => {
  it("孵化启动仍注入 book-pitch，继续孵化时按现有清单追加", () => {
    const c = view();
    expect(c).toContain("base.add(SKILL_NAME)");
    expect(c).toContain(
      "SetEnabledSkills({ novel_id: n.id, names: ADOPT_SKILLS })",
    );
    // 全量覆盖只允许发生在「新建孵化器」一次；采纳路径不得再写死技能集。
    expect(c.split("names: ADOPT_SKILLS").length - 1).toBe(1);
  });

  it("孵化完成按后端当前清单过滤移除，保留用户自行勾选的其他技能", () => {
    const b = adoptBlock();
    expect(b).toContain("await GetEnabledSkills(n.id)");
    expect(b).toContain("current.filter((name) => name !== SKILL_NAME)");
    expect(b).toContain("SetEnabledSkills({ novel_id: n.id, names: rest })");
    expect(b).toContain("qc.setQueryData(skillKeys.enabled(n.id), rest)");
    expect(b).not.toContain("names: []");
    expect(b).not.toContain("names: ADOPT_SKILLS");
    // 清理必须发生在清空孵化器之前，否则本轮就不再知道该清哪本书。
    expect(b.indexOf("names: rest")).toBeLessThan(
      b.indexOf("setIncubatorId(null)"),
    );
  });

  it("清理失败不阻塞建书", () => {
    const b = adoptBlock();
    expect(b).toContain("技能清理失败不阻塞建书");
    expect(b.indexOf("技能清理失败不阻塞建书")).toBeLessThan(
      b.indexOf("setIncubatorId(null)"),
    );
    expect(b).toContain('toastSuccess(t("bookpitch.savedToast"))');
  });

  it("只清理当前孵化书，不批量改写历史小说，不碰新书向导的策略套用入口", () => {
    const c = view();
    for (const banned of ["ListNovels(", "novels.map", ".enabled.json"]) {
      expect(c).not.toContain(banned);
    }
    expect(read("components/novel/NewBookWizard.tsx")).toContain(
      "names: strategy.skills",
    );
  });
});
