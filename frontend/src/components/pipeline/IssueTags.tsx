import { useTranslation } from "react-i18next";

// issue 三枚举（target_scope/dimension/direction）的标签展示（前向适配提示）：
// - direction=修复方向：forward_fix（前向适配，默认）/ retcon_candidate（建议回改，仅硬矛盾）；
// - target_scope=落点卷作用域：active_volume（本卷）/ frozen_volume（冻结卷）/ none（全书）；
// - dimension=四维机器归属：character / locus_arc / timeline / reader_pov / none。
// 旧版体检/巡检数据可能缺省，缺省的标签不渲染；未知枚举值也不渲染（避免裸 key 泄漏）。
// 契约单源见后端 internal/pipeline/contract.go。

const DIRECTIONS = new Set(["forward_fix", "retcon_candidate"]);
const SCOPES = new Set(["active_volume", "frozen_volume", "none"]);
const DIMENSIONS = new Set([
  "character",
  "locus_arc",
  "timeline",
  "reader_pov",
  "none",
]);

export function IssueScopeTags({
  issue,
}: {
  issue: {
    target_scope?: string;
    dimension?: string;
    direction?: string;
  };
}) {
  const { t } = useTranslation();
  const tags: { key: string; cls: string }[] = [];
  if (issue.direction && DIRECTIONS.has(issue.direction)) {
    tags.push({
      key: `pipeline.issueTags.direction.${issue.direction}`,
      // retcon_candidate 是「要动冻结卷」的警示标签，其余方向标签用主色。
      cls:
        issue.direction === "retcon_candidate"
          ? "bg-warning/10 text-warning-foreground"
          : "bg-primary/10 text-primary",
    });
  }
  if (issue.target_scope && SCOPES.has(issue.target_scope)) {
    tags.push({
      key: `pipeline.issueTags.scope.${issue.target_scope}`,
      cls:
        issue.target_scope === "frozen_volume"
          ? "bg-warning/10 text-warning-foreground"
          : "bg-muted text-muted-foreground",
    });
  }
  if (issue.dimension && DIMENSIONS.has(issue.dimension)) {
    tags.push({
      key: `pipeline.issueTags.dimension.${issue.dimension}`,
      cls: "bg-muted text-muted-foreground",
    });
  }
  if (tags.length === 0) return null;
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {tags.map((tag) => (
        <span
          key={tag.key}
          className={`rounded px-1.5 py-0.5 text-xs ${tag.cls}`}
        >
          {t(tag.key)}
        </span>
      ))}
    </div>
  );
}
