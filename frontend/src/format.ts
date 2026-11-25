import type { Entry, Outcome, PlanStatus, Preset, SchedulePlan, Snapshot, ToolStatus } from "./types";

const WEEKDAY = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
/** Monday-first display order of the Sunday-indexed weekdays array. */
export const DAY_ORDER = [1, 2, 3, 4, 5, 6, 0];
export const DAY_SHORT = ["日", "一", "二", "三", "四", "五", "六"];

export const PRESET_LABEL: Record<Preset, string> = {
  "early-bird": "早鸟",
  three: "三轮",
  custom: "自定义",
};

export function hhmm(t: string | Date): string {
  const d = typeof t === "string" ? new Date(t) : t;
  return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
}

export function isValidTime(t?: string): t is string {
  return !!t && !t.startsWith("0001-") && !Number.isNaN(Date.parse(t));
}

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** "今天 · 周三", "昨天 · 周二", "11月23日 · 周一". */
export function dayLabel(d: Date, now = new Date()): string {
  const diff = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
  const wd = WEEKDAY[d.getDay()];
  if (diff === 0) return `今天 · ${wd}`;
  if (diff === 1) return `昨天 · ${wd}`;
  if (diff === -1) return `明天 · ${wd}`;
  return `${d.getMonth() + 1}月${d.getDate()}日 · ${wd}`;
}

/** hhmm, prefixed with the day when it is not today ("明天 06:30"). */
export function whenLabel(t: string, now = new Date()): string {
  const d = new Date(t);
  const diff = Math.round((startOfDay(d) - startOfDay(now)) / 86_400_000);
  if (diff === 0) return hhmm(d);
  if (diff === 1) return `明天 ${hhmm(d)}`;
  return `${WEEKDAY[d.getDay()]} ${hhmm(d)}`;
}

/** "2 小时 10 分", "8 分钟", "不到 1 分钟". */
export function countdown(to: string, now = Date.now()): string {
  const mins = Math.floor((Date.parse(to) - now) / 60_000);
  if (mins < 1) return "不到 1 分钟";
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  if (h === 0) return `${m} 分钟`;
  if (h >= 24) return `${Math.floor(h / 24)} 天 ${h % 24} 小时`;
  return m === 0 ? `${h} 小时` : `${h} 小时 ${m} 分`;
}

export function daysLabel(w: boolean[]): string {
  const on = DAY_ORDER.filter((i) => w[i]);
  if (on.length === 7) return "每天";
  if (on.length === 0) return "不唤醒";
  if (on.length === 5 && on.every((i) => i >= 1 && i <= 5)) return "工作日";
  if (on.length === 2 && w[0] && w[6]) return "周末";
  return "周" + on.map((i) => DAY_SHORT[i]).join("、");
}

export function scheduleSummary(p: SchedulePlan, times: string[] | null): string {
  return `${PRESET_LABEL[p.preset]} · ${daysLabel(p.weekdays)} ${(times ?? []).join("、")}`.trim();
}

/** "codex-cli 0.171.0" / "2.1.327 (Claude Code)" -> "0.171.0" / "2.1.327". */
export function cleanVersion(v?: string): string {
  return v?.match(/\d+\.\d+[\w.+-]*/)?.[0] ?? v ?? "";
}

export function toolFor(snap: Snapshot, toolID: string): ToolStatus | undefined {
  return snap.report.tools?.find((t) => t.id === toolID);
}

const CLAUDE_TIER: Record<string, string> = { pro: "Pro", max: "Max", team: "Team", enterprise: "Enterprise" };
const CHATGPT_TIER: Record<string, string> = {
  plus: "Plus", pro: "Pro", team: "Team", business: "Business", enterprise: "Enterprise", edu: "Edu", free: "Free",
};

/** "Claude Pro" when the tier is known, the generic plan name otherwise. */
export function planTitle(snap: Snapshot, ps: PlanStatus): string {
  const tool = toolFor(snap, ps.plan.tool);
  const tier = tool?.auth?.plan?.toLowerCase();
  if (ps.plan.tool === "claude") {
    return tier && CLAUDE_TIER[tier] ? `Claude ${CLAUDE_TIER[tier]}` : ps.plan.name;
  }
  if (ps.plan.tool === "codex") {
    const t = tier ?? snap.quota?.[ps.plan.id]?.planType?.toLowerCase();
    return t && CHATGPT_TIER[t] ? `ChatGPT ${CHATGPT_TIER[t]}` : ps.plan.name;
  }
  return ps.plan.name;
}

export const LOGIN_COMMAND: Record<string, string> = {
  claude: "claude auth login",
  codex: "codex login",
};

export type Tone = "ok" | "warn" | "bad" | "muted";

/** Short result label and tone for a log entry. */
export function outcomeChip(e: Entry): { label: string; tone: Tone } {
  const o: Outcome = e.outcome;
  switch (o) {
    case "ok":
      if (e.trigger === "makeup") return { label: "补发成功", tone: "ok" };
      if (e.trigger === "retry") return { label: "重试成功", tone: "ok" };
      return { label: "成功", tone: "ok" };
    case "skipped": return { label: "已跳过", tone: "warn" };
    case "offline": return { label: "等待网络", tone: "warn" };
    case "network_error": return { label: "网络错误", tone: "warn" };
    case "timeout": return { label: "超时", tone: "warn" };
    case "busy": return { label: "稍后重试", tone: "warn" };
    case "not_logged_in": return { label: "未登录", tone: "bad" };
    case "quota_exhausted": return { label: "额度用完", tone: "bad" };
    case "expired": return { label: "已错过", tone: "bad" };
    case "paused": return { label: "已暂停", tone: "muted" };
    default: return { label: "失败", tone: "bad" };
  }
}

/** Translates the reasons Kikri itself writes; CLI errors stay as they are. */
export function reasonText(msg: string | undefined): string {
  if (!msg) return "";
  const rules: [RegExp, (m: RegExpMatchArray) => string][] = [
    [/^a window is already running; it resets at (\d\d:\d\d)$/, (m) => `窗口已在跑，${m[1]} 刷新`],
    [/^the window opened by the (\d\d:\d\d) wake runs until (\d\d:\d\d)$/, (m) => `${m[1]} 唤醒的窗口要到 ${m[2]} 才刷新`],
    [/^Kikri already woke it at (\d\d:\d\d)$/, (m) => `Kikri ${m[1]} 已经唤醒过`],
    [/^you used it at (\d\d:\d\d), so a window is already running$/, (m) => `窗口已在跑（你 ${m[1]} 用过）`],
    [/^this plan has no 5-hour window \((.*)\)$/, (m) => `这个套餐没有 5 小时窗口（${m[1]}）`],
    [/^paused for today$/, () => "今天已暂停"],
    [/^still failing \(no network\?\) when the make-up deadline (\d\d:\d\d) passed$/, (m) => `一直没联网，过了补发截止时间 ${m[1]}`],
    [/^(.*) when the make-up deadline (\d\d:\d\d) passed$/, (m) => `电脑没开或没联网，过了补发截止时间 ${m[2]}`],
  ];
  for (const [re, f] of rules) {
    const m = msg.match(re);
    if (m) return f(m);
  }
  return msg;
}

/** "3.6s · 633 tokens · 11:30 刷新". */
export function resultDetail(e: Entry, withReset = true): string {
  const r = e.result;
  const parts: string[] = [];
  if (e.trigger === "makeup") parts.push("开盖后补发");
  if (e.trigger === "manual") parts.push("手动");
  if (e.outcome === "ok" && r) {
    if (r.durationMs) parts.push(`${(r.durationMs / 1000).toFixed(1)}s`);
    if (r.usage) parts.push(`${r.usage.inputTokens.toLocaleString("en-US")} tokens`);
    const w = r.windows?.find((x) => x.kind === "five_hour");
    if (withReset && w && isValidTime(w.resetsAt)) parts.push(`${hhmm(w.resetsAt)} 刷新`);
    return parts.join(" · ");
  }
  const tool = r?.tool ?? "";
  switch (e.outcome) {
    case "offline": parts.push("没有网络，联网后补发"); break;
    case "not_logged_in": parts.push(`请在终端运行 ${LOGIN_COMMAND[tool] ?? tool + " login"}`); break;
    case "busy": parts.push("上一次唤醒还没结束"); break;
    default: parts.push(reasonText(r?.message) || (r?.outcome ?? ""));
  }
  return parts.filter(Boolean).join(" · ");
}
