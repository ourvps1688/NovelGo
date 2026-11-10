import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  ListEditorFeedbacks,
  RecordEditorFeedback,
} from "@/lib/wailsjs/go/app/App";
import type { app } from "@/lib/wailsjs/go/models";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

interface Props {
  novelId: number;
}

const QUERY_KEY = "editor-feedback";

// 编辑反馈的显式录入面板（#111 P2，§3.1）：把 editor-feedback 技能从
// 「只有 LLM 在 chat 里碰巧调 MCP 工具」变成用户可操作的确定性入口。
// 后端复用 record_editor_feedback 的两段式落库 + 结构化拆解 + F 系工单回流；
// 拆解不可用时只留原话（parse_status=raw），绝不伪造结构化产物。
export default function EditorFeedbackPanel({ novelId }: Props) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [workTitle, setWorkTitle] = useState("");
  const [platform, setPlatform] = useState("");
  const [channel, setChannel] = useState("");
  const [verdict, setVerdict] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const key = ["feature-usage", QUERY_KEY, novelId];
  const { data: rows = [], isError } = useQuery({
    queryKey: key,
    queryFn: async () =>
      (await ListEditorFeedbacks(novelId)) as app.EditorFeedbackRow[],
  });
  const canSubmit =
    !!novelId &&
    !submitting &&
    workTitle.trim().length > 0 &&
    platform.trim().length > 0 &&
    verdict.trim().length > 0;

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      const res = await RecordEditorFeedback({
        novel_id: novelId,
        work_title: workTitle.trim(),
        platform: platform.trim(),
        channel: channel.trim(),
        verdict: verdict.trim(),
      } as app.EditorFeedbackInput);
      if (res?.parse_status === "raw") {
        toastInfo(t("skillMatrix.feedback.rawToast"));
      } else {
        toastSuccess(t("skillMatrix.feedback.savedToast"));
      }
      setVerdict("");
      await qc.invalidateQueries({ queryKey: key });
    } catch (e) {
      toastError(toErrorMessage(e, t("skillMatrix.feedback.failed")));
    } finally {
      setSubmitting(false);
    }
  };

  const inputClass =
    "w-full rounded-md border bg-background px-2.5 py-1.5 text-xs text-foreground outline-none focus:border-primary/60";

  return (
    <section data-testid="editor-feedback-panel" className="mb-6 space-y-3">
      <div>
        <h2 className="text-sm font-medium text-foreground">
          {t("skillMatrix.feedback.title")}
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("skillMatrix.feedback.hint")}
        </p>
      </div>

      <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        <input
          aria-label={t("skillMatrix.feedback.workTitle")}
          placeholder={t("skillMatrix.feedback.workTitle")}
          value={workTitle}
          onChange={(e) => setWorkTitle(e.target.value)}
          className={inputClass}
        />
        <input
          aria-label={t("skillMatrix.feedback.platform")}
          placeholder={t("skillMatrix.feedback.platform")}
          value={platform}
          onChange={(e) => setPlatform(e.target.value)}
          className={inputClass}
        />
        <input
          aria-label={t("skillMatrix.feedback.channel")}
          placeholder={t("skillMatrix.feedback.channel")}
          value={channel}
          onChange={(e) => setChannel(e.target.value)}
          className={inputClass}
        />
      </div>
      <textarea
        aria-label={t("skillMatrix.feedback.verdict")}
        placeholder={t("skillMatrix.feedback.verdictPlaceholder")}
        value={verdict}
        onChange={(e) => setVerdict(e.target.value)}
        rows={3}
        className={inputClass}
      />
      <button
        type="button"
        data-testid="feedback-submit"
        disabled={!canSubmit}
        onClick={submit}
        className="rounded-md border border-primary/40 bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 disabled:opacity-50"
      >
        {submitting
          ? t("skillMatrix.feedback.submitting")
          : t("skillMatrix.feedback.submit")}
      </button>

      <div>
        <h3 className="text-xs font-medium text-foreground">
          {t("skillMatrix.feedback.listTitle")}
        </h3>
        {isError || rows.length === 0 ? (
          <p
            data-testid="feedback-empty"
            className="mt-1 text-xs text-muted-foreground"
          >
            {t("skillMatrix.feedback.empty")}
          </p>
        ) : (
          <ul className="mt-2 space-y-1.5">
            {rows.slice(0, 8).map((row) => (
              <li
                key={row.id}
                data-testid="feedback-row"
                className="rounded-md border px-3 py-2 text-xs"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{row.work_title}</span>
                  <span className="text-muted-foreground">{row.platform}</span>
                  <span className="text-muted-foreground">
                    {row.created_at}
                  </span>
                  <span
                    data-testid="feedback-status"
                    className={
                      row.parse_status === "parsed"
                        ? "text-primary"
                        : "text-destructive"
                    }
                  >
                    {row.parse_status === "parsed"
                      ? t("skillMatrix.feedback.parsed")
                      : t("skillMatrix.feedback.raw")}
                  </span>
                  <span className="text-muted-foreground">
                    {t("skillMatrix.feedback.issueCount", {
                      count: row.issue_count ?? 0,
                    })}
                  </span>
                </div>
                {row.summary && (
                  <p className="mt-1 whitespace-pre-wrap text-muted-foreground">
                    {row.summary}
                  </p>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}
