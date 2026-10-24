import { useTranslation } from "react-i18next";
import { useFeatureUsage } from "./useFeatureUsage";

// 功能使用率区块（P4 WBS T2.3）：近 30 天 top 功能计数，供取舍规则看的数据。
// 克制呈现：不做运营大屏（宁缺勿错）。
// 降级纪律：有度量才显示区块；无数据/加载失败=静默不渲染，
// 界面与旧行为逐字一致（零报错零空态噪音）。
const TOP_ROWS = 10;

export default function FeatureUsageSection() {
  const { t } = useTranslation();
  const { data } = useFeatureUsage(30);
  const rows = (data ?? []).slice(0, TOP_ROWS);
  if (rows.length === 0) return null;
  return (
    <section data-testid="feature-usage-section">
      <h2 className="text-sm font-medium text-foreground mb-1">
        {t("profile.featureUsage.title")}
      </h2>
      <p className="text-xs text-muted-foreground mb-3">
        {t("profile.featureUsage.hint")}
      </p>
      <ul className="space-y-1.5">
        {rows.map((row) => (
          <li
            key={row.feature_key}
            data-testid="feature-usage-row"
            data-count={row.count}
            className="flex items-center gap-3 text-xs"
          >
            <span className="text-muted-foreground truncate">
              {row.feature_key}
            </span>
            <span className="ml-auto tabular-nums text-foreground">
              {t("profile.featureUsage.count", { count: row.count })}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}
