import { useState, useEffect, useCallback } from "react";
import {
  Folder,
  FolderInput,
  RefreshCw,
  GitFork,
  Languages,
  Download,
  Loader2,
  CheckCircle2,
  AlertCircle,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  GetVersion,
  CheckUpdate,
  GetAppConfig,
  PickDirectory,
  UpdateDataDir,
} from "@/lib/wailsjs/go/app/App";
import { EventsOn } from "@/lib/wailsjs/runtime";
import ConfirmDialog from "@/components/ui/ConfirmDialog";
import type { update as updateModels } from "@/lib/wailsjs/go/models";
import { useNovels } from "@/components/novel/useNovels";
import { useSettings } from "@/components/settings/useSettings";
import { useSaveGitConfig } from "./useSaveGitConfig";
import { useRebuildNovelIndex } from "./useRebuildNovelIndex";
import UpdateDialog from "@/components/update/UpdateDialog";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

// formatBytes 把字节数转为可读字符串（用于迁移进度展示）。
function formatBytes(n: number): string {
  if (!n || n < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(
    units.length - 1,
    Math.floor(Math.log(n) / Math.log(1024)),
  );
  const v = n / Math.pow(1024, i);
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export default function GeneralConfigTab() {
  const { t, i18n } = useTranslation();
  const [dataDir, setDataDir] = useState("");
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<{
    done: number;
    total: number;
  } | null>(null);
  const [confirmPath, setConfirmPath] = useState<string | null>(null);
  // 3.9: novels 走 useNovels query（共享缓存）。
  const { data: novels = [] } = useNovels();
  // 5.8: settings 走 useSettings query（与 chat/profile 共享 settingsKeys.all 缓存）。
  const settingsQuery = useSettings();
  const saveGitMutation = useSaveGitConfig();
  const rebuildMutation = useRebuildNovelIndex();
  const [selectedID, setSelectedID] = useState<number>(0);
  const [rebuilding, setRebuilding] = useState(false);
  const [gitName, setGitName] = useState("");
  const [gitEmail, setGitEmail] = useState("");
  const [gitSaving, setGitSaving] = useState(false);
  const [gitSaved, setGitSaved] = useState(false);
  const [gitError, setGitError] = useState<string | null>(null);
  const [appVersion, setAppVersion] = useState("");
  const [checking, setChecking] = useState(false);
  const [updateResult, setUpdateResult] =
    useState<updateModels.CheckResult | null>(null);
  const [updateError, setUpdateError] = useState<string | null>(null);
  const [showUpdateDialog, setShowUpdateDialog] = useState(false);

  // GetAppConfig: 直接 import wailsjs（无结构化类型，启动读一次，query 化收益低）。
  // 失败时 toast 提示（原静默吞错违反规则 9），dataDir 保持空字符串 UI 正常渲染。
  // GetVersion: 直接 import wailsjs，低频 GET，保留命令式。
  const refreshDataDir = useCallback(() => {
    GetAppConfig()
      .then((cfg) => {
        setDataDir((cfg?.data_dir as string) || "");
      })
      .catch((err) => {
        toastError(toErrorMessage(err, t("settings.loadFailed")));
      });
  }, [t]);

  useEffect(() => {
    refreshDataDir();
    GetVersion()
      .then((v) => setAppVersion(v || "dev"))
      .catch(() => {});
  }, [refreshDataDir]);

  // 订阅后端数据迁移进度事件，用于展示进度条。
  useEffect(() => {
    const off = EventsOn("data:relocate-progress", (data: any) => {
      if (
        data &&
        typeof data.done === "number" &&
        typeof data.total === "number"
      ) {
        setProgress({ done: data.done, total: data.total });
      }
    });
    return () => off();
  }, []);

  // GetSettings: 复用 useSettings query（与 chat/profile 共享 settingsKeys.all 缓存）。
  // query data ready 后回填 selectedID/gitName/gitEmail。
  // GET 错误由全局中间件接管（settings 前缀 → chat.settingsLoadFailed），不在此处理。
  useEffect(() => {
    const s = settingsQuery.data;
    if (!s) return;
    if (s.last_novel_id) setSelectedID(s.last_novel_id);
    if (s.git_name) setGitName(s.git_name);
    if (s.git_email) setGitEmail(s.git_email);
  }, [settingsQuery.data]);

  async function handleSaveGit() {
    setGitSaving(true);
    setGitSaved(false);
    setGitError(null);
    try {
      await saveGitMutation.mutateAsync({ gitName, gitEmail });
      setGitSaved(true);
      setTimeout(() => setGitSaved(false), 2000);
    } catch (err) {
      setGitError(toErrorMessage(err, t("settings.saveFailed")));
    } finally {
      setGitSaving(false);
    }
  }

  async function handleRebuild() {
    if (!selectedID) return;
    setRebuilding(true);
    try {
      await rebuildMutation.mutateAsync(selectedID);
    } catch (err) {
      console.error("Rebuild failed:", err);
      toastError(toErrorMessage(err, t("settings.rebuildFailed")));
    } finally {
      setRebuilding(false);
    }
  }

  async function handleCheckUpdate() {
    setChecking(true);
    setUpdateResult(null);
    setUpdateError(null);
    try {
      const result = await CheckUpdate(true);
      setUpdateResult(result);
      if (result?.hasUpdate) {
        setShowUpdateDialog(true);
      }
    } catch (err) {
      setUpdateError(toErrorMessage(err, t("update.checkFailed")));
    } finally {
      setChecking(false);
    }
  }

  // 更改数据目录：先选目录，再以 force=false 试迁移；目标非空时后端返回
  // ErrDstNotEmpty（"目标目录非空"），捕获后弹确认框让用户决定是否强制覆盖。
  async function handleChangeDir() {
    if (busy) return;
    setBusy(true);
    setProgress(null);
    try {
      const dir = await PickDirectory();
      if (!dir) {
        setBusy(false);
        return;
      }
      try {
        await UpdateDataDir(dir, false);
        toastSuccess(t("settings.dataDirChangeSuccess"));
        refreshDataDir();
      } catch (e) {
        if (toErrorMessage(e).includes("目标目录非空")) {
          setConfirmPath(dir);
          return;
        }
        throw e;
      }
    } catch (e) {
      toastError(t("settings.dataDirChangeFailed") + ": " + toErrorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  // 确认强制迁移：以 force=true 重试，源数据优先合并进目标。
  async function handleConfirmMigrate() {
    if (!confirmPath) return;
    setBusy(true);
    try {
      await UpdateDataDir(confirmPath, true);
      toastSuccess(t("settings.dataDirChangeSuccess"));
      refreshDataDir();
    } catch (e) {
      toastError(t("settings.dataDirChangeFailed") + ": " + toErrorMessage(e));
    } finally {
      setBusy(false);
      setConfirmPath(null);
    }
  }

  return (
    <div className="flex-1 flex flex-col">
      <h3 className="text-sm font-medium mb-5">{t("settings.basicConfig")}</h3>

      <div className="space-y-2">
        <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
          <Folder className="w-3.5 h-3.5" />
          {t("settings.dataDir")}
        </label>
        <p className="text-[11px] text-muted-foreground">
          {t("settings.dataDirHint")}
        </p>
        <div className="flex items-center gap-2">
          <input
            value={dataDir}
            readOnly
            className="flex-1 h-8 rounded-md border bg-muted/50 px-3 text-xs font-mono focus:outline-none cursor-default"
          />
          <button
            onClick={handleChangeDir}
            disabled={busy}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-xs border hover:bg-muted transition-colors disabled:opacity-50 shrink-0"
          >
            {busy ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <FolderInput className="w-3.5 h-3.5" />
            )}
            {busy
              ? t("settings.dataDirMigrating")
              : t("settings.dataDirChange")}
          </button>
        </div>
        {progress && (
          <div className="flex items-center gap-2">
            <div className="flex-1 h-1.5 rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-primary transition-all"
                style={{
                  width:
                    progress.total > 0
                      ? `${Math.min(100, (progress.done / progress.total) * 100)}%`
                      : "100%",
                }}
              />
            </div>
            <span className="text-[11px] text-muted-foreground font-mono shrink-0">
              {formatBytes(progress.done)} / {formatBytes(progress.total)}
            </span>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirmPath !== null}
        danger
        title={t("settings.dataDirConfirmTitle")}
        message={t("settings.dataDirConfirmMessage")}
        confirmText={t("settings.dataDirChange")}
        loading={busy}
        onConfirm={handleConfirmMigrate}
        onClose={() => setConfirmPath(null)}
      />

      <div className="mt-6 space-y-3">
        <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
          <GitFork className="w-3.5 h-3.5" />
          {t("settings.gitConfig")}
        </label>
        <p className="text-[11px] text-muted-foreground">
          {t("settings.gitConfigDesc")}
        </p>
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground w-12 shrink-0">
              {t("settings.nickname")}
            </span>
            <input
              value={gitName}
              onChange={(e) => setGitName(e.target.value)}
              placeholder="NovelGo"
              className="flex-1 h-8 rounded-md border bg-background px-3 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-muted-foreground w-12 shrink-0">
              {t("settings.email")}
            </span>
            <input
              value={gitEmail}
              onChange={(e) => setGitEmail(e.target.value)}
              placeholder="novelgo@local"
              className="flex-1 h-8 rounded-md border bg-background px-3 text-xs focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
          <div className="flex items-center justify-between gap-2 pt-1">
            <div className="flex items-center gap-2">
              {gitError && (
                <span className="text-[11px] text-rose-500">{gitError}</span>
              )}
            </div>
            <button
              onClick={handleSaveGit}
              disabled={gitSaving}
              className="inline-flex items-center gap-1.5 h-8 px-4 rounded-md text-xs border hover:bg-muted transition-colors disabled:opacity-50"
            >
              {gitSaving
                ? t("common.saving")
                : gitSaved
                  ? t("common.saved")
                  : t("common.save")}
            </button>
          </div>
        </div>
      </div>

      <div className="mt-6 space-y-2">
        <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
          <Languages className="w-3.5 h-3.5" />
          {t("settings.language")}
        </label>
        <div className="inline-flex items-center gap-1 rounded-lg bg-muted/60 p-0.5">
          <button
            onClick={() => i18n.changeLanguage("zh-CN")}
            className={`h-7 px-3 rounded-md text-xs transition-colors ${
              i18n.language.startsWith("zh")
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            中文
          </button>
          <button
            onClick={() => i18n.changeLanguage("en")}
            className={`h-7 px-3 rounded-md text-xs transition-colors ${
              i18n.language === "en"
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            English
          </button>
        </div>
      </div>

      <div className="mt-6 space-y-2">
        <label className="text-xs font-medium text-muted-foreground">
          {t("settings.maintenance")}
        </label>
        <p className="text-[11px] text-muted-foreground">
          {t("settings.rebuildIndexDesc")}
        </p>
        <div className="flex items-center gap-2">
          <select
            value={selectedID}
            onChange={(e) => setSelectedID(Number(e.target.value))}
            className="h-8 rounded-md border bg-background px-2 text-xs focus:outline-none"
          >
            {novels.map((n) => (
              <option key={n.id} value={n.id}>
                {n.title}
              </option>
            ))}
          </select>
          <button
            onClick={handleRebuild}
            disabled={rebuilding || !selectedID}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-xs border hover:bg-muted transition-colors disabled:opacity-50"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${rebuilding ? "animate-spin" : ""}`}
            />
            {rebuilding ? t("settings.rebuilding") : t("settings.rebuildIndex")}
          </button>
        </div>
      </div>

      <div className="mt-6 space-y-2">
        <label className="text-xs font-medium text-muted-foreground flex items-center gap-1.5">
          <Download className="w-3.5 h-3.5" />
          {t("update.versionAndUpdate")}
        </label>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">
            {t("update.currentVersion")}
          </span>
          <span className="text-xs font-mono text-foreground">
            v{appVersion}
          </span>
          <button
            onClick={handleCheckUpdate}
            disabled={checking}
            className="inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-xs border hover:bg-muted transition-colors disabled:opacity-50"
          >
            {checking ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <RefreshCw className="w-3.5 h-3.5" />
            )}
            {checking ? t("update.checking") : t("update.checkNow")}
          </button>
          {updateResult?.hasUpdate && (
            <span className="inline-flex items-center gap-1 text-xs text-primary">
              <Download className="w-3 h-3" />
              {t("update.versionLabel", {
                version: updateResult.latest.tag_name,
              })}
            </span>
          )}
          {updateResult && !updateResult.hasUpdate && (
            <span className="inline-flex items-center gap-1 text-xs text-tag-green-foreground">
              <CheckCircle2 className="w-3 h-3" />
              {t("update.upToDate")}
            </span>
          )}
          {updateError && (
            <span className="inline-flex items-center gap-1 text-xs text-rose-500">
              <AlertCircle className="w-3 h-3" />
              {updateError}
            </span>
          )}
        </div>
      </div>

      <UpdateDialog
        open={showUpdateDialog}
        result={updateResult}
        onClose={() => setShowUpdateDialog(false)}
      />
    </div>
  );
}
