import { useState, useRef, useLayoutEffect, useMemo } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useTranslation } from "react-i18next";
import {
  Plus,
  Pencil,
  Trash2,
  BookOpen,
  Camera,
  Download,
  Upload,
  Wand2,
  Sparkles,
} from "lucide-react";
import BookCover from "@/components/sidebar/BookCover";
import GenerateCoverDialog from "@/components/novel/GenerateCoverDialog";
import { useNovels } from "@/components/novel/useNovels";
import { useNovelStore } from "@/components/novel/useNovelStore";
import type { novel } from "@/lib/wailsjs/go/models";
import { toErrorMessage } from "@/utils/error";

interface Props {
  onSelectNovel: (n: novel.Novel) => void;
  onSaveCover: (novelID: number, file: File) => Promise<void>;
  onImportNovel: () => void;
  onStartBookPitch: () => void;
}

// 与原 grid-cols-[repeat(auto-fill,minmax(180px,1fr))] + gap-5 对齐
const GAP = 20;
const MIN_COL = 180;
const PADDING = 48; // p-6 两侧内边距

export default function BookshelfView({
  onSelectNovel,
  onSaveCover,
  onImportNovel,
  onStartBookPitch,
}: Props) {
  const { t } = useTranslation();
  // 3.2: 数据 + UI 状态从 store/query 订阅（原由 WorkspaceView 注入 props）。
  // useNovels 与 WorkspaceView 共享 novelKeys.all 缓存，不重复 fetch。
  const { data: novels = [] } = useNovels();
  // 参考/写作分栏：kind=reference 为参考小说，其余（含默认 writing）为写作目标书。
  const [tab, setTab] = useState<"writing" | "reference">("writing");
  const writingNovels = useMemo(
    () => novels.filter((n) => n.kind !== "reference"),
    [novels],
  );
  const referenceNovels = useMemo(
    () => novels.filter((n) => n.kind === "reference"),
    [novels],
  );
  const list = tab === "reference" ? referenceNovels : writingNovels;
  const activeNovelId = useNovelStore((s) => s.activeNovelId);
  const setEditingNovel = useNovelStore((s) => s.setEditingNovel);
  const setDeletingNovel = useNovelStore((s) => s.setDeletingNovel);
  const setShowCreateDialog = useNovelStore((s) => s.setShowCreateDialog);
  const setExportNovelId = useNovelStore((s) => s.setExportNovelId);
  const [coverKeys, setCoverKeys] = useState<Record<number, number>>({});
  const [coverError, setCoverError] = useState<Record<number, string>>({});
  const [generateFor, setGenerateFor] = useState<novel.Novel | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const uploadingRef = useRef<number | null>(null);

  // F-5: 网格虚拟滚动——列数随容器宽度自适应（复制原 grid 的 auto-fill 行为），仅渲染可视行。
  const scrollRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const update = () => setWidth(el.clientWidth);
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const innerWidth = Math.max(0, width - PADDING);
  const columnCount = Math.max(
    1,
    Math.floor((innerWidth + GAP) / (MIN_COL + GAP)),
  );
  const rowCount = Math.ceil(list.length / columnCount);

  const rowVirtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 320,
    overscan: 3,
  });

  function triggerCoverUpload(novelID: number) {
    uploadingRef.current = novelID;
    setCoverError((prev) => {
      const next = { ...prev };
      delete next[novelID];
      return next;
    });
    fileInputRef.current?.click();
  }

  function handleCoverClick(novelID: number, e: React.MouseEvent) {
    e.stopPropagation();
    triggerCoverUpload(novelID);
  }

  function handleCoverGenerated(novelID: number) {
    setCoverKeys((prev) => ({
      ...prev,
      [novelID]: (prev[novelID] ?? 0) + 1,
    }));
  }

  async function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file || uploadingRef.current == null) return;
    const novelID = uploadingRef.current;
    uploadingRef.current = null;
    // 清空 input 以便重复选同一文件
    e.target.value = "";
    try {
      await onSaveCover(novelID, file);
      setCoverError((prev) => {
        const next = { ...prev };
        delete next[novelID];
        return next;
      });
      setCoverKeys((prev) => ({
        ...prev,
        [novelID]: (prev[novelID] ?? 0) + 1,
      }));
    } catch (err) {
      setCoverError((prev) => ({
        ...prev,
        [novelID]: toErrorMessage(err, t("novel.coverSaveFailed")),
      }));
    }
  }

  function renderCard(n: novel.Novel) {
    const isReference = n.kind === "reference";
    return (
      <div
        key={n.id}
        className={`group relative flex flex-col rounded-xl border bg-card transition-all duration-200 hover:-translate-y-0.5 hover:shadow-lg hover:border-foreground/15 cursor-pointer select-none overflow-hidden
          ${n.id === activeNovelId ? "ring-2 ring-primary border-primary/30" : ""}`}
      >
        {/* 点击卡片主体切换书 */}
        <div
          className="flex flex-col flex-1 p-3"
          onClick={() => onSelectNovel(n)}
        >
          <div className="w-full aspect-[3/4] mb-3 rounded-sm overflow-hidden relative">
            <BookCover novelId={n.id} refreshKey={coverKeys[n.id]} />
            {/* 悬浮封面操作（参考书只读，不提供封面上传/生成入口） */}
            {!isReference && (
              <div
                onClick={(e) => handleCoverClick(n.id, e)}
                className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-black/40 opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer"
                title={t("novel.changeCover")}
              >
                <Camera className="w-5 h-5 text-white" />
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    setGenerateFor(n);
                  }}
                  className="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-primary text-primary-foreground text-xs font-medium shadow hover:opacity-90 transition-opacity"
                  title={t("novel.aiCover")}
                >
                  <Wand2 className="w-3.5 h-3.5" />
                  {t("novel.aiCover")}
                </button>
              </div>
            )}
          </div>
          {coverError[n.id] && (
            <div className="text-xs text-destructive flex items-center gap-2 mb-1">
              <span className="truncate">{coverError[n.id]}</span>
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  triggerCoverUpload(n.id);
                }}
                className="text-primary hover:underline shrink-0"
              >
                {t("common.retry")}
              </button>
            </div>
          )}
          <h3 className="text-sm font-medium truncate mb-1">{n.title}</h3>
          {n.genre ? (
            <span className="inline-block self-start text-[11px] px-2 py-0.5 rounded-md bg-primary/10 text-primary font-medium border border-primary/15 mb-1.5">
              {n.genre}
            </span>
          ) : (
            <span className="inline-block self-start text-[11px] px-2 py-0.5 rounded-md bg-muted text-muted-foreground border border-border mb-1.5">
              {t("novel.uncategorized")}
            </span>
          )}
          {n.kind === "reference" && (
            <span className="inline-block self-start text-[11px] px-2 py-0.5 rounded-md bg-tag-amber/10 text-tag-amber-foreground font-medium border border-tag-amber/20 mb-1.5 ml-1.5">
              {t("novel.referenceBadge")}
            </span>
          )}
          {n.description && (
            <p className="text-xs text-muted-foreground line-clamp-2 leading-relaxed">
              {n.description}
            </p>
          )}
        </div>

        {/* 悬浮操作按钮 */}
        <div className="absolute top-2 right-2 flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
          <button
            onClick={(e) => {
              e.stopPropagation();
              setExportNovelId(n.id);
            }}
            className="w-7 h-7 flex items-center justify-center rounded-md bg-background/90 border shadow-sm hover:bg-muted transition-colors"
            title={t("novel.export")}
          >
            <Download className="w-3.5 h-3.5 text-muted-foreground" />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              setEditingNovel(n);
            }}
            className="w-7 h-7 flex items-center justify-center rounded-md bg-background/90 border shadow-sm hover:bg-muted transition-colors"
            title={t("novel.edit")}
          >
            <Pencil className="w-3.5 h-3.5 text-muted-foreground" />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              setDeletingNovel(n);
            }}
            className="w-7 h-7 flex items-center justify-center rounded-md bg-background/90 border shadow-sm hover:bg-danger-bg hover:border-danger-border transition-colors"
            title={t("novel.delete")}
          >
            <Trash2 className="w-3.5 h-3.5 text-muted-foreground hover:text-destructive" />
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 flex flex-col min-h-0 bg-background">
      {/* 隐藏文件选择器 */}
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={handleFileChange}
      />

      {/* 顶部工具栏 */}
      <div className="flex items-center justify-between px-6 py-4 border-b shrink-0 gap-3 flex-wrap">
        <div className="flex items-center gap-1">
          <button
            onClick={() => setTab("writing")}
            className={`h-8 px-3 rounded-md text-sm transition-colors ${
              tab === "writing"
                ? "bg-primary/10 text-primary font-medium border border-primary/15"
                : "text-muted-foreground hover:bg-muted border border-transparent"
            }`}
          >
            {t("novel.tabWriting")}
            <span className="ml-1.5 text-xs opacity-70">
              {writingNovels.length}
            </span>
          </button>
          <button
            onClick={() => setTab("reference")}
            className={`h-8 px-3 rounded-md text-sm transition-colors ${
              tab === "reference"
                ? "bg-tag-amber/10 text-tag-amber-foreground font-medium border border-tag-amber/20"
                : "text-muted-foreground hover:bg-muted border border-transparent"
            }`}
          >
            {t("novel.tabReference")}
            <span className="ml-1.5 text-xs opacity-70">
              {referenceNovels.length}
            </span>
          </button>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={onImportNovel}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-sm border hover:bg-muted transition-colors"
          >
            <Upload className="w-4 h-4" />
            {t("novel.importBook")}
          </button>
          <button
            onClick={onStartBookPitch}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-sm border border-primary/30 text-primary hover:bg-primary/5 transition-colors"
          >
            <Sparkles className="w-4 h-4" />
            {t("novel.aiPitchNewBook")}
          </button>
          <button
            onClick={() => setShowCreateDialog(true)}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-sm bg-primary text-primary-foreground hover:opacity-90 transition-opacity"
          >
            <Plus className="w-4 h-4" />
            {t("novel.newWork")}
          </button>
        </div>
      </div>

      {/* 空状态 */}
      {novels.length === 0 ? (
        <div className="flex-1 flex items-center justify-center p-8">
          <div className="flex flex-col items-center gap-3 text-muted-foreground border-2 border-dashed border-border rounded-2xl px-16 py-12">
            <BookOpen className="w-12 h-12 opacity-40" />
            <p className="text-sm">{t("novel.noWorksYet")}</p>
          </div>
        </div>
      ) : list.length === 0 ? (
        <div className="flex-1 flex items-center justify-center p-8">
          <div className="flex flex-col items-center gap-3 text-muted-foreground border-2 border-dashed border-border rounded-2xl px-16 py-12">
            <BookOpen className="w-12 h-12 opacity-40" />
            <p className="text-sm">
              {tab === "reference"
                ? t("novel.tabReference") + " " + t("novel.noWorksYet")
                : t("novel.noWorksYet")}
            </p>
          </div>
        </div>
      ) : (
        /* 书架网格（F-5: 按行虚拟滚动，列数自适应容器宽度） */
        <div
          ref={scrollRef}
          className="flex-1 overflow-y-auto overscroll-contain p-6"
        >
          <div
            style={{
              height: rowVirtualizer.getTotalSize(),
              position: "relative",
              width: "100%",
            }}
          >
            {rowVirtualizer.getVirtualItems().map((vr) => {
              const start = vr.index * columnCount;
              const rowNovels = list.slice(start, start + columnCount);
              return (
                <div
                  key={vr.key}
                  data-index={vr.index}
                  ref={rowVirtualizer.measureElement}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${vr.start}px)`,
                    paddingBottom: GAP,
                  }}
                >
                  <div
                    className="grid gap-5"
                    style={{
                      gridTemplateColumns: `repeat(${columnCount}, minmax(${MIN_COL}px, 1fr))`,
                    }}
                  >
                    {rowNovels.map((n) => renderCard(n))}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      )}

      <GenerateCoverDialog
        open={!!generateFor}
        novel={generateFor}
        onClose={() => setGenerateFor(null)}
        onGenerated={handleCoverGenerated}
      />
    </div>
  );
}
