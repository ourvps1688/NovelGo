import { useEffect, useRef, useState } from "react";
import { Loader2, Save, ShieldCheck, Sparkle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toErrorMessage } from "@/utils/error";
import {
  ExtractPattern,
  ExtractPatternAll,
  CancelExtractPattern,
  GetContent,
} from "@/lib/wailsjs/go/app/App";
import { pattern } from "@/lib/wailsjs/go/models";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { useSaveContent } from "@/components/content/useSaveContent";
import { isContentConflict } from "@/components/content/editorSaveQueue";
import { reportAIFileChange } from "@/components/content/aiFileChanges";
import { toastSuccess } from "@/utils/toast";
import { usePatternProgress } from "@/hooks/usePatternProgress";
import Markdown from "@/components/Markdown";
import { splitFrontmatter } from "@/components/content/types";
import PatternProgressView from "./PatternProgressView";
import ConfirmDialog from "@/components/ui/ConfirmDialog";

interface Props {
  taskId: string;
  novelId: number;
  chapterIds: number[]; // 空数组表示全书
  providerName: string; // 结构化模型入参（替代拼接 modelKey + splitModelKey）
  modelId: string;
  reasoningEffort: string; // reasoning effort（model 不支持时为空串）
  title: string; // 小说标题（进度页展示用）
  chapterCount: number; // 本次提取的章节数
  flavor?: string; // "struct" (default) | "mental"，单产物提取时使用
  flavors?: string[]; // 多产物提取（struct/mental），>1 时走 ExtractPatternAll 一次前缀多产物
  sourceAuthor?: string; // 仅 mental：来源作者，写入 skill frontmatter
  deepMultiLens?: boolean; // 仅 mental：深度多镜头精提（V3 P1，opt-in）
  onExit: () => void; // 返回选择页
}

type Status = "running" | "done" | "failed";

// 单个产物的提取结果（与 wails 返回的 ExtractPatternResult 对齐，snake_case）。
interface OneResult {
  taskId: string;
  novelId: number;
  name: string;
  description: string;
  filePath: string;
  rawContent: string;
  flavor?: string;
  fidelity?: pattern.FidelityScorecard;
  demotedModels?: string[];
}

// ResultCard 渲染单个产物的预览与保存（套路 / 心智复用同一卡片）。
function ResultCard({
  result,
  onExit,
  onSaved,
}: {
  result: OneResult;
  onExit: () => void;
  onSaved?: () => void;
}) {
  const { t } = useTranslation();
  const [error, setError] = useState("");
  const [overwriteContent, setOverwriteContent] = useState<string | null>(null);
  const [checkingSave, setCheckingSave] = useState(false);
  const saveMutation = useSaveContent();

  const saveGenerated = async (expectedContent: string) => {
    if (!result) return;
    setError("");
    try {
      await saveMutation.mutateAsync({
        novel_id: result.novelId,
        path: result.filePath,
        content: result.rawContent,
        expected_content: expectedContent,
      });
      reportAIFileChange({ novelId: result.novelId, path: result.filePath });
      setOverwriteContent(null);
      // mental 提取：勾选常驻 + 设为本书写作人格已由后端 SaveContent 统一收口
      // （覆盖所有心智落盘路径，含手动创建/编辑），此处仅保留用户侧反馈。
      const { meta: savedMeta } = splitFrontmatter(result.rawContent);
      if (savedMeta.kind === "mental") {
        toastSuccess(t("extract.mindsetBoundPersona", { name: result.name }));
      }
      onSaved?.();
      onExit();
    } catch (e: unknown) {
      setOverwriteContent(null);
      setError(
        isContentConflict(e)
          ? t("skill.generatedTargetChanged")
          : toErrorMessage(e, t("extract.saveFailed")),
      );
    }
  };

  const handleSave = async () => {
    if (!result) return;
    setError("");
    setCheckingSave(true);
    try {
      const existing = await GetContent(result.novelId, result.filePath);
      if (existing) setOverwriteContent(existing);
      else await saveGenerated(existing);
    } catch (e: unknown) {
      setError(toErrorMessage(e, t("extract.saveFailed")));
    } finally {
      setCheckingSave(false);
    }
  };

  const { body } = splitFrontmatter(result.rawContent);
  const isMental = result.flavor === "mental";

  return (
    <div className="rounded-lg border bg-card/50 p-4 space-y-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-semibold text-foreground flex items-center gap-2">
          <Sparkle className="w-4 h-4 text-primary" />
          {isMental
            ? t("extract.mindsetResultTitle")
            : t("extract.structResultTitle")}
          ：「{result.name}」
        </span>
        <div className="flex items-center gap-2">
          <button
            onClick={onExit}
            disabled={checkingSave || saveMutation.isPending}
            className="h-8 px-3 rounded-lg text-sm border border-border hover:bg-muted transition-colors"
          >
            {t("extract.cancel")}
          </button>
          <button
            onClick={handleSave}
            disabled={checkingSave || saveMutation.isPending}
            className="inline-flex items-center gap-1.5 h-8 px-4 rounded-lg text-sm font-medium bg-action-save text-action-save-foreground hover:bg-action-save/80 disabled:opacity-50 transition-colors"
          >
            <Save className="w-3.5 h-3.5" />
            {checkingSave || saveMutation.isPending
              ? t("extract.saving")
              : t("extract.saveToUserSkill")}
          </button>
        </div>
      </div>

      {error && (
        <div className="px-3 py-2 text-xs text-destructive bg-danger-bg border border-danger-border rounded-md">
          {error}
        </div>
      )}

      {isMental && result.fidelity && (
        <div className="rounded-lg border bg-muted/10 p-4 space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm font-semibold text-foreground flex items-center gap-2">
              <ShieldCheck className="w-4 h-4 text-action-save" />
              {t("extract.fidelity.title")}
            </span>
            <span
              className={`text-xs font-medium px-2 py-0.5 rounded-full ${
                result.fidelity.passed
                  ? "bg-success/15 text-success-foreground"
                  : "bg-warning/15 text-warning-foreground"
              }`}
            >
              {result.fidelity.passed
                ? t("extract.fidelity.passed")
                : t("extract.fidelity.failed")}
            </span>
          </div>
          <div className="grid grid-cols-2 gap-2 text-xs">
            <div className="flex items-center justify-between rounded-md border bg-background px-3 py-2">
              <span className="text-muted-foreground">
                {t("extract.fidelity.knownConsistency")}
              </span>
              <span className="font-semibold text-foreground">
                {result.fidelity.known_consistency}/5
              </span>
            </div>
            <div className="flex items-center justify-between rounded-md border bg-background px-3 py-2">
              <span className="text-muted-foreground">
                {t("extract.fidelity.unknownUncertainty")}
              </span>
              <span className="font-semibold text-foreground">
                {result.fidelity.unknown_uncertainty}/5
              </span>
            </div>
          </div>
          {result.fidelity.issues && result.fidelity.issues.length > 0 && (
            <div>
              <p className="text-xs text-muted-foreground mb-1">
                {t("extract.fidelity.issues")}
              </p>
              <ul className="list-disc pl-5 space-y-0.5 text-xs text-foreground">
                {result.fidelity.issues.map((iss, i) => (
                  <li key={i}>{iss}</li>
                ))}
              </ul>
            </div>
          )}
          {result.demotedModels && result.demotedModels.length > 0 && (
            <div className="text-xs text-warning-foreground">
              {t("extract.fidelity.demoted")}
              {result.demotedModels.join("、")}
            </div>
          )}
          <p className="text-[11px] text-muted-foreground">
            {t("extract.fidelity.note")}
          </p>
        </div>
      )}

      <div className="rounded-lg border bg-muted/10 p-4">
        <Markdown content={body} />
      </div>

      <ConfirmDialog
        open={overwriteContent !== null}
        title={t("skill.generatedOverwriteTitle")}
        message={t("skill.generatedOverwriteMessage", { name: result.name })}
        confirmText={t("skill.generatedConfirmOverwrite")}
        danger
        loading={saveMutation.isPending}
        onClose={() => {
          if (!saveMutation.isPending) setOverwriteContent(null);
        }}
        onConfirm={() => {
          if (overwriteContent !== null) void saveGenerated(overwriteContent);
        }}
      />
    </div>
  );
}

export default function PatternSessionView({
  taskId,
  novelId,
  chapterIds,
  providerName,
  modelId,
  reasoningEffort,
  title,
  chapterCount,
  flavor,
  flavors,
  sourceAuthor,
  deepMultiLens,
  onExit,
}: Props) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<Status>("running");
  const [allResult, setAllResult] =
    useState<pattern.ExtractPatternAllResult | null>(null);
  const [error, setError] = useState("");
  const { progress, events, reset } = usePatternProgress(taskId);
  const startedRef = useRef(false);

  // 多产物（>1 flavor）走 ExtractPatternAll；否则退化为单产物 ExtractPattern。
  const activeFlavors =
    flavors && flavors.length > 0 ? flavors : [flavor || "struct"];
  const multi = activeFlavors.length > 1;
  const hasMental = activeFlavors.includes("mental");

  const runExtract = async () => {
    setStatus("running");
    setError("");
    setAllResult(null);
    reset();

    if (!providerName || !modelId) {
      setError(t("extract.extractFailed"));
      setStatus("failed");
      return;
    }

    try {
      // 提取动作埋点（P4 T2.2）：失败静默零提示，不影响提取链路。
      trackFeature(ACTION_FEATURE_KEYS.extract);
      let res: pattern.ExtractPatternAllResult;
      if (multi) {
        res = await ExtractPatternAll({
          task_id: taskId,
          novel_id: novelId,
          provider_name: providerName,
          model_id: modelId,
          reasoning_effort: reasoningEffort,
          chapter_ids: chapterIds.length > 0 ? chapterIds : undefined,
          flavors: activeFlavors,
          source_author: sourceAuthor || undefined,
          deep_multi_lens:
            deepMultiLens && hasMental ? deepMultiLens : undefined,
        });
      } else {
        const single = await ExtractPattern({
          task_id: taskId,
          novel_id: novelId,
          provider_name: providerName,
          model_id: modelId,
          reasoning_effort: reasoningEffort,
          chapter_ids: chapterIds.length > 0 ? chapterIds : undefined,
          flavor: activeFlavors[0],
          source_author: sourceAuthor || undefined,
          deep_multi_lens:
            deepMultiLens && activeFlavors[0] === "mental"
              ? deepMultiLens
              : undefined,
        });
        res = pattern.ExtractPatternAllResult.createFrom({
          task_id: single.task_id,
          struct: activeFlavors[0] === "mental" ? undefined : single,
          mental: activeFlavors[0] === "mental" ? single : undefined,
        });
      }
      setAllResult(res);
      setStatus("done");
    } catch (e: unknown) {
      const msg = toErrorMessage(e, "");
      if (!msg.includes("canceled") && !msg.includes("取消")) {
        setError(msg || t("extract.extractFailed"));
        setStatus("failed");
      }
    }
  };

  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;
    runExtract();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleCancel = async () => {
    try {
      await CancelExtractPattern(novelId);
    } catch {
      // ignore cancel errors
    }
    onExit();
  };

  // 将 wails 返回的 ExtractPatternResult 映射为前端展示用的 OneResult。
  const toOne = (r: pattern.ExtractPatternResult): OneResult => ({
    taskId: r.task_id ?? "",
    novelId,
    name: r.name,
    description: r.description,
    filePath: r.file_path,
    rawContent: r.raw_content,
    flavor: r.flavor ?? undefined,
    fidelity: r.fidelity,
    demotedModels: r.demoted_models,
  });

  // 收集要展示的产物（按 套路 → 心智 顺序）。
  const cards: OneResult[] = [];
  if (allResult?.struct) cards.push(toOne(allResult.struct));
  if (allResult?.mental) cards.push(toOne(allResult.mental));

  return (
    <div className="flex-1 flex flex-col min-h-0 bg-background">
      <div className="flex items-center justify-between px-6 py-4 border-b shrink-0">
        {status === "running" && (
          <>
            <span className="text-sm text-muted-foreground flex items-center gap-2">
              <Sparkle className="w-4 h-4 text-primary" />
              {multi
                ? t("extract.progress.unifiedTitle")
                : hasMental
                  ? t("extract.progress.mindsetTitle")
                  : t("extract.progress.title")}
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={handleCancel}
                className="inline-flex items-center gap-1.5 h-8 px-4 rounded-lg text-sm font-medium bg-destructive text-destructive-foreground hover:bg-destructive/80 transition-colors shadow-sm"
              >
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
                {t("extract.cancel")}
              </button>
            </div>
          </>
        )}

        {status === "done" && (
          <>
            <span className="text-sm text-muted-foreground flex items-center gap-2">
              <Sparkle className="w-4 h-4 text-primary" />
              {multi
                ? t("extract.unifiedGenerated", { count: cards.length })
                : t("extract.generated")}
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => {
                  setStatus("running");
                  runExtract();
                }}
                className="h-8 px-3 rounded-lg text-sm border border-border hover:bg-muted transition-colors"
              >
                {t("extract.reExtract")}
              </button>
            </div>
          </>
        )}

        {status === "failed" && (
          <>
            <span className="text-sm text-muted-foreground flex items-center gap-2">
              <Sparkle className="w-4 h-4 text-primary" />
              {t("extract.extractFailed")}
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => {
                  setStatus("running");
                  runExtract();
                }}
                className="h-8 px-3 rounded-lg text-sm border border-border hover:bg-muted transition-colors"
              >
                {t("extract.reExtract")}
              </button>
              <button
                onClick={onExit}
                className="h-8 px-3 rounded-lg text-sm border border-border hover:bg-muted transition-colors"
              >
                {t("extract.cancel")}
              </button>
            </div>
          </>
        )}
      </div>

      {error && (
        <div className="mx-6 mt-3 px-3 py-2 text-xs text-destructive bg-danger-bg border border-danger-border rounded-md shrink-0">
          {error}
        </div>
      )}

      {status === "done" && allResult ? (
        <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4 space-y-3">
          {cards.map((r) => (
            <ResultCard
              key={r.flavor || r.filePath}
              result={r}
              onExit={onExit}
            />
          ))}
        </div>
      ) : (
        <div className="flex-1 min-h-0 overflow-y-auto p-6">
          <PatternProgressView
            progress={progress}
            events={events}
            novelTitle={title}
            chapterCount={chapterCount}
            flavor={hasMental ? "mental" : "struct"}
            deepMultiLens={deepMultiLens}
          />
        </div>
      )}
    </div>
  );
}
