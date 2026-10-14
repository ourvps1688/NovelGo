import { useEffect, useRef, useState } from "react";
import { Loader2, Save, ShieldCheck, Sparkle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toErrorMessage } from "@/utils/error";
import {
  ExtractPattern,
  CancelExtractPattern,
  GetContent,
} from "@/lib/wailsjs/go/app/App";
import type { pattern } from "@/lib/wailsjs/go/models";
import { useSaveContent } from "@/components/content/useSaveContent";
import { isContentConflict } from "@/components/content/editorSaveQueue";
import { reportAIFileChange } from "@/components/content/aiFileChanges";
import { usePatternProgress } from "@/hooks/usePatternProgress";
import Markdown from "@/components/Markdown";
import { splitFrontmatter } from "@/components/content/types";
import PatternProgressView from "./PatternProgressView";
import ConfirmDialog from "@/components/ui/ConfirmDialog";
import { GetEnabledSkills, SetEnabledSkills } from "@/lib/wailsjs/go/app/App";

interface Props {
  taskId: string;
  novelId: number;
  chapterIds: number[]; // 空数组表示全书
  providerName: string; // 结构化模型入参（替代拼接 modelKey + splitModelKey）
  modelId: string;
  reasoningEffort: string; // reasoning effort（model 不支持时为空串）
  title: string; // 小说标题（进度页展示用）
  chapterCount: number; // 本次提取的章节数
  flavor?: string; // "struct" (default) | "mental"
  sourceAuthor?: string; // 仅 mental：来源作者，写入 skill frontmatter
  deepMultiLens?: boolean; // 仅 mental：深度多镜头精提（V3 P1，opt-in）
  onExit: () => void; // 返回选择页
}

type Status = "running" | "done" | "failed";

interface ExtractResult {
  taskId: string;
  novelId: number;
  name: string;
  description: string;
  filePath: string;
  rawContent: string;
  fidelity?: pattern.FidelityScorecard; // V2 产出后保真度自测（仅 mental）
  demotedModels?: string[]; // V2 确定性门禁降级的心智模型（仅 mental）
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
  sourceAuthor,
  deepMultiLens,
  onExit,
}: Props) {
  const { t } = useTranslation();
  const [status, setStatus] = useState<Status>("running");
  const [result, setResult] = useState<ExtractResult | null>(null);
  const [error, setError] = useState("");
  const [overwriteContent, setOverwriteContent] = useState<string | null>(null);
  const [checkingSave, setCheckingSave] = useState(false);
  // 5.3 pattern commit 2: SaveContent 走 useSaveContent mutation（复用 5.2 content 领域），
  //   loading 由 mutation.isPending 推导，删手动 setLoading state。
  //   流式 ExtractPattern/CancelExtractPattern 改直接 import wailsjs（删 useApp），保持本地 state + try/catch。
  const saveMutation = useSaveContent();
  const { progress, events, reset } = usePatternProgress(taskId);
  const startedRef = useRef(false);

  const runExtract = async () => {
    setStatus("running");
    setError("");
    setResult(null);
    reset();

    // 结构化入参校验（替代 splitModelKey 拆解 + 校验）
    if (!providerName || !modelId) {
      setError(t("extract.extractFailed"));
      setStatus("failed");
      return;
    }

    try {
      const res = await ExtractPattern({
        task_id: taskId,
        novel_id: novelId,
        provider_name: providerName,
        model_id: modelId,
        reasoning_effort: reasoningEffort,
        chapter_ids: chapterIds.length > 0 ? chapterIds : undefined,
        flavor: flavor || undefined,
        source_author: sourceAuthor || undefined,
        deep_multi_lens:
          deepMultiLens && flavor === "mental" ? deepMultiLens : undefined,
      });
      setResult({
        taskId: res.task_id || taskId,
        novelId,
        name: res.name,
        description: res.description,
        filePath: res.file_path,
        rawContent: res.raw_content,
        fidelity: res.fidelity,
        demotedModels: res.demoted_models,
      });
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
      // mental 提取：保存成功后自动并入写作流（合并写入本书 .enabled.json，不全量覆盖）。
      const { meta: savedMeta } = splitFrontmatter(result.rawContent);
      if (savedMeta.kind === "mental") {
        try {
          const current = (await GetEnabledSkills(result.novelId)) ?? [];
          if (!current.includes(result.name)) {
            await SetEnabledSkills({
              novel_id: result.novelId,
              names: [...current, result.name],
            });
          }
        } catch {
          // 自动启用失败不阻塞已保存的 skill，静默忽略。
        }
      }
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

  const { meta, body } = result
    ? splitFrontmatter(result.rawContent)
    : { meta: {}, body: "" };

  return (
    <div className="flex-1 flex flex-col min-h-0 bg-background">
      <div className="flex items-center justify-between px-6 py-4 border-b shrink-0">
        {status === "running" && (
          <>
            <span className="text-sm text-muted-foreground flex items-center gap-2">
              <Sparkle className="w-4 h-4 text-primary" />
              {t(
                flavor === "mental"
                  ? "extract.progress.mindsetTitle"
                  : "extract.progress.title",
              )}
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

        {status === "done" && result && (
          <>
            <span className="text-sm text-muted-foreground flex items-center gap-2">
              <Sparkle className="w-4 h-4 text-primary" />
              {t("extract.generated")}「{result.name}」
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
                onClick={runExtract}
                disabled={checkingSave || saveMutation.isPending}
                className="h-8 px-3 rounded-lg text-sm border border-border hover:bg-muted transition-colors"
              >
                {t("extract.reExtract")}
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
                onClick={runExtract}
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

      {status === "done" && result ? (
        <div className="flex-1 min-h-0 overflow-y-auto px-6 py-4 space-y-3">
          {Object.keys(meta).length > 0 && (
            <table className="border bg-muted/20 w-full text-sm rounded-lg overflow-hidden">
              <tbody>
                {meta.name && (
                  <tr className="border-b">
                    <td className="px-4 py-2 text-muted-foreground whitespace-nowrap w-16">
                      {t("extract.name")}
                    </td>
                    <td className="px-4 py-2 text-foreground font-semibold">
                      {meta.name}
                    </td>
                  </tr>
                )}
                {(meta.description || result.description) && (
                  <tr className="border-b">
                    <td className="px-4 py-2 text-muted-foreground whitespace-nowrap w-16">
                      {t("extract.summary")}
                    </td>
                    <td className="px-4 py-2 text-foreground">
                      {meta.description || result.description}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          )}
          {flavor === "mental" && result.fidelity && (
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
        </div>
      ) : (
        <div className="flex-1 min-h-0 overflow-y-auto p-6">
          <PatternProgressView
            progress={progress}
            events={events}
            novelTitle={title}
            chapterCount={chapterCount}
            flavor={flavor}
            deepMultiLens={deepMultiLens}
          />
        </div>
      )}
      <ConfirmDialog
        open={overwriteContent !== null}
        title={t("skill.generatedOverwriteTitle")}
        message={t("skill.generatedOverwriteMessage", { name: result?.name })}
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
