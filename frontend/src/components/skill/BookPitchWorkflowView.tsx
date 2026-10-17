import { useCallback, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Sparkles, Play, Check, AlertTriangle, Loader2 } from "lucide-react";
import {
  useEnabledSkills,
  useSetEnabledSkills,
} from "@/components/chat/useNovelSkills";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { useBookPitchStore } from "@/stores/useBookPitchStore";
import { useCreateNovel } from "@/components/novel/useCreateNovel";
import { useUpdateNovel } from "@/components/novel/useUpdateNovel";
import { useNovelStore } from "@/components/novel/useNovelStore";
import {
  CATEGORY_CREATIVE,
  CATEGORY_PLATFORM,
} from "@/components/preference/preferenceCategories";
import { CreatePreference, SetEnabledSkills } from "@/lib/wailsjs/go/app/App";
import type { app } from "@/lib/wailsjs/go/models";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

// 被编排的 manual 技能名（经 SetEnabledSkills 常驻注入 system 上下文）。
const SKILL_NAME = "book-pitch";

// 仅承载阶段 key；技能标识与 kickoff 提示均经 i18n 注入（避免硬编码中文）。
const STAGES = ["stage0", "stage1", "stage2", "stage3"];

// 建书后默认启用的技能集（让本书后续能继续用 book-pitch 打磨外包装）。
const ADOPT_SKILLS = ["book-pitch"];

interface Props {
  novelId: number;
  onCreated: (novelId: number) => void | Promise<void>;
}

export default function BookPitchWorkflowView({ novelId, onCreated }: Props) {
  const { t } = useTranslation();
  const [launching, setLaunching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  // 启动前必须输入的灵感/梗/关键词；会拼进 kickoff，避免 LLM 无的放矢。
  const [ideaDraft, setIdeaDraft] = useState("");

  // 「采纳方案并建书」表单（仅书名必填；题材/平台/风格为引导项，可空）。
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [tags, setTags] = useState("");
  const [direction, setDirection] = useState("");
  const [genre, setGenre] = useState("");
  const [platform, setPlatform] = useState("");
  const [style, setStyle] = useState("");

  const titleInputRef = useRef<HTMLInputElement>(null);
  const adoptSectionRef = useRef<HTMLDivElement>(null);

  const enabledQuery = useEnabledSkills(novelId);
  const setEnabled = useSetEnabledSkills(novelId);
  const modelsQuery = useModels();
  const settingsQuery = useSettings();
  const createNovel = useCreateNovel();
  const updateNovel = useUpdateNovel();
  const switchNovel = useNovelStore((s) => s.switchNovel);

  // 新书孵化器小说 id 提升到全局 store：
  // - 卸载 BookPitchWorkflowView 后仍保留，切回 bookpitch 面板可继续。
  // - 保存/取消后由 workflow 清空，避免把已建好的书再次当成孵化器。
  const incubatorId = useBookPitchStore((s) => s.incubatorId);
  const setIncubatorId = useBookPitchStore((s) => s.setIncubatorId);
  const isIncubating = incubatorId !== null && incubatorId === novelId;

  const enabledSet = useMemo(
    () => new Set(enabledQuery.data ?? []),
    [enabledQuery.data],
  );
  const isEnabled = enabledSet.has(SKILL_NAME);

  const selectedKey = settingsQuery.data?.selected_model_key || "";
  const hasModel = useMemo(
    () => !!modelsQuery.data?.find((m) => m.Key === selectedKey),
    [modelsQuery.data, selectedKey],
  );

  const handleStart = useCallback(async () => {
    if (!hasModel) {
      toastError(t("bookpitch.configureModelFirst"));
      return;
    }
    const idea = ideaDraft.trim();
    if (!idea) {
      toastError(t("bookpitch.ideaRequired"));
      return;
    }

    // 已经在孵化当前书：仅继续发 kickoff（用户可能想追加灵感或重新发起讨论）。
    if (isIncubating) {
      const send = useChatLaunchStore.getState().send;
      if (!send) {
        toastError(t("bookpitch.launchFailed"));
        return;
      }
      setLaunching(true);
      try {
        const base = new Set(enabledQuery.data ?? []);
        base.add(SKILL_NAME);
        await setEnabled.mutateAsync([...base]);
        send(t("bookpitch.kickoff", { idea }));
      } catch (err) {
        toastError(toErrorMessage(err, t("bookpitch.enableFailed")));
      } finally {
        setLaunching(false);
      }
      return;
    }

    // 新建孵化器：无论当前有没有 active 书，都创建一本临时小说作为讨论场。
    // 这样 bookpitch 始终是「开新书」流程，不会误把当前已有书当成工作对象。
    setLaunching(true);
    try {
      const seedTitle = idea.slice(0, 20);
      const n = await createNovel.mutateAsync({
        title: seedTitle,
        description: idea.slice(0, 200),
        genre: "",
      });
      // 新书无既有技能，直接启用 book-pitch 作为孵化技能。
      try {
        await SetEnabledSkills({ novel_id: n.id, names: ADOPT_SKILLS });
      } catch {
        // 技能启用失败不阻塞建书，静默忽略。
      }
      // 把这本临时小说标记为当前孵化器。
      setIncubatorId(n.id);
      await switchNovel(n.id);
      // 写入待发 kickoff：交由 ChatPanel 在新 novelId 上下文就绪（send 重新注册、
      // 模型恢复）后自动发送，规避入口面板直接取 send 时闭包仍是旧 novelId 的竞态。
      const kickoff = t("bookpitch.kickoff", { idea });
      useChatLaunchStore.getState().setPendingKickoff(kickoff);
    } catch (err) {
      toastError(toErrorMessage(err, t("bookpitch.launchFailed")));
    } finally {
      setLaunching(false);
    }
  }, [
    novelId,
    hasModel,
    ideaDraft,
    isIncubating,
    enabledQuery.data,
    setEnabled,
    createNovel,
    switchNovel,
    setIncubatorId,
    t,
  ]);

  const canSave = title.trim().length > 0 && !saving;

  async function handleAdopt() {
    if (!canSave || !isIncubating || incubatorId == null) return;
    setSaving(true);
    setError("");
    try {
      // 直接更新孵化器小说，不会产生第二本。
      const n = await updateNovel.mutateAsync({
        id: incubatorId,
        input: {
          title: title.trim(),
          description: description.trim(),
          genre: genre.trim(),
          kind: "",
        },
      });

      // 启用 book-pitch（新书无既有技能，直接 set）。
      try {
        await SetEnabledSkills({ novel_id: n.id, names: ADOPT_SKILLS });
      } catch {
        // 技能启用失败不阻塞建书，静默忽略。
      }

      // 引导项：平台 / 风格 非空则写入本书偏好（题材已写入 novel.genre 字段）。
      const extra: Array<{ category: string; content: string }> = [];
      if (platform.trim())
        extra.push({ category: CATEGORY_PLATFORM, content: platform.trim() });
      if (style.trim())
        extra.push({ category: CATEGORY_CREATIVE, content: style.trim() });
      for (const e of extra) {
        try {
          await CreatePreference(n.id, {
            is_global: false,
            category: e.category,
            content: e.content,
          } as app.CreatePreferenceInput);
        } catch {
          // 偏好写入失败不阻塞建书，静默忽略。
        }
      }

      // 孵化完成：清空孵化器，避免已建好的书被再次当成孵化器。
      setIncubatorId(null);
      toastSuccess(t("bookpitch.savedToast"));
      await onCreated(n.id);
    } catch (e) {
      setError(toErrorMessage(e, t("novel.saveFailedRetry")));
    } finally {
      setSaving(false);
    }
  }

  const buttonDisabled = launching || !hasModel;
  const buttonTitle = !hasModel ? t("bookpitch.configureModelFirst") : "";

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <div className="px-6 py-4 border-b shrink-0">
        <div className="flex items-center gap-2">
          <Sparkles className="w-5 h-5 text-primary" />
          <h1 className="text-lg font-semibold">{t("bookpitch.title")}</h1>
          {isEnabled && (
            <span className="inline-flex items-center gap-1 rounded-full bg-primary/10 text-primary border border-primary/30 px-2 py-0.5 text-[11px]">
              <Check className="w-3 h-3" />
              {t("bookpitch.enabledBadge")}
            </span>
          )}
        </div>
        <p className="text-sm text-muted-foreground mt-1 max-w-3xl">
          {t("bookpitch.subtitle")}
        </p>
        <div className="mt-4 max-w-3xl">
          <label className="block text-xs font-medium text-muted-foreground mb-1.5">
            {t("bookpitch.ideaLabel")}
          </label>
          <textarea
            value={ideaDraft}
            onChange={(e) => setIdeaDraft(e.target.value)}
            placeholder={t("bookpitch.ideaPlaceholder")}
            rows={2}
            className="w-full rounded-md border bg-background px-3 py-2 text-sm resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto overscroll-contain px-6 py-4">
        <div className="space-y-3 max-w-3xl">
          {STAGES.map((key, idx) => (
            <div key={key} className="flex gap-3 rounded-lg border bg-card p-3">
              <div className="shrink-0 w-7 h-7 rounded-full bg-primary/10 text-primary flex items-center justify-center text-sm font-semibold">
                {idx}
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-2">
                  <h2 className="text-sm font-semibold">
                    {t(`bookpitch.${key}Title`)}
                  </h2>
                  <code className="shrink-0 text-[11px] text-muted-foreground font-mono">
                    {t(`bookpitch.${key}Skill`)}
                  </code>
                </div>
                <p className="mt-1 text-[12px] text-muted-foreground">
                  <span className="text-foreground/80">
                    {t("bookpitch.goalLabel")}
                  </span>
                  {t(`bookpitch.${key}Goal`)}
                </p>
                <p className="mt-0.5 text-[12px] text-muted-foreground">
                  <span className="text-foreground/80">
                    {t("bookpitch.doneLabel")}
                  </span>
                  {t(`bookpitch.${key}Done`)}
                </p>
              </div>
            </div>
          ))}
        </div>

        {/* 采纳方案并建书：只在当前小说确实是进行中的孵化器时显示；
            否则说明用户刚进入 bookpitch 面板，还没开始新的孵化流程。 */}
        {isIncubating ? (
          <div
            ref={adoptSectionRef}
            className="mt-5 rounded-lg border bg-muted/30 p-4 max-w-3xl space-y-3"
          >
            <div>
              <h2 className="text-sm font-semibold">
                {t("bookpitch.adoptTitle")}
              </h2>
              <p className="text-[12px] text-muted-foreground mt-1">
                {t("bookpitch.adoptHint")}
              </p>
            </div>

            {error && (
              <p className="text-sm text-destructive bg-danger-bg border border-danger-border rounded-md px-3 py-2">
                {error}
              </p>
            )}

            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                {t("bookpitch.fieldTitleReq")}
              </label>
              <input
                ref={titleInputRef}
                type="text"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={t("novel.enterBookTitle")}
                className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                {t("bookpitch.fieldSummary")}
              </label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t("novel.summaryPlaceholder")}
                rows={3}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                {t("bookpitch.fieldTags")}
              </label>
              <input
                type="text"
                value={tags}
                onChange={(e) => setTags(e.target.value)}
                placeholder={t("bookpitch.tagsPlaceholder")}
                className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                {t("bookpitch.fieldDirection")}
              </label>
              <input
                type="text"
                value={direction}
                onChange={(e) => setDirection(e.target.value)}
                placeholder={t("bookpitch.adoptHint")}
                className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  {t("bookpitch.fieldGenre")}
                </label>
                <input
                  type="text"
                  value={genre}
                  onChange={(e) => setGenre(e.target.value)}
                  placeholder={t("bookpitch.genreHint")}
                  className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  {t("bookpitch.fieldPlatform")}
                </label>
                <input
                  type="text"
                  value={platform}
                  onChange={(e) => setPlatform(e.target.value)}
                  placeholder={t("bookpitch.platformHint")}
                  className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  {t("bookpitch.fieldStyle")}
                </label>
                <input
                  type="text"
                  value={style}
                  onChange={(e) => setStyle(e.target.value)}
                  placeholder={t("bookpitch.styleHint")}
                  className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
            </div>

            <div className="flex justify-end pt-1">
              <button
                type="button"
                onClick={handleAdopt}
                disabled={!canSave}
                className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {saving ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <Sparkles className="w-4 h-4" />
                )}
                {saving ? t("bookpitch.saving") : t("bookpitch.saveBtn")}
              </button>
            </div>
          </div>
        ) : (
          <div className="mt-5 rounded-lg border border-dashed bg-muted/20 p-6 max-w-3xl text-center">
            <p className="text-sm text-muted-foreground">
              {t("bookpitch.startHint")}
            </p>
          </div>
        )}
      </div>

      <div className="px-6 py-4 border-t shrink-0">
        {!hasModel && !!novelId && (
          <div className="flex items-center gap-2 mb-2 text-[12px] text-destructive">
            <AlertTriangle className="w-3.5 h-3.5" />
            {t("bookpitch.configureModelFirst")}
          </div>
        )}
        <button
          type="button"
          disabled={buttonDisabled}
          title={buttonTitle}
          onClick={handleStart}
          className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {launching ? (
            <Loader2 className="w-4 h-4 animate-spin" />
          ) : (
            <Play className="w-4 h-4" />
          )}
          {t("bookpitch.startButton")}
        </button>
      </div>
    </div>
  );
}
