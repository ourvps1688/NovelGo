import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Wand2, RefreshCw, Loader2, ImageIcon, Sparkles } from "lucide-react";
import type { novel } from "@/lib/wailsjs/go/models";
import {
  GenerateCover,
  GenerateCoverPrompt,
  OptimizeCoverPrompt,
} from "@/lib/wailsjs/go/app/App";
import { useLLMConfig } from "@/components/settings/useLLMConfig";
import { useSettings } from "@/components/settings/useSettings";
import { toErrorMessage } from "@/utils/error";

interface Props {
  open: boolean;
  novel?: novel.Novel | null;
  onClose: () => void;
  onGenerated: (novelID: number) => void;
}

// 网文封面尺寸预设：竖版 2:3 为网文平台（起点/番茄/晋江）封面位最通用比例。
const SIZE_OPTIONS = [
  { value: "1024x1536", labelKey: "novel.coverSizePortrait" },
  { value: "1024x1024", labelKey: "novel.coverSizeSquare" },
  { value: "1536x1024", labelKey: "novel.coverSizeLandscape" },
  { value: "custom", labelKey: "novel.coverSizeCustom" },
];

// 封面风格预设：键需与后端 coverStylePresets 对齐。
const STYLE_PRESETS = [
  { value: "", labelKey: "novel.coverStyleNone" },
  { value: "realistic", labelKey: "novel.coverStyleRealistic" },
  { value: "guofeng", labelKey: "novel.coverStyleGuofeng" },
  { value: "anime", labelKey: "novel.coverStyleAnime" },
  { value: "gothic", labelKey: "novel.coverStyleGothic" },
  { value: "cyberpunk", labelKey: "novel.coverStyleCyberpunk" },
  { value: "cinematic", labelKey: "novel.coverStyleCinematic" },
  { value: "pixel", labelKey: "novel.coverStylePixel" },
  { value: "watercolor", labelKey: "novel.coverStyleWatercolor" },
];

// 与后端 defaultNegativePrompt 一致的英文负向约束，便于用户查看/修改。
const DEFAULT_NEGATIVE_PROMPT =
  "text, words, letters, title, logo, watermark, signature, blurry, low resolution, " +
  "deformed, extra limbs, bad anatomy, distorted proportions, oversaturated, ugly, duplicate";

function parseSize(s: string): { w: number; h: number } | null {
  const m = s.match(/^(\d+)\s*[xX]\s*(\d+)$/);
  if (!m) return null;
  const w = parseInt(m[1], 10);
  const h = parseInt(m[2], 10);
  if (w <= 0 || h <= 0) return null;
  return { w, h };
}

// GenerateCoverDialog：AI 封面生成弹窗。
// 复用用户已配置的 LLM Provider（图像端点 = Provider.ChatURL 推导，APIKey 复用），
// 提示词可由 LLM 基于书名/题材/简介自动生成并允许编辑/精修；默认竖版 2:3 网文封面尺寸。
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
  const [size, setSize] = useState("1024x1536");
  const [customSize, setCustomSize] = useState("800x1200");
  const [stylePreset, setStylePreset] = useState("");
  const [negativePrompt, setNegativePrompt] = useState(DEFAULT_NEGATIVE_PROMPT);
  const [loadingPrompt, setLoadingPrompt] = useState(false);
  const [optimizing, setOptimizing] = useState(false);
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
    setSize("1024x1536");
    setCustomSize("800x1200");
    setStylePreset("");
    setNegativePrompt(DEFAULT_NEGATIVE_PROMPT);
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

  async function handleOptimize() {
    if (!novel || !prompt.trim() || optimizing) return;
    setOptimizing(true);
    setError("");
    try {
      const p = await OptimizeCoverPrompt(novel.id, prompt.trim());
      if (p) setPrompt(p);
    } catch (e) {
      setError(toErrorMessage(e, t("novel.coverOptimizeFailed")));
    } finally {
      setOptimizing(false);
    }
  }

  const effectiveSize = size === "custom" ? customSize.trim() : size;
  const aspect = parseSize(effectiveSize) ?? { w: 2, h: 3 };

  async function handleGenerate() {
    if (!novel || !prompt.trim() || generating) return;
    if (providers.length === 0) {
      setError(t("novel.coverNoProvider"));
      return;
    }
    if (!parseSize(effectiveSize)) {
      setError(t("novel.coverSizeInvalid"));
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
        size: effectiveSize,
        style_preset: stylePreset,
        negative_prompt: negativePrompt.trim(),
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
      <div className="relative bg-background rounded-xl shadow-2xl border w-[540px] max-w-[92vw] max-h-[90vh] overflow-y-auto p-6">
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
          {/* 供应商 + 模型 */}
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

          {/* 尺寸（网文竖版推荐） */}
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
                <option key={s.value} value={s.value}>
                  {t(s.labelKey)}
                </option>
              ))}
            </select>
            {size === "custom" && (
              <input
                type="text"
                value={customSize}
                onChange={(e) => setCustomSize(e.target.value)}
                placeholder="800x1200"
                className="mt-2 w-full h-9 rounded-md border bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              />
            )}
            <p className="mt-1 text-[10px] text-muted-foreground">
              {t("novel.coverSizeHint")}
            </p>
          </div>

          {/* 风格预设 */}
          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1.5">
              {t("novel.coverStyle")}
            </label>
            <select
              value={stylePreset}
              onChange={(e) => setStylePreset(e.target.value)}
              className="w-full h-9 rounded-md border bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {STYLE_PRESETS.map((s) => (
                <option key={s.value} value={s.value}>
                  {t(s.labelKey)}
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
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => void handleOptimize()}
                  disabled={optimizing || !prompt.trim()}
                  className="inline-flex items-center gap-1 text-xs text-primary hover:underline disabled:opacity-50"
                >
                  <Sparkles
                    className={`w-3 h-3 ${optimizing ? "animate-spin" : ""}`}
                  />
                  {t("novel.coverOptimizePrompt")}
                </button>
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
            </div>
            <textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              rows={5}
              placeholder="Describe the cover you want the AI to draw…"
              className="w-full rounded-md border bg-background px-3 py-2 text-sm resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>

          {/* 负向提示词 */}
          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1.5">
              {t("novel.coverNegativePrompt")}
            </label>
            <textarea
              value={negativePrompt}
              onChange={(e) => setNegativePrompt(e.target.value)}
              rows={2}
              placeholder="text, logo, watermark, blurry…"
              className="w-full rounded-md border bg-background px-3 py-2 text-xs resize-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>

          {/* 预览（按所选尺寸的真实宽高比） */}
          {generated && (
            <div>
              <p className="text-xs font-medium text-muted-foreground mb-1.5">
                {t("novel.coverPreview")}
              </p>
              <div
                className="w-32 mx-auto rounded-md overflow-hidden border shadow-sm bg-muted"
                style={{ aspectRatio: `${aspect.w} / ${aspect.h}` }}
              >
                <img
                  key={previewKey}
                  src={`/covers/${novel.id}?v=${previewKey}`}
                  alt=""
                  className="w-full h-full object-cover block"
                />
              </div>
              <p className="text-center text-xs text-status-ok mt-1.5">
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
