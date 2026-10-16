import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Sparkles, Play, Check, AlertTriangle, Loader2 } from "lucide-react";
import {
  useEnabledSkills,
  useSetEnabledSkills,
} from "@/components/chat/useNovelSkills";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { useCreateNovel } from "@/components/novel/useCreateNovel";
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

  const enabledQuery = useEnabledSkills(novelId);
  const setEnabled = useSetEnabledSkills(novelId);
  const modelsQuery = useModels();
  const settingsQuery = useSettings();
  const createNovel = useCreateNovel();

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
    if (!novelId) {
      toastError(t("bookpitch.selectNovelFirst"));
      return;
    }
    if (!hasModel) {
      toastError(t("bookpitch.configureModelFirst"));
      return;
    }
    const idea = ideaDraft.trim();
    if (!idea) {
      toastError(t("bookpitch.ideaRequired"));
      return;
    }
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
      const kickoff = t("bookpitch.kickoff", { idea });
      send(kickoff);
    } catch (err) {
      toastError(toErrorMessage(err, t("bookpitch.enableFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, ideaDraft, enabledQuery.data, setEnabled, t]);

  const canSave = title.trim().length > 0 && !saving;

  async function handleAdopt() {
    if (!canSave) return;
    setSaving(true);
    setError("");
    try {
      const n = await createNovel.mutateAsync({
        title: title.trim(),
        description: description.trim(),
        genre: genre.trim(),
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

      toastSuccess(t("bookpitch.createdToast"));
      await onCreated(n.id);
    } catch (e) {
      setError(toErrorMessage(e, t("novel.saveFailedRetry")));
    } finally {
      setSaving(false);
    }
  }

  const noNovel = !novelId;
  const buttonDisabled = launching || noNovel || !hasModel;
  const buttonTitle = noNovel
    ? t("bookpitch.selectNovelFirst")
    : !hasModel
      ? t("bookpitch.configureModelFirst")
      : "";

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

        {/* 采纳方案并建书：书名必填，题材/平台/风格为可选引导项。 */}
        <div className="mt-5 rounded-lg border bg-muted/30 p-4 max-w-3xl space-y-3">
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
              {saving ? t("bookpitch.creating") : t("bookpitch.createBtn")}
            </button>
          </div>
        </div>
      </div>

      <div className="px-6 py-4 border-t shrink-0">
        {!hasModel && !noNovel && (
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
