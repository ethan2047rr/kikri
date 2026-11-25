// Mirrors the JSON produced by internal/app.Snapshot and the packages it uses.

export type Source = "override" | "login-shell" | "process-path" | "fallback";

export interface AuthInfo {
  loggedIn: boolean;
  method?: string;
  plan?: string;
  expiresAt?: string;
  detail?: string;
}

export interface ToolStatus {
  id: string;
  name: string;
  found: boolean;
  path?: string;
  source?: Source;
  version?: string;
  auth?: AuthInfo;
  error?: string;
}

export interface SearchDir {
  dir: string;
  source: Source;
}

export interface Report {
  checkedAt: string;
  searchPath: { dirs: SearchDir[] | null; loginShellError?: string; proxy?: string };
  tools: ToolStatus[] | null;
}

export interface Layout {
  root: string;
  sandbox: string;
  codexInstructions: string;
  database: string;
}

export type Preset = "early-bird" | "three" | "custom";

/** internal/schedule.Plan. Weekdays are indexed Sunday = 0. */
export interface SchedulePlan {
  enabled: boolean;
  preset: Preset;
  times: string[] | null;
  weekdays: boolean[];
  since?: string;
}

export interface PlanDef {
  id: string;
  name: string;
  tool: string;
  windowMins: number;
}

export type Outcome =
  | "ok" | "quota_exhausted" | "not_logged_in" | "network_error" | "timeout"
  | "offline" | "skipped" | "busy" | "failed" | "expired" | "paused";

export type Trigger = "scheduled" | "makeup" | "retry" | "manual";

export interface Entry {
  id: number;
  plan: string;
  slotAt?: string;
  trigger: Trigger;
  startedAt: string;
  outcome: Outcome;
  result?: WakeResult;
}

export interface WakeWindow {
  kind: string;
  utilization?: number;
  resetsAt: string;
  status?: string;
}

export interface WakeResult {
  tool: string;
  outcome: Outcome;
  message?: string;
  reply?: string;
  startedAt: string;
  durationMs?: number;
  usage?: { inputTokens: number; cachedInputTokens?: number; outputTokens: number };
  windows?: WakeWindow[];
}

export interface PlanStatus {
  plan: PlanDef;
  schedule: SchedulePlan;
  times: string[] | null;
  next?: string;
  last?: Entry;
  paused: boolean;
}

export interface SchedulerSettings {
  plans: Record<string, SchedulePlan>;
  cutoff: string;
  useActivity: boolean;
  pausedUntil?: string;
}

export interface QuotaWindow {
  usedPercent?: number;
  resetsAt?: string;
  active: boolean;
}

export interface PlanQuota {
  source?: "live" | "cached";
  at?: string;
  fiveHour?: QuotaWindow;
  weekly?: QuotaWindow;
  limitReached?: boolean;
  planType?: string;
  error?: string;
  lastUsed?: string;
}

export interface Snapshot {
  version: string;
  appDir: Layout;
  appDirError?: string;
  detecting: boolean;
  detectedOnce: boolean;
  report: Report;

  plans?: PlanStatus[];
  settings?: SchedulerSettings;
  storeError?: string;

  prefs: { onboarded: boolean; notify: boolean };
  autostart: boolean;
  notifyUnavailable?: string;
  quota: Record<string, PlanQuota> | null;
  stats: { monthWakes: number; monthSkipped: number };
  agentsMd?: { path: string; bytes: number; tokens: number };
  waking: Record<string, boolean> | null;
  logVersion: number;
}
