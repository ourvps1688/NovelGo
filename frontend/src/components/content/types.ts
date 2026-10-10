export type EditorTab = {
  id: string;
  type: "file" | "diff";
  path: string;
  outlinePath?: string;
  title: string;
  // file tab
  content?: string;
  outlineContent?: string;
  contentBase?: string;
  outlineContentBase?: string;
  isDirty?: boolean;
  outlineIsDirty?: boolean;
  contentConflict?: boolean;
  outlineContentConflict?: boolean;
  contentNeedsRefresh?: boolean;
  outlineNeedsRefresh?: boolean;
  viewMode?: "content" | "outline" | "outline-edit" | "preview" | "edit";
  readOnly?: boolean;
  // diff tab
  diff?: string;
  original?: string;
  modified?: string;
  changeType?: string;
  reason?: string;
  toolId?: string;
};

export function novelgoPath(): string {
  return "novelgo.md";
}

export function isContentPath(p: string): boolean {
  return p.startsWith("chapters/") || p === "novelgo.md";
}

export function isOutlinePath(p: string): boolean {
  return p.startsWith("outlines/");
}

export function isVolumeOutlinePath(p: string): boolean {
  return /^volumes\/id_[1-9]\d*\.md$/.test(p);
}

export function isStandaloneMarkdownPath(p: string): boolean {
  return p === "novelgo.md" || isVolumeOutlinePath(p);
}

export function isSkillPath(p: string): boolean {
  return (
    p.startsWith("skills/") ||
    p.startsWith("~/.novelgo/skills/") ||
    p.startsWith("/builtin/skills/")
  );
}

export type SkillSource = "builtin" | "user" | "novel";

// sourceFromPath infers the skill layer from its tab path.
// Mirrors the path conventions in SkillList.skillPath.
export function sourceFromPath(p: string): SkillSource {
  if (p.startsWith("/builtin/skills/")) return "builtin";
  if (p.startsWith("~/.novelgo/skills/")) return "user";
  return "novel";
}

export function skillNameFromPath(p: string): string {
  return p.replace(/.*\//, "").replace(".md", "");
}

// splitFrontmatter splits YAML frontmatter from markdown content.
export function splitFrontmatter(content: string): {
  meta: Record<string, string>;
  body: string;
} {
  if (!content.startsWith("---")) {
    return { meta: {}, body: content };
  }
  const end = content.indexOf("\n---", 3);
  if (end === -1) {
    return { meta: {}, body: content };
  }
  const fm = content.substring(3, end).trim();
  const body = content.substring(end + 4).trim();
  const meta: Record<string, string> = {};
  for (const line of fm.split("\n")) {
    const i = line.indexOf(":");
    if (i > 0) {
      meta[line.substring(0, i).trim()] = line.substring(i + 1).trim();
    }
  }
  return { meta, body };
}
