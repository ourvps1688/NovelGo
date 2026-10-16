import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Glasses, Sparkles, ScrollText } from "lucide-react";
import { useNovels } from "@/components/novel/useNovels";
import { useChapters } from "@/components/chapter/useChapters";
import { useUpdateNovel } from "@/components/novel/useUpdateNovel";
import ReaderView from "@/components/reader/ReaderView";
import PatternExtractView from "@/components/pattern/PatternExtractView";

type Tab = "chapters" | "reader" | "pattern";

// ReferenceAnalyzeView 是参考小说的专属「只读/分析」视图，与写作目标书的创作工作区
// 完全分离：可查看章节结构、运行读者视角与套路提取等分析工具，但无写章入口。
export default function ReferenceAnalyzeView({ novelId }: { novelId: number }) {
  const { t } = useTranslation();
  const { data: novels = [] } = useNovels();
  const novel = novels.find((n) => n.id === novelId);
  const [tab, setTab] = useState<Tab>("reader");
  const [selectedChapter, setSelectedChapter] = useState<number | null>(null);
  const updateNovel = useUpdateNovel();
  const { data: chapters = [] } = useChapters(novelId);

  const tabs: { id: Tab; label: string; icon: typeof Glasses }[] = [
    { id: "chapters", label: t("shell.chapters"), icon: ScrollText },
    { id: "reader", label: t("shell.readerView"), icon: Glasses },
    { id: "pattern", label: t("shell.extract"), icon: Sparkles },
  ];

  return (
    <div className="flex-1 flex flex-col min-h-0 bg-background">
      {/* 顶部：参考书标识 + 标题 + 转为写作书 */}
      <div className="flex items-center justify-between gap-3 px-6 py-3 border-b shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <span className="px-1.5 py-0.5 rounded bg-amber-500/20 text-amber-600 dark:text-amber-400 text-[11px] font-medium shrink-0">
            {t("novel.referenceBadge")}
          </span>
          <h1 className="text-sm font-semibold truncate">{novel?.title}</h1>
        </div>
        <button
          onClick={async () => {
            if (!novel) return;
            try {
              await updateNovel.mutateAsync({
                id: novel.id,
                input: {
                  title: novel.title,
                  description: novel.description ?? "",
                  genre: novel.genre ?? "",
                  kind: "writing",
                },
              });
            } catch {
              /* 错误由 mutation 上层 toast 处理 */
            }
          }}
          className="shrink-0 h-7 px-3 rounded-md text-xs bg-amber-500/20 hover:bg-amber-500/30 text-amber-800 dark:text-amber-200 font-medium transition-colors"
        >
          {t("novel.convertToWriting")}
        </button>
      </div>

      {/* 提示条 */}
      <div className="px-6 py-1.5 text-xs text-muted-foreground border-b shrink-0">
        {t("novel.referenceReadOnlyHint")}
      </div>

      {/* 子标签 */}
      <div className="flex items-center gap-1 px-6 py-2 border-b shrink-0">
        {tabs.map((tb) => {
          const Icon = tb.icon;
          const active = tab === tb.id;
          return (
            <button
              key={tb.id}
              onClick={() => setTab(tb.id)}
              className={`inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-sm transition-colors ${
                active
                  ? "bg-primary/10 text-primary font-medium border border-primary/15"
                  : "text-muted-foreground hover:bg-muted border border-transparent"
              }`}
            >
              <Icon className="w-4 h-4" />
              {tb.label}
            </button>
          );
        })}
      </div>

      {/* 内容区 */}
      <div className="flex-1 min-h-0 overflow-auto p-6">
        {tab === "reader" && <ReaderView novelId={novelId} />}
        {tab === "pattern" && <PatternExtractView currentNovelId={novelId} />}
        {tab === "chapters" && (
          <div className="max-w-2xl">
            {chapters.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {t("novel.noWorksYet")}
              </p>
            ) : (
              <ul className="space-y-1">
                {chapters.map((ch, idx) => (
                  <li key={ch.id}>
                    <button
                      onClick={() => setSelectedChapter(ch.id)}
                      className={`w-full text-left flex items-center gap-3 px-3 py-2 rounded-md text-sm transition-colors ${
                        selectedChapter === ch.id
                          ? "bg-primary/10 text-primary"
                          : "hover:bg-muted"
                      }`}
                    >
                      <span className="w-6 text-right text-xs text-muted-foreground tabular-nums">
                        {idx + 1}
                      </span>
                      <span className="truncate">{ch.title}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
