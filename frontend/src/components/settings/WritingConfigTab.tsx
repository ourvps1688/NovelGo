import { useEffect, useState } from "react";
import { Loader2, CheckCircle2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { useWritingConfig, useSaveWritingConfig } from "./useWritingConfig";
import { toErrorMessage } from "@/utils/error";

// WritingConfigTab: 本书"写作评分"配置面板。
// - 自动评分开关（auto_score_enabled，默认开）
// - 及格阈值（score_threshold，0-100，默认 60）
// - 最大重试次数（score_max_retries，1-10，默认 3）
// 配置按 activeNovelId 加载/保存；未选书时提示先选书。
export default function WritingConfigTab() {
  const { t } = useTranslation();
  const activeNovelId = useNovelStore((s) => s.activeNovelId);
  const query = useWritingConfig(activeNovelId);
  const saveMutation = useSaveWritingConfig(activeNovelId);

  const [enabled, setEnabled] = useState(true);
  const [threshold, setThreshold] = useState(60);
  const [maxRetries, setMaxRetries] = useState(3);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // query data ready 后回填本地编辑态。
  useEffect(() => {
    const c = query.data;
    if (!c) return;
    setEnabled(c.auto_score_enabled);
    setThreshold(c.score_threshold);
    setMaxRetries(c.score_max_retries);
  }, [query.data]);

  async function handleSave() {
    if (activeNovelId <= 0) return;
    setSaved(false);
    setError(null);
    try {
      await saveMutation.mutateAsync({
        auto_score_enabled: enabled,
        score_threshold: threshold,
        score_max_retries: maxRetries,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      setError(toErrorMessage(err, t("settings.saveFailed")));
    }
  }

  if (activeNovelId <= 0) {
    return (
      <div className="flex-1 flex flex-col">
        <h3 className="text-sm font-medium mb-5">
          {t("settings.writingScore")}
        </h3>
        <p className="text-xs text-muted-foreground">
          {t("settings.selectNovelFirstHint")}
        </p>
      </div>
    );
  }

  if (query.isLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const normalizedThreshold = Math.max(0, Math.min(100, threshold));
  const normalizedRetries = Math.max(1, Math.min(10, maxRetries));

  return (
    <div className="flex-1 flex flex-col">
      <h3 className="text-sm font-medium mb-5">{t("settings.writingScore")}</h3>

      <p className="text-[11px] text-muted-foreground mb-5">
        {t("settings.writingScoreDesc")}
      </p>

      {/* 启用开关 */}
      <div className="flex items-center justify-between gap-3 py-2">
        <div className="min-w-0">
          <div className="text-xs font-medium text-foreground">
            {t("settings.autoScoreEnabled")}
          </div>
          <p className="text-[11px] text-muted-foreground mt-0.5">
            {t("settings.autoScoreEnabledDesc")}
          </p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={enabled}
          aria-label={t("settings.autoScoreEnabled")}
          onClick={() => setEnabled((v) => !v)}
          className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
            enabled ? "bg-primary" : "bg-muted-foreground/30"
          }`}
        >
          <span
            className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
              enabled ? "translate-x-4" : "translate-x-0.5"
            }`}
          />
        </button>
      </div>

      {/* 及格阈值 */}
      <div className="mt-4 space-y-1.5">
        <label className="text-xs font-medium text-foreground">
          {t("settings.scoreThreshold")}
        </label>
        <p className="text-[11px] text-muted-foreground">
          {t("settings.scoreThresholdDesc")}
        </p>
        <div className="flex items-center gap-2">
          <input
            type="number"
            min={0}
            max={100}
            value={normalizedThreshold}
            onChange={(e) => setThreshold(Number(e.target.value))}
            className="h-8 w-24 rounded-md border bg-background px-3 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
          />
          <span className="text-xs text-muted-foreground">
            {t("settings.scoreThresholdUnit")}
          </span>
        </div>
      </div>

      {/* 最大重试次数 */}
      <div className="mt-4 space-y-1.5">
        <label className="text-xs font-medium text-foreground">
          {t("settings.scoreMaxRetries")}
        </label>
        <p className="text-[11px] text-muted-foreground">
          {t("settings.scoreMaxRetriesDesc")}
        </p>
        <div className="flex items-center gap-2">
          <input
            type="number"
            min={1}
            max={10}
            value={normalizedRetries}
            onChange={(e) => setMaxRetries(Number(e.target.value))}
            className="h-8 w-24 rounded-md border bg-background px-3 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
          />
          <span className="text-xs text-muted-foreground">
            {t("settings.scoreMaxRetriesUnit")}
          </span>
        </div>
      </div>

      {/* 底部保存栏 */}
      <div className="flex items-end justify-between gap-3 pt-5 border-t mt-6">
        <span className="flex-1 text-xs whitespace-pre-line max-w-[60ch] text-left">
          {error && <span className="text-rose-500">{error}</span>}
          {saved && !error && (
            <span className="inline-flex items-center gap-1 text-success-foreground">
              <CheckCircle2 className="w-3 h-3" />
              {t("common.saved")}
            </span>
          )}
        </span>
        <button
          onClick={handleSave}
          disabled={saveMutation.isPending}
          className="h-8 px-4 rounded-md bg-primary text-primary-foreground text-sm disabled:opacity-50 shrink-0"
        >
          {saveMutation.isPending ? t("common.saving") : t("common.save")}
        </button>
      </div>
    </div>
  );
}
