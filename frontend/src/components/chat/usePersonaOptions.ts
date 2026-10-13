import { useMemo } from "react";
import { useSkills } from "@/components/skill/useSkills";

// PersonaOption 是写作人格选择器的可选项：来自本书已安装技能。
export interface PersonaOption {
  kind: "skill" | "author";
  ref: string;
  label: string;
  description?: string;
}

// usePersonaOptions: 从本书已安装技能派生写作人格可选项：
//  - 每个 mental 技能 → kind:"skill", ref: 技能名
//  - 每个 distinct source_author（在 mental 技能中）→ kind:"author", ref: 规范作者名
// 优先用聚合档（aggregated=true）的作者，避免把同一作者的单书档重复列出。
// 与后端 resolvePersonaContent（author 优先聚合档）保持一致。
export function usePersonaOptions(novelId: number): PersonaOption[] {
  const skillsQuery = useSkills(novelId);
  return useMemo(() => {
    const list = skillsQuery.data ?? [];
    const mental = list.filter((s) => s.kind === "mental");

    const options: PersonaOption[] = mental.map((s) => ({
      kind: "skill",
      ref: s.name,
      label: s.name,
      description: s.description,
    }));

    const seenAuthors = new Set<string>();
    // 先放聚合档作者（规范心智）
    for (const s of mental) {
      if (s.aggregated && s.source_author) {
        const key = s.source_author.trim().toLowerCase();
        if (!seenAuthors.has(key)) {
          seenAuthors.add(key);
          options.push({
            kind: "author",
            ref: s.source_author,
            label: s.source_author,
            description: "作者创作心智（聚合）",
          });
        }
      }
    }
    // 再放单书档作者（去重）
    for (const s of mental) {
      if (!s.aggregated && s.source_author) {
        const key = s.source_author.trim().toLowerCase();
        if (!seenAuthors.has(key)) {
          seenAuthors.add(key);
          options.push({
            kind: "author",
            ref: s.source_author,
            label: s.source_author,
            description: "作者创作心智（单书）",
          });
        }
      }
    }
    return options;
  }, [skillsQuery.data]);
}
