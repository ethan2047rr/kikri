import { L } from "./i18n";
import type { Entry, Outcome, PlanStatus, Preset, SchedulePlan, Snapshot, ToolStatus } from "./types";

/** Monday-first display order of the Sunday-indexed weekdays array. */
export const DAY_ORDER = [1, 2, 3, 4, 5, 6, 0];

export function presetLabel(p: Preset): string {
  return L.preset[p] ?? p;
}

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

const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** "今天 · 周三", "昨天 · 周二", "11月23日 · 周一" ("Today · Wed", "Nov 23 · Mon"). */
export function dayLabel(d: Date, now = new Date()): string {
  const diff = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
  const wd = L.weekday[d.getDay()];
  const day = diff === 0 ? L.today : diff === 1 ? L.yesterday : diff === -1 ? L.tomorrow : L.monthDay(d);
  return `${cap(day)} · ${wd}`;
}

/** hhmm, prefixed with the day when it is not today ("明天 06:30"). */
export function whenLabel(t: string, now = new Date()): string {
  const d = new Date(t);
  const diff = Math.round((startOfDay(d) - startOfDay(now)) / 86_400_000);
  if (diff === 0) return hhmm(d);
  if (diff === 1) return L.dayAt(L.tomorrow, hhmm(d));
  return L.dayAt(L.weekday[d.getDay()]!, hhmm(d));
}

/** "2 小时 10 分", "8 分钟", "不到 1 分钟". */
export function countdown(to: string, now = Date.now()): string {
  const mins = Math.floor((Date.parse(to) - now) / 60_000);
  if (mins < 1) return L.lessThanMinute;
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  if (h === 0) return L.minutes(m);
  if (h >= 24) return L.daysHours(Math.floor(h / 24), h % 24);
  return m === 0 ? L.hours(h) : L.hoursMinutes(h, m);
}

export function daysLabel(w: boolean[]): string {
  const on = DAY_ORDER.filter((i) => w[i]);
  if (on.length === 7) return L.everyday;
  if (on.length === 0) return L.never;
  if (on.length === 5 && on.every((i) => i >= 1 && i <= 5)) return L.workdays;
  if (on.length === 2 && w[0] && w[6]) return L.weekend;
  return L.daysList(on.map((i) => L.dayShort[i]!));
}

export function scheduleSummary(p: SchedulePlan, times: string[] | null): string {
  return `${presetLabel(p.preset)} · ${daysLabel(p.weekdays)} ${(times ?? []).join(L.listSep)}`.trim();
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
      if (e.trigger === "makeup") return { label: L.outcome.makeupOk, tone: "ok" };
      if (e.trigger === "retry") return { label: L.outcome.retryOk, tone: "ok" };
      if (e.trigger === "deferred") return { label: L.outcome.deferredOk, tone: "ok" };
      return { label: L.outcome.ok, tone: "ok" };
    case "skipped": return { label: L.outcome.skipped, tone: "warn" };
    case "offline": return { label: L.outcome.offline, tone: "warn" };
    case "network_error": return { label: L.outcome.network_error, tone: "warn" };
    case "timeout": return { label: L.outcome.timeout, tone: "warn" };
    case "busy": return { label: L.outcome.busy, tone: "warn" };
    case "not_logged_in": return { label: L.outcome.not_logged_in, tone: "bad" };
    case "quota_exhausted": return { label: L.outcome.quota_exhausted, tone: "bad" };
    case "expired": return { label: L.outcome.expired, tone: "bad" };
    case "paused": return { label: L.outcome.paused, tone: "muted" };
    default: return { label: L.outcome.failed, tone: "bad" };
  }
}

/** Translates the reasons Kikri itself writes; CLI errors stay as they are. */
export function reasonText(msg: string | undefined): string {
  if (!msg) return "";
  const deferred = msg.match(/^(.*); deferred to (\d\d:\d\d)$/);
  if (deferred) return L.reason.deferred(reasonText(deferred[1]), deferred[2]!);
  const r = L.reason;
  const rules: [RegExp, (m: RegExpMatchArray) => string][] = [
    [/^a window is already running; it resets at (\d\d:\d\d)$/, (m) => r.running(m[1]!)],
    [/^the window opened by the (\d\d:\d\d) wake runs until (\d\d:\d\d)$/, (m) => r.kikriWindow(m[1]!, m[2]!)],
    [/^Kikri already woke it at (\d\d:\d\d)$/, (m) => r.kikriWoke(m[1]!)],
    [/^you used it at (\d\d:\d\d), so a window is already running$/, (m) => r.youUsed(m[1]!)],
    [/^this plan has no 5-hour window \((.*)\)$/, (m) => r.noWindow(m[1]!)],
    [/^paused for today$/, () => r.pausedToday],
    [/^still failing \(no network\?\) when the make-up deadline (\d\d:\d\d) passed$/, (m) => r.offlineExpired(m[1]!)],
    [/^deferred wake missed .* when the make-up deadline (\d\d:\d\d) passed$/, (m) => r.deferredMissed(m[1]!)],
    [/^(.*) when the make-up deadline (\d\d:\d\d) passed$/, (m) => r.missed(m[2]!)],
  ];
  for (const [re, f] of rules) {
    const m = msg.match(re);
    if (m) return f(m);
  }
  return msg;
}

/** "3.6s · 633 tokens · 11:30 刷新" ("... · resets 11:30"). */
export function resultDetail(e: Entry, withReset = true): string {
  const r = e.result;
  const parts: string[] = [];
  if (e.trigger === "makeup") parts.push(L.detail.makeup);
  if (e.trigger === "manual") parts.push(L.detail.manual);
  if (e.trigger === "deferred") parts.push(L.detail.deferred);
  if (e.outcome === "ok" && r) {
    if (r.durationMs) parts.push(`${(r.durationMs / 1000).toFixed(1)}s`);
    if (r.usage) parts.push(`${r.usage.inputTokens.toLocaleString("en-US")} tokens`);
    const w = r.windows?.find((x) => x.kind === "five_hour");
    if (withReset && w && isValidTime(w.resetsAt)) parts.push(L.detail.resets(hhmm(w.resetsAt)));
    return parts.join(" · ");
  }
  const tool = r?.tool ?? "";
  switch (e.outcome) {
    case "offline": parts.push(L.detail.offline); break;
    case "not_logged_in": parts.push(L.detail.runLogin(LOGIN_COMMAND[tool] ?? tool + " login")); break;
    case "busy": parts.push(L.detail.busy); break;
    default: parts.push(reasonText(r?.message) || (r?.outcome ?? ""));
  }
  return parts.filter(Boolean).join(" · ");
}
