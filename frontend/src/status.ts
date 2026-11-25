import {
  countdown, hhmm, isValidTime, LOGIN_COMMAND, planTitle, reasonText, resultDetail, toolFor, whenLabel,
} from "./format";
import type { PlanQuota, PlanStatus, QuotaWindow, Snapshot } from "./types";
import { attempt, call, chip, h, toast } from "./ui";

interface CardState {
  chip: { label: string; tone: string };
  big: string;
  lead: (Node | string)[];
  note?: (Node | string)[];
}

function bold(s: string): HTMLElement {
  return h("b", null, s);
}

function isToday(t: string): boolean {
  return new Date(t).toDateString() === new Date().toDateString();
}

/** The card's headline: the refresh time when it is known, otherwise the next wake. */
function cardState(snap: Snapshot, ps: PlanStatus, q: PlanQuota | undefined): CardState {
  const tool = toolFor(snap, ps.plan.tool);
  const now = Date.now();
  const five = q?.fiveHour;
  const windowMs = ps.plan.windowMins * 60_000;
  const lastUsed = q?.lastUsed && now - Date.parse(q.lastUsed) < windowMs ? q.lastUsed : undefined;
  const last = ps.last;
  const nextLead: (Node | string)[] = ps.next ? ["下次唤醒 ", bold(whenLabel(ps.next))] : ["没有排班"];

  // Chip: what happened today, or what is blocking the plan.
  let c: CardState["chip"];
  if (snap.waking?.[ps.plan.id]) c = { label: "唤醒中…", tone: "muted" };
  else if (tool && !tool.found) c = { label: "未安装", tone: "bad" };
  else if (tool?.auth && !tool.auth.loggedIn) c = { label: "未登录", tone: "bad" };
  else if (q?.limitReached) c = { label: "额度用完", tone: "bad" };
  else if (ps.paused) c = { label: "今天已暂停", tone: "muted" };
  else if (last && isToday(last.startedAt)) {
    const at = hhmm(last.slotAt ?? last.startedAt);
    switch (last.outcome) {
      case "ok": c = { label: "今天已唤醒", tone: "ok" }; break;
      case "skipped": c = { label: `${at} 已跳过`, tone: "warn" }; break;
      case "offline": case "network_error": case "timeout": case "busy":
        c = { label: "等待网络", tone: "warn" }; break;
      case "paused": c = { label: "今天已暂停", tone: "muted" }; break;
      case "not_logged_in": c = { label: "未登录", tone: "bad" }; break;
      default: c = { label: "唤醒失败", tone: "bad" };
    }
  } else if (ps.next) c = { label: `等待 ${whenLabel(ps.next)}`, tone: "muted" };
  else c = { label: "没有排班", tone: "muted" };

  if (five?.active && isValidTime(five.resetsAt)) {
    return { chip: c, big: hhmm(five.resetsAt), lead: ["刷新 · 还有 ", bold(countdown(five.resetsAt))] };
  }
  if (lastUsed) {
    return { chip: c, big: "活跃中", lead: [`你 ${hhmm(lastUsed)} 用过，刷新时间未知`] };
  }
  if (ps.next) {
    return { chip: c, big: hhmm(ps.next), lead: ["唤醒 · 还有 ", bold(countdown(ps.next))] };
  }
  return { chip: c, big: "—", lead: nextLead };
}

/** One muted line about the last wake, or what the user has to do. */
function detailLine(snap: Snapshot, ps: PlanStatus): (Node | string)[] | null {
  const tool = toolFor(snap, ps.plan.tool);
  if (tool && !tool.found) return [`没找到 ${ps.plan.tool}，安装后点设置里的「重新检测」`];
  if (tool?.auth && !tool.auth.loggedIn) {
    return ["请在终端运行 ", h("code", null, LOGIN_COMMAND[ps.plan.tool] ?? `${ps.plan.tool} login`), " 登录"];
  }
  const last = ps.last;
  if (!last) return ps.next ? null : ["在设置里选择唤醒时间"];
  const at = hhmm(last.slotAt ?? last.startedAt);
  const prefix = isToday(last.startedAt) ? at : whenLabel(last.slotAt ?? last.startedAt);
  if (last.outcome === "ok") return [`${prefix} 唤醒成功 · ${resultDetail(last, false)}`];
  if (last.outcome === "skipped") {
    const reason = reasonText(last.result?.message);
    return ps.next ? [`${reason}；下次 `, bold(whenLabel(ps.next)), " 再唤醒"] : [reason];
  }
  return [`${prefix} ${resultDetail(last)}`];
}

function bar(label: string, w: QuotaWindow | undefined): HTMLElement | null {
  if (w?.usedPercent === undefined) return null;
  const v = Math.max(0, Math.min(100, w.usedPercent));
  return h("div", { class: "meter" },
    h("div", { class: "meter-head" }, h("span", null, label), h("span", { class: "num" }, `${Math.round(v)}%`)),
    h("div", { class: "track", role: "meter", "aria-label": `${label}已用`, "aria-valuenow": String(Math.round(v)), "aria-valuemin": "0", "aria-valuemax": "100" },
      h("div", { class: v >= 90 ? "fill hot" : "fill", style: `width:${v}%` })));
}

function card(snap: Snapshot, ps: PlanStatus): HTMLElement {
  const q = snap.quota?.[ps.plan.id];
  const st = cardState(snap, ps, q);
  const detail = detailLine(snap, ps);
  const meters = [bar("5 小时", q?.fiveHour), bar("本周", q?.weekly)].filter(Boolean) as HTMLElement[];
  // Claude's numbers come from its last wake; say so unless the line above
  // already describes that wake.
  const sameWake = ps.last?.outcome === "ok" && q?.at && Date.parse(ps.last.startedAt) === Date.parse(q.at);
  const source = q?.source === "cached" && meters.length && q.at && !sameWake ? `用量来自 ${whenLabel(q.at)} 的唤醒` : null;
  const agents = ps.plan.tool === "codex" && snap.agentsMd
    ? h("div", { class: "note warn" },
      `全局 AGENTS.md 有 ${Math.round(snap.agentsMd.bytes / 1024)} KB，每次唤醒约多 ${(snap.agentsMd.tokens / 1000).toFixed(1)}k tokens`)
    : null;
  return h("section", { class: "card" },
    h("div", { class: "card-head" }, h("h2", null, planTitle(snap, ps)), chip(st.chip.label, st.chip.tone)),
    h("div", { class: "headline" }, h("span", { class: st.big.length > 5 ? "big word" : "big" }, st.big), h("span", { class: "lead" }, ...st.lead)),
    detail && h("span", { class: "detail" }, ...detail),
    meters.length > 0 && h("div", { class: "meters" }, meters),
    source && h("span", { class: "detail faint" }, source),
    agents);
}

export function renderStatus(snap: Snapshot, goSettings: () => void): HTMLElement {
  const plans = (snap.plans ?? []).filter((p) => p.schedule.enabled);
  const view = h("div", { class: "view" });
  const body = h("div", { class: "body stack-sm scroll" });

  if (snap.storeError || snap.appDirError) {
    body.append(h("section", { class: "card" }, h("h2", null, "无法启动调度"),
      h("span", { class: "detail" }, snap.storeError ?? snap.appDirError ?? "")));
  } else if (plans.length === 0) {
    body.append(h("section", { class: "card empty" },
      h("h2", null, "还没有开启任何套餐"),
      h("span", { class: "detail" }, "在设置里打开 Claude 或 ChatGPT，并选一个唤醒时间。"),
      h("button", { type: "button", class: "btn", onclick: goSettings }, "去设置")));
  } else {
    body.append(...plans.map((p) => card(snap, p)));
  }

  const paused = (snap.plans ?? []).some((p) => p.paused);
  const anyWaking = Object.keys(snap.waking ?? {}).length > 0;
  const wakeable = plans.filter((p) => {
    const t = toolFor(snap, p.plan.tool);
    return !t || (t.found && t.auth?.loggedIn !== false);
  });

  const wakeAll = async () => {
    const results = await Promise.allSettled(wakeable.map((p) => call<{ outcome: string; message?: string }>("WakeNow", p.plan.id)));
    const failed = results.filter((r) => r.status === "rejected" || r.value.outcome !== "ok").length;
    if (failed) toast(`${failed} 个套餐唤醒失败，详情见日志`, true);
    else toast("已唤醒");
  };

  const foot = h("div", { class: "foot stack-sm" },
    h("div", { class: "grid2" },
      h("button", {
        type: "button", class: "btn primary", disabled: anyWaking || wakeable.length === 0,
        title: "马上发一条唤醒请求，开启 5 小时窗口（不检查窗口是否已在跑）",
        onclick: () => void attempt(wakeAll),
      }, anyWaking ? "唤醒中…" : "立即唤醒"),
      h("button", {
        type: "button", class: "btn", disabled: plans.length === 0,
        onclick: () => void attempt(() => call("PauseToday", !paused)),
      }, paused ? "恢复今日" : "暂停今日")),
    h("span", { class: "foot-note" }, `本月已提前唤醒 ${snap.stats.monthWakes} 次`));

  view.append(body, foot);
  return view;
}
