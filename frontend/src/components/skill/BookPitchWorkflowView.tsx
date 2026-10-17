import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  Sparkles,
  Play,
  Check,
  AlertTriangle,
  Loader2,
  RefreshCw,
} from "lucide-react";
import {
  useEnabledSkills,
  useSetEnabledSkills,
} from "@/components/chat/useNovelSkills";
import { useSessions } from "@/components/chat/useSessions";
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
  CATEGORY_TAGS,
  CATEGORY_DIRECTION,
  CATEGORY_HOOK,
} from "@/components/preference/preferenceCategories";
import {
  CreatePreference,
  GetSessionMessages,
  SetEnabledSkills,
  UpdateNovel,
} from "@/lib/wailsjs/go/app/App";
import type { app } from "@/lib/wailsjs/go/models";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { novelKeys, preferenceKeys } from "@/lib/queryKeys";
import {
  extractPitchJSON,
  pitchSignature,
  type PitchData,
} from "./bookPitchParse";

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
  const qc = useQueryClient();
  const [launching, setLaunching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [autoSaving, setAutoSaving] = useState(false);
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
  const [oneLiner, setOneLiner] = useState("");

  const titleInputRef = useRef<HTMLInputElement>(null);
  const adoptSectionRef = useRef<HTMLDivElement>(null);

  // v2：字段级增量回填所需的统一映射与手动编辑保护。
  const FIELD_NAMES = [
    "title",
    "description",
    "tags",
    "direction",
    "genre",
    "platform",
    "style",
    "oneLiner",
  ] as const;
  type FieldName = (typeof FIELD_NAMES)[number];

  // 字段名 i18n key 映射：冲突提示时本地化展示字段名（避免源码硬编码中文）。
  const FIELD_I18N: Record<FieldName, string> = {
    title: "bookpitch.fTitle",
    description: "bookpitch.fDesc",
    tags: "bookpitch.fTags",
    direction: "bookpitch.fDirection",
    genre: "bookpitch.fGenre",
    platform: "bookpitch.fPlatform",
    style: "bookpitch.fStyle",
    oneLiner: "bookpitch.fOneLiner",
  };

  // 最新表单值快照，供 fillFromData 读取当前值做冲突判定（避免闭包陈旧）。
  const formRef = useRef<Record<FieldName, string>>({
    title,
    description,
    tags,
    direction,
    genre,
    platform,
    style,
    oneLiner,
  });
  useEffect(() => {
    formRef.current = {
      title,
      description,
      tags,
      direction,
      genre,
      platform,
      style,
      oneLiner,
    };
  }, [title, description, tags, direction, genre, platform, style, oneLiner]);

  // 用户手动编辑过的字段集合：受自动 JSON 覆盖保护。
  const manualFieldsRef = useRef<Set<FieldName>>(new Set());

  // 按字段名读写 state 的映射表。
  const setField = useCallback((f: FieldName, v: string) => {
    switch (f) {
      case "title":
        setTitle(v);
        break;
      case "description":
        setDescription(v);
        break;
      case "tags":
        setTags(v);
        break;
      case "direction":
        setDirection(v);
        break;
      case "genre":
        setGenre(v);
        break;
      case "platform":
        setPlatform(v);
        break;
      case "style":
        setStyle(v);
        break;
      case "oneLiner":
        setOneLiner(v);
        break;
    }
  }, []);

  // 标记某字段被用户手动编辑（不再被自动 JSON 覆盖）。
  const markManual = useCallback((f: FieldName) => {
    manualFieldsRef.current.add(f);
  }, []);

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
  const appliedPitchSig = useBookPitchStore((s) => s.appliedPitchSig);
  const setAppliedPitchSig = useBookPitchStore((s) => s.setAppliedPitchSig);
  const isIncubating = incubatorId !== null && incubatorId === novelId;

  // 孵化中：轮询本书最新会话的助手消息，检测 book-pitch 终稿 JSON 并自动回填表单。
  // enabled 仅在 incubatorId 有效时；refetchInterval 让对话定稿后数秒内自动抓取。
  const sessionsQuery = useSessions({
    novelId: incubatorId ?? 0,
    page: 1,
    size: 5,
    search: "",
  });
  const latestSid = sessionsQuery.data?.items?.[0]?.session_id ?? "";
  const msgQuery = useQuery({
    queryKey: ["bookpitch-session-messages", latestSid],
    queryFn: () => GetSessionMessages(latestSid),
    enabled: !!latestSid,
    refetchInterval: isIncubating ? 2500 : false,
  });

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

  // 把解析出的终稿数据回填到表单。
  // v2 字段级增量：force=true 时无视签名与手动保护，全量覆盖；
  // 否则只更新「用户未手动改过」的字段，并跳过空值（不清除已有内容）。
  // 若某字段用户已手动改过且 AI 有新建议值，记录为冲突并保留用户值，仅提示。
  const fillFromData = useCallback(
    (d: PitchData, force: boolean) => {
      const sig = pitchSignature(d);
      if (!force && sig === appliedPitchSig) return;
      if (force) manualFieldsRef.current.clear();

      const conflicts: FieldName[] = [];
      for (const f of FIELD_NAMES) {
        const incoming = d[f] ?? "";
        // 空值不覆盖：保留用户/历史已有内容。
        if (!incoming) continue;
        // 非强制且用户已手动改过：保留手动值，有新建议则记冲突。
        if (!force && manualFieldsRef.current.has(f)) {
          const current = formRef.current[f] ?? "";
          if (incoming !== current) conflicts.push(f);
          continue;
        }
        if (incoming !== (formRef.current[f] ?? "")) {
          setField(f, incoming);
        }
      }
      setAppliedPitchSig(sig);
      if (conflicts.length > 0) {
        toastInfo(
          t("bookpitch.conflictToast", {
            fields: conflicts.map((f) => t(FIELD_I18N[f])).join("、"),
          }),
        );
      } else {
        toastSuccess(t("bookpitch.parsedToast"));
      }
    },
    [appliedPitchSig, setField, t],
  );

  // 自动回填 + 自动保存书架：孵化中且拿到会话消息后，从最新一条助手消息
  // 往前找首个终稿 JSON；解析到新书签即回填表单，并立即把书名/简介/题材
  // 写入小说主体、把标签/方向/卖点/平台/风格写入本书偏好，让书架实时刷新。
  useEffect(() => {
    if (!isIncubating || !incubatorId) return;
    const msgs = msgQuery.data;
    if (!msgs || msgs.length === 0) return;
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role !== "assistant") continue;
      const data = extractPitchJSON(msgs[i].content || "");
      if (data) {
        const sig = pitchSignature(data);
        // 同一签名已处理过则跳过，避免重复回填与保存。
        if (sig === appliedPitchSig) return;
        fillFromData(data, false);
        // 自动滚动到「采纳方案并建书」区域，让用户立即看到可保存的表单。
        requestAnimationFrame(() => {
          adoptSectionRef.current?.scrollIntoView({
            behavior: "smooth",
            block: "start",
          });
        });
        // 构造合并数据：用户手动改过的字段保留其手动值，其余用 AI 最新 JSON；
        // 空值不覆盖（保留已有内容）。这样自动保存不会抹掉用户确认过的字段。
        const merged: PitchData = {
          title:
            manualFieldsRef.current.has("title") &&
            (formRef.current.title ?? "")
              ? formRef.current.title
              : data.title || formRef.current.title || "",
          description:
            manualFieldsRef.current.has("description") &&
            (formRef.current.description ?? "")
              ? formRef.current.description
              : data.description || formRef.current.description || "",
          tags:
            manualFieldsRef.current.has("tags") && (formRef.current.tags ?? "")
              ? formRef.current.tags
              : data.tags || formRef.current.tags || "",
          direction:
            manualFieldsRef.current.has("direction") &&
            (formRef.current.direction ?? "")
              ? formRef.current.direction
              : data.direction || formRef.current.direction || "",
          genre:
            manualFieldsRef.current.has("genre") &&
            (formRef.current.genre ?? "")
              ? formRef.current.genre
              : data.genre || formRef.current.genre || "",
          platform:
            manualFieldsRef.current.has("platform") &&
            (formRef.current.platform ?? "")
              ? formRef.current.platform
              : data.platform || formRef.current.platform || "",
          style:
            manualFieldsRef.current.has("style") &&
            (formRef.current.style ?? "")
              ? formRef.current.style
              : data.style || formRef.current.style || "",
          oneLiner:
            manualFieldsRef.current.has("oneLiner") &&
            (formRef.current.oneLiner ?? "")
              ? formRef.current.oneLiner
              : data.oneLiner || formRef.current.oneLiner || "",
        };
        // 异步自动保存到小说主体 + 偏好，不阻塞 UI；失败静默（用户仍可手动保存）。
        setAutoSaving(true);
        (async () => {
          try {
            await UpdateNovel(incubatorId, {
              title: merged.title.trim(),
              description: merged.description.trim(),
              genre: merged.genre.trim(),
              kind: "",
            });
            await persistPitchPreferences(incubatorId, merged);
            qc.invalidateQueries({ queryKey: novelKeys.all });
            qc.invalidateQueries({
              queryKey: preferenceKeys.list(incubatorId),
            });
            toastSuccess(t("bookpitch.autoSavedToast"));
          } catch {
            // 自动保存失败不 toast，避免打扰；用户可继续聊天或手动保存。
          } finally {
            setAutoSaving(false);
          }
        })();
        break;
      }
    }
  }, [
    msgQuery.data,
    isIncubating,
    incubatorId,
    appliedPitchSig,
    fillFromData,
    qc,
    t,
  ]);

  // 手动「采用 AI 建议」：无视签名与手动保护，以最新终稿 JSON 全量覆盖表单并保存书架。
  const handleAdoptAll = useCallback(() => {
    if (!incubatorId) return;
    const msgs = msgQuery.data;
    if (!msgs || msgs.length === 0) {
      toastError(t("bookpitch.noParse"));
      return;
    }
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role !== "assistant") continue;
      const data = extractPitchJSON(msgs[i].content || "");
      if (data) {
        fillFromData(data, true); // 强制全量覆盖（已清空 manual 保护）
        requestAnimationFrame(() => {
          adoptSectionRef.current?.scrollIntoView({
            behavior: "smooth",
            block: "start",
          });
        });
        // 全量覆盖后直接保存书架：manual 已清空，merged == data。
        setAutoSaving(true);
        (async () => {
          try {
            await UpdateNovel(incubatorId, {
              title: data.title.trim(),
              description: data.description.trim(),
              genre: data.genre.trim(),
              kind: "",
            });
            await persistPitchPreferences(incubatorId, data);
            qc.invalidateQueries({ queryKey: novelKeys.all });
            qc.invalidateQueries({
              queryKey: preferenceKeys.list(incubatorId),
            });
            toastSuccess(t("bookpitch.autoSavedToast"));
          } catch {
            // 静默：用户可再次点保存
          } finally {
            setAutoSaving(false);
          }
        })();
        return;
      }
    }
    toastError(t("bookpitch.noParse"));
  }, [msgQuery.data, fillFromData, incubatorId, qc, t]);

  // 把解析出的引导项持久化为本书偏好（自动保存 + 手动保存共用）。
  async function persistPitchPreferences(
    novelID: number,
    data: PitchData,
  ): Promise<void> {
    const extra: Array<{ category: string; content: string }> = [];
    if (data.platform.trim())
      extra.push({
        category: CATEGORY_PLATFORM,
        content: data.platform.trim(),
      });
    if (data.style.trim())
      extra.push({ category: CATEGORY_CREATIVE, content: data.style.trim() });
    if (data.tags.trim())
      extra.push({ category: CATEGORY_TAGS, content: data.tags.trim() });
    if (data.direction.trim())
      extra.push({
        category: CATEGORY_DIRECTION,
        content: data.direction.trim(),
      });
    if (data.oneLiner.trim())
      extra.push({ category: CATEGORY_HOOK, content: data.oneLiner.trim() });
    for (const e of extra) {
      try {
        await CreatePreference(novelID, {
          is_global: false,
          category: e.category,
          content: e.content,
        } as app.CreatePreferenceInput);
      } catch {
        // 偏好写入失败不阻塞主流程，静默忽略。
      }
    }
  }

  const canSave = title.trim().length > 0 && !saving && !autoSaving;

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

      // 引导项：平台 / 风格 / 标签 / 方向 / 一句话卖点 非空则写入本书偏好
      // （题材已写入 novel.genre 字段；书名/简介随小说主体更新）。
      await persistPitchPreferences(n.id, {
        title,
        description,
        tags,
        direction,
        genre,
        platform,
        style,
        oneLiner,
      });

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
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="text-sm font-semibold">
                  {t("bookpitch.adoptTitle")}
                </h2>
                <p className="text-[12px] text-muted-foreground mt-1">
                  {t("bookpitch.adoptHint")}
                </p>
                <p className="text-[12px] text-muted-foreground mt-1">
                  {t("bookpitch.parsedHint")}
                </p>
              </div>
              <button
                type="button"
                onClick={handleAdoptAll}
                disabled={!latestSid}
                className="shrink-0 inline-flex items-center gap-1.5 h-8 px-2.5 rounded-md text-xs border hover:bg-muted transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <RefreshCw className="w-3.5 h-3.5" />
                {t("bookpitch.adoptAllBtn")}
              </button>
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
                onChange={(e) => {
                  markManual("title");
                  setTitle(e.target.value);
                }}
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
                onChange={(e) => {
                  markManual("description");
                  setDescription(e.target.value);
                }}
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
                onChange={(e) => {
                  markManual("tags");
                  setTags(e.target.value);
                }}
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
                onChange={(e) => {
                  markManual("direction");
                  setDirection(e.target.value);
                }}
                placeholder={t("bookpitch.adoptHint")}
                className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                {t("bookpitch.fieldOneLiner")}
              </label>
              <textarea
                value={oneLiner}
                onChange={(e) => {
                  markManual("oneLiner");
                  setOneLiner(e.target.value);
                }}
                placeholder={t("bookpitch.oneLinerPlaceholder")}
                rows={2}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
                  onChange={(e) => {
                    markManual("genre");
                    setGenre(e.target.value);
                  }}
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
                  onChange={(e) => {
                    markManual("platform");
                    setPlatform(e.target.value);
                  }}
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
                  onChange={(e) => {
                    markManual("style");
                    setStyle(e.target.value);
                  }}
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
