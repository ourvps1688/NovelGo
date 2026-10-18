import { useEffect, useState } from "react";
import { PenLine, Plus, Sparkles, Target, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { CreatePreference, SetEnabledSkills } from "@/lib/wailsjs/go/app/App";
import type { app } from "@/lib/wailsjs/go/models";
import { toErrorMessage } from "@/utils/error";
import { toastError, toastSuccess } from "@/utils/toast";
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { useCreateNovel } from "./useCreateNovel";
import {
  POLAR_STAR_CATEGORY,
  serializePolar,
  type PolarData,
} from "@/components/preference/polarStarData";

// 等待 React 完成「切换小说 → ChatPanel 用新 novelId 重新 register(send)」的渲染提交，
// 避免把 kickoff 误发到旧书。双 rAF 覆盖一次完整 commit 周期。
function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()));
}
async function flushToNextRender(): Promise<void> {
  await nextFrame();
  await nextFrame();
}

type StrategyKey = "signing" | "free" | "experimental";

interface StrategyDef {
  key: StrategyKey;
  titleKey: string;
  descKey: string;
  icon: typeof Target;
  skills: string[];
  genreDefaultKey: string; // "" = 不预填
  requireNorthStar: boolean;
}

// 创作策略三分：与题材卡（怎么写）正交，管「这本要往哪个方向发力」。
// 每档 = 适配技能启用预设 + 题材默认 + 是否强制北极星。
const STRATEGIES: StrategyDef[] = [
  {
    key: "signing",
    titleKey: "wizard.strategy.signing.title",
    descKey: "wizard.strategy.signing.desc",
    icon: Target,
    skills: ["story-blueprint", "worldbuilding-design", "signability-audit"],
    genreDefaultKey: "wizard.genreDefault.signing",
    requireNorthStar: true,
  },
  {
    key: "free",
    titleKey: "wizard.strategy.free.title",
    descKey: "wizard.strategy.free.desc",
    icon: PenLine,
    skills: ["next"],
    genreDefaultKey: "",
    requireNorthStar: false,
  },
  {
    key: "experimental",
    titleKey: "wizard.strategy.experimental.title",
    descKey: "wizard.strategy.experimental.desc",
    icon: Sparkles,
    skills: ["brainstorm-composer", "plot-hook"],
    genreDefaultKey: "wizard.genreDefault.experimental",
    requireNorthStar: false,
  },
];

interface Props {
  open: boolean;
  onClose: () => void;
  onCreated: (novelId: number) => void | Promise<void>;
}

// NewBookWizard: 开书向导（创作策略三分）。
// 选中策略 → 套用技能启用预设 + 题材默认 → 收尾可填北极星/底线（签约冲刺强制建议）。
// 创建后自动启用对应技能并把北极星/底线写入本书偏好。
export default function NewBookWizard({ open, onClose, onCreated }: Props) {
  const { t } = useTranslation();
  const createNovel = useCreateNovel();

  const [step, setStep] = useState<1 | 2>(1);
  const [strategyKey, setStrategyKey] = useState<StrategyKey>("signing");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [genre, setGenre] = useState("");
  const [northStar, setNorthStar] = useState("");
  const [bottomLines, setBottomLines] = useState<string[]>([""]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  // 签约冲刺档建书成功后，询问是否一键启动签约潜力工作流（含黄金三章）。
  const [launchPromptId, setLaunchPromptId] = useState<number | null>(null);

  const strategy =
    STRATEGIES.find((s) => s.key === strategyKey) ?? STRATEGIES[0];

  useEffect(() => {
    if (open) {
      setStep(1);
      setStrategyKey("signing");
      setTitle("");
      setDescription("");
      setGenre(t("wizard.genreDefault.signing"));
      setNorthStar("");
      setBottomLines([""]);
      setSaving(false);
      setError("");
      setLaunchPromptId(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  if (!open) return null;

  function selectStrategy(key: StrategyKey) {
    setStrategyKey(key);
    const def = STRATEGIES.find((s) => s.key === key)!;
    // 切换策略时把题材预填为该策略默认值（step1 时用户尚未输入题材，无覆盖风险）。
    setGenre(def.genreDefaultKey ? t(def.genreDefaultKey) : "");
  }

  function updateLine(i: number, v: string) {
    setBottomLines((prev) => prev.map((l, idx) => (idx === i ? v : l)));
  }
  function addLine() {
    setBottomLines((prev) => [...prev, ""]);
  }
  function removeLine(i: number) {
    setBottomLines((prev) => prev.filter((_, idx) => idx !== i));
  }

  const canSave = title.trim().length > 0 && !saving;

  async function handleCreate() {
    if (!canSave) return;
    setSaving(true);
    setError("");
    try {
      const n = await createNovel.mutateAsync({
        title: title.trim(),
        description: description.trim(),
        genre: genre.trim(),
      });

      // 套用策略：启用适配技能集（新书无既有技能，直接 set）。
      try {
        await SetEnabledSkills({
          novel_id: n.id,
          names: strategy.skills,
        });
        toastSuccess(
          t("wizard.appliedToast", {
            strategy: t(strategy.titleKey),
            count: strategy.skills.length,
          }),
        );
      } catch {
        // 技能启用失败不阻塞建书，静默忽略。
      }

      // 北极星/底线：非空则写入本书偏好。
      const data: PolarData = {
        north_star: northStar,
        bottom_lines: bottomLines.map((l) => l.trim()).filter(Boolean),
      };
      if (data.north_star || data.bottom_lines.length > 0) {
        try {
          await CreatePreference(n.id, {
            is_global: false,
            category: POLAR_STAR_CATEGORY,
            content: serializePolar(data),
          } as app.CreatePreferenceInput);
        } catch {
          // 北极星写入失败不阻塞建书，静默忽略。
        }
      }

      toastSuccess(t("wizard.success"));
      // 签约冲刺档：建书后询问是否一键启动签约潜力工作流（含黄金三章）。
      // 其它策略直接进章节视图，不弹提示。
      if (strategy.key === "signing") {
        setLaunchPromptId(n.id);
        setSaving(false);
        return;
      }
      await onCreated(n.id);
    } catch (e) {
      setError(toErrorMessage(e, t("novel.saveFailedRetry")));
    } finally {
      setSaving(false);
    }
  }

  // 「立即启动」：先切到新书（bind chat），等 ChatPanel 用新 novelId 重注册 send，
  // 再发签约工作流 kickoff。切书在 onCreated 内完成（含 setActivePanel）。
  async function handleLaunchConfirm() {
    const id = launchPromptId;
    if (id == null || saving) return;
    setSaving(true);
    try {
      await onCreated(id);
      await flushToNextRender();
      const send = useChatLaunchStore.getState().send;
      if (send) {
        // 再确保本工作流技能已启用（建书时已随策略启用，此处幂等兜底）。
        const base = new Set(strategy.skills);
        base.add("signability-workflow");
        try {
          await SetEnabledSkills({ novel_id: id, names: [...base] });
        } catch {
          // 兜底启用失败不阻塞 kickoff。
        }
        send(t("signability.kickoff"));
        toastSuccess(
          t("wizard.launchedToast", { workflow: t("workflow.signability") }),
        );
      } else {
        toastError(t("wizard.launchChatNotReady"));
      }
    } catch (e) {
      setError(toErrorMessage(e, t("wizard.launchFailed")));
    } finally {
      setSaving(false);
      onClose();
    }
  }

  function handleLaunchLater() {
    const id = launchPromptId;
    if (id == null) return;
    void onCreated(id);
    onClose();
  }

  const cardBtn = (active: boolean) =>
    `flex flex-col items-start gap-2 rounded-lg border p-3 text-left transition-colors ${
      active
        ? "border-primary bg-primary/5 ring-1 ring-primary"
        : "border-border hover:border-primary/50 hover:bg-muted/40"
    }`;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative bg-background rounded-xl shadow-2xl border w-[460px] max-w-[92vw] max-h-[90vh] overflow-y-auto p-6">
        <button
          onClick={onClose}
          className="absolute top-3 right-3 w-7 h-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
        >
          ✕
        </button>

        <h2 className="text-base font-semibold">{t("wizard.title")}</h2>
        <p className="text-xs text-muted-foreground mt-1">
          {t("wizard.subtitle")}
        </p>

        {launchPromptId !== null && (
          <div className="mt-5 rounded-lg border border-border bg-card/50 p-4 space-y-3">
            <p className="text-sm font-medium text-foreground leading-relaxed">
              {t("wizard.launchPrompt")}
            </p>
            <p className="text-xs text-muted-foreground leading-relaxed">
              {t("wizard.launchHint")}
            </p>
            <div className="flex justify-end gap-2 pt-1">
              <button
                type="button"
                onClick={handleLaunchLater}
                disabled={saving}
                className="h-9 px-4 rounded-md text-sm border hover:bg-muted transition-colors"
              >
                {t("wizard.launchLater")}
              </button>
              <button
                type="button"
                onClick={handleLaunchConfirm}
                disabled={saving}
                className="h-9 px-4 rounded-md text-sm bg-primary text-primary-foreground hover:opacity-90 transition-opacity disabled:opacity-50"
              >
                {saving ? t("wizard.creating") : t("wizard.launchNow")}
              </button>
            </div>
          </div>
        )}

        {launchPromptId === null && error && (
          <p className="text-sm text-destructive bg-danger-bg border border-danger-border rounded-md px-3 py-2 mt-4">
            {error}
          </p>
        )}

        {launchPromptId === null &&
          (step === 1 ? (
            <div className="mt-5 space-y-2">
              <p className="text-xs font-medium text-muted-foreground">
                {t("wizard.stepStrategy")}
              </p>
              {STRATEGIES.map((s) => {
                const Icon = s.icon;
                return (
                  <button
                    key={s.key}
                    type="button"
                    onClick={() => selectStrategy(s.key)}
                    className={cardBtn(strategyKey === s.key)}
                  >
                    <span className="flex items-center gap-2">
                      <Icon className="w-4 h-4 text-primary" />
                      <span className="text-sm font-semibold text-foreground">
                        {t(s.titleKey)}
                      </span>
                    </span>
                    <span className="text-xs text-muted-foreground leading-relaxed">
                      {t(s.descKey)}
                    </span>
                  </button>
                );
              })}
              <div className="flex justify-end pt-3">
                <button
                  type="button"
                  onClick={() => setStep(2)}
                  className="h-9 px-4 rounded-md text-sm bg-primary text-primary-foreground hover:opacity-90 transition-opacity"
                >
                  {t("wizard.next")}
                </button>
              </div>
            </div>
          ) : (
            <div className="mt-5 space-y-4">
              <p className="text-xs font-medium text-muted-foreground">
                {t("wizard.stepInfo")}
                <span className="ml-1 text-primary">
                  · {t(strategy.titleKey)}
                </span>
              </p>

              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  {t("novel.bookTitle")}{" "}
                  <span className="text-destructive">*</span>
                </label>
                <input
                  type="text"
                  value={title}
                  autoFocus
                  onChange={(e) => setTitle(e.target.value)}
                  placeholder={t("novel.enterBookTitle")}
                  className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>

              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  {t("novel.genre")}
                </label>
                <input
                  type="text"
                  value={genre}
                  onChange={(e) => setGenre(e.target.value)}
                  placeholder={t("novel.genreExample")}
                  className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
                {strategy.genreDefaultKey && (
                  <p className="text-[10px] text-muted-foreground mt-1">
                    {t("wizard.genreDefaultHint")}
                  </p>
                )}
              </div>

              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  {t("novel.summary")}
                </label>
                <textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder={t("novel.summaryPlaceholder")}
                  rows={3}
                  className="w-full rounded-md border bg-background px-3 py-2 text-sm resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>

              <div className="rounded-lg border border-border bg-card/50 p-3">
                <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                  <Target className="h-3.5 w-3.5 text-primary" />
                  {t("polarStar.title")}
                  <span
                    className={`ml-auto text-[10px] ${
                      strategy.requireNorthStar
                        ? "text-warning-foreground"
                        : "text-muted-foreground"
                    }`}
                  >
                    {strategy.requireNorthStar
                      ? t("wizard.northStarRequired")
                      : t("wizard.northStarOptional")}
                  </span>
                </div>

                <div className="mt-3 space-y-3">
                  <div>
                    <label className="text-[11px] font-medium text-foreground">
                      {t("polarStar.northStarLabel")}
                    </label>
                    <textarea
                      value={northStar}
                      onChange={(e) => setNorthStar(e.target.value)}
                      placeholder={t("polarStar.northStarPlaceholder")}
                      rows={2}
                      className="mt-1.5 w-full resize-y rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </div>
                  <div>
                    <label className="text-[11px] font-medium text-foreground">
                      {t("polarStar.bottomLinesLabel")}
                    </label>
                    <div className="mt-1.5 space-y-1.5">
                      {bottomLines.map((line, i) => (
                        <div key={i} className="flex items-center gap-1.5">
                          <span className="shrink-0 text-muted-foreground text-xs">
                            {i + 1}.
                          </span>
                          <input
                            value={line}
                            onChange={(e) => updateLine(i, e.target.value)}
                            placeholder={t("polarStar.bottomLinePlaceholder")}
                            className="flex-1 h-8 rounded-md border border-border bg-background px-2.5 text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-1 focus:ring-ring"
                          />
                          <button
                            type="button"
                            onClick={() => removeLine(i)}
                            disabled={bottomLines.length <= 1}
                            className="shrink-0 flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:opacity-40 transition-colors"
                            title={t("polarStar.removeLine")}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      ))}
                      <button
                        type="button"
                        onClick={addLine}
                        className="inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
                      >
                        <Plus className="h-3 w-3" />
                        {t("polarStar.addLine")}
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              <div className="flex justify-between gap-2 pt-1">
                <button
                  type="button"
                  onClick={() => setStep(1)}
                  disabled={saving}
                  className="h-9 px-4 rounded-md text-sm border hover:bg-muted transition-colors"
                >
                  {t("wizard.back")}
                </button>
                <button
                  type="button"
                  onClick={handleCreate}
                  disabled={!canSave}
                  className="h-9 px-4 rounded-md text-sm bg-primary text-primary-foreground hover:opacity-90 transition-opacity disabled:opacity-50"
                >
                  {saving ? t("wizard.creating") : t("wizard.startCreating")}
                </button>
              </div>
            </div>
          ))}
      </div>
    </div>
  );
}
