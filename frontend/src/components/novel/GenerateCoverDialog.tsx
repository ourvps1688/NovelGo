import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Wand2, RefreshCw, Loader2, ImageIcon } from "lucide-react";
import type { novel } from "@/lib/wailsjs/go/models";
import { GenerateCover, GenerateCoverPrompt } from "@/lib/wailsjs/go/app/App";
import { useLLMConfig } from "@/components/settings/useLLMConfig";
import { useSettings } from "@/components/settings/useSettings";
import { toErrorMessage } from "@/utils/error";

interface Props {
  open: boolean;
  novel?: novel.Novel | null;
  onClose: () => void;
  onGenerated: (novelID: number) => void;
}

const SIZE_OPTIONS = ["1024x1024", "1536x1024", "1024x1536", "auto"];

// GenerateCoverDialog：AI 封面生成弹窗。
// 复用用户已配置的 LLM Provider（图像端点 = Provider.ChatURL 推导，APIKey 复用），
// 提示词可由 LLM 基于书名/题材/简介自动生成并允许编辑。
export default function GenerateCoverDialog({
  open,
  novel,
  onClose,
  onGenerated,
}: Props) {
  const { t } = useTranslation();
  const { data: llmConfig } = useLLMConfig();
  const { data: settings } = useSettings();

  const providers =
    llmConfig?.providers?.filter((p) => p.api_key && p.api_key.trim() !== "") ??
    [];

  const [prompt, setPrompt] = useState("");
  const [providerName, setProviderName] = useState("");
  const [model, setModel] = useState("gpt-image-1");
  const [size, setSize] = useState("1024x1024");
  const [loadingPrompt, setLoadingPrompt] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState("");
  const [previewKey, setPreviewKey] = useState(0);
  const [generated, setGenerated] = useState(false);

  // 打开时初始化：默认供应商 + 模型，并尝试自动生成提示词
  useEffect(() => {
    if (!open || !novel) return;
    setError("");
    setGenerated(false);
    setPreviewKey(0);
    setProviderName(providers[0]?.key ?? "");
    setModel(settings?.image_model ?? "gpt-image-1");
    setSize("1024x1024");
    setPrompt("");
    void autoGenPrompt();
    // 仅依赖 open / novel，避免 provider/settings 变化时重复触发
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, novel?.id]);

  async function autoGenPrompt() {
    if (!novel) return;
    setLoadingPrompt(true);
    setError("");
    try {
      const p = await GenerateCoverPrompt(novel.id);
      if (p) setPrompt(p);
    } catch (e) {
      // 自动生成失败不阻断：用户可手动填写
      setError(toErrorMessage(e, t("novel.coverGenFailed")));
    } finally {
      setLoadingPrompt(false);
    }
  }

  async function handleGenerate() {
    if (!novel || !prompt.trim() || generating) return;
    if (providers.length === 0) {
      setError(t("novel.coverNoProvider"));
      return;
    }
    setGenerating(true);
    setError("");
    try {
      await GenerateCover({
        novel_id: novel.id,
        prompt: prompt.trim(),
        provider_name: providerName,
        model: model.trim() || "gpt-image-1",
        size,
      });
      setGenerated(true);
      setPreviewKey(Date.now());
      onGenerated(novel.id);
    } catch (e) {
      setError(toErrorMessage(e, t("novel.coverGenFailed")));
    } finally {
      setGenerating(false);
    }
  }

  if (!open || !novel) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative bg-background rounded-xl shadow-2xl border w-[520px] max-w-[92vw] max-h-[90vh] overflow-y-auto p-6">
        <button
          onClick={onClose}
          className="absolute top-3 right-3 w-7 h-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
        >
          ✕
        </button>

        <h2 className="text-base font-semibold mb-1 flex items-center gap-2">
          <Wand2 className="w-4 h-4 text-primary" />
          {t("novel.aiCoverTitle")}
        </h2>
        <p className="text-xs text-muted-foreground mb-4 truncate">
          {novel.title}
          {novel.genre ? ` · ${novel.genre}` : ""}
        </p>

        <div className="space-y-4">
          {/* 供应商 + 模型 + 尺寸 */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                {t("novel.coverProvider")}
              </label>
              {providers.length === 0 ? (
                <div className="h-9 flex items-center text-xs text-destructive px-2 border rounded-md bg-danger-bg">
                  {t("novel.coverNoProvider")}
                </div>
              ) : (
                <select
                  value={providerName}
                  onChange={(e) => setProviderName(e.target.value)}
                  className="w-full h-9 rounded-md border bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {providers.map((p) => (
                    <option key={p.key} value={p.key}>
                      {p.name}
                    </option>
                  ))}
                </select>
              )}
            </div>
            <div>
              <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                {t("novel.coverModel")}
              </label>
              <input
                type="text"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="gpt-image-1"
                className="w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            </div>
          </div>

          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1.5">
              {t("novel.coverSize")}
            </label>
            <select
              value={size}
              onChange={(e) => setSize(e.target.value)}
              className="w-full h-9 rounded-md border bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {SIZE_OPTIONS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </div>

          {/* 提示词 */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                {t("novel.coverPromptLabel")}
              </label>
              <button
                type="button"
                onClick={() => void autoGenPrompt()}
                disabled={loadingPrompt}
                className="inline-flex items-center gap-1 text-xs text-primary hover:underline disabled:opacity-50"
              >
                <RefreshCw
                  className={`w-3 h-3 ${loadingPrompt ? "animate-spin" : ""}`}
                />
                {t("novel.generatePromptBtn")}
              </button>
            </div>
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              rows={5}
              placeholder="Describe the cover you want the AI to draw…"
              className="w-full rounded-md border bg-background px-3 py-2 text-sm resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>

          {/* 预览 */}
          {generated && (
            <div>
              <p className="text-xs font-medium text-muted-foreground mb-1.5">
                {t("novel.coverPreview")}
              </p>
              <div className="w-32 mx-auto aspect-[3/4] rounded-md overflow-hidden border shadow-sm bg-muted">
                <img
                  key={previewKey}
                  src={`/covers/${novel.id}?v=${previewKey}`}
                  alt=""
                  className="w-full h-full object-cover block"
                />
              </div>
              <p className="text-center text-xs text-emerald-600 dark:text-emerald-400 mt-1.5">
                {t("novel.coverGenSuccess")}
              </p>
            </div>
          )}

          {error && (
            <p className="text-sm text-destructive bg-danger-bg border border-danger-border rounded-md px-3 py-2">
              {error}
            </p>
          )}
        </div>

        <div className="flex justify-end gap-2 mt-6">
          <button
            onClick={onClose}
            className="h-9 px-4 rounded-md text-sm border hover:bg-muted transition-colors"
          >
            {t("common.close")}
          </button>
          <button
            onClick={() => void handleGenerate()}
            disabled={!prompt.trim() || generating || providers.length === 0}
            className="inline-flex items-center gap-1.5 h-9 px-4 rounded-md text-sm bg-primary text-primary-foreground hover:opacity-90 transition-opacity disabled:opacity-50"
          >
            {generating ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <ImageIcon className="w-4 h-4" />
            )}
            {generating ? t("novel.generatingCover") : t("novel.aiCover")}
          </button>
        </div>
      </div>
    </div>
  );
}
