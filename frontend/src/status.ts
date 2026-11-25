import { countdown, hhmm, isValidTime, LOGIN_COMMAND, planTitle, reasonText, toolFor, whenLabel } from "./format";
import { L } from "./i18n";
import { logo } from "./logos";
import type { PlanQuota, PlanStatus, QuotaWindow, Snapshot } from "./types";
import { attempt, call, chip, h, toast } from "./ui";

// A card answers three questions, in this order of weight:
//   1. When does my quota refresh?             (the big number)
//   2. How much of it have I used?              (the ring)
//   3. Did the wake work, and when is the next? (one quiet line)
// Everything else (tokens, duration, AGENTS.md size) lives in the log or
// settings. A chip only appears when something needs the user.

function isToday(t: string): boolean {
  return new Date(t).toDateString() === new Date().toDateString();
}

/** A problem the user has to act on, shown as a red chip. */
function problem(snap: Snapshot, ps: PlanStatus, q: PlanQuota | undefined): string | null {
  const tool = toolFor(snap, ps.plan.tool);
  if (tool && snap.detectedOnce && !tool.found) return L.chip.notInstalled;
  if (tool?.auth && !tool.auth.loggedIn) return L.chip.notLoggedIn;
  if (q?.limitReached) return L.chip.limit;
  const last = ps.last;
  if (last && isToday(last.startedAt) && ["failed", "quota_exhausted", "not_logged_in", "expired"].includes(last.outcome)) {
    return L.chip.failed;
  }
  return null;
}

/**
 * The refresh time of a window the user opened themselves, bounded without
 * any request: it started after the last known reset and no later than the
 * last session activity, so it refreshes between those plus the window.
 */
function estimate(prev: QuotaWindow | undefined, lastUsed: string, windowMs: number): string | null {
  if (!prev?.resetsAt || !isValidTime(prev.resetsAt)) return null;
  const reset = Date.parse(prev.resetsAt);
  const used = Date.parse(lastUsed);
  if (!(used > reset && used - reset < windowMs)) return null;
  const lo = new Date(reset + windowMs);
  const hi = new Date(used + windowMs);
  return hi.getTime() - lo.getTime() < 3 * 60_000 ? L.estimateOne(hhmm(lo)) : L.estimateRange(hhmm(lo), hhmm(hi));
}

/** The headline: refresh time if known, else "in use", else the next wake. */
function headline(ps: PlanStatus, q: PlanQuota | undefined): { big: string; sub: string; showsNext: boolean } {
  const five = q?.fiveHour;
  if (five?.active && isValidTime(five.resetsAt)) {
    return { big: hhmm(five.resetsAt), sub: L.resetsIn(countdown(five.resetsAt)), showsNext: false };
  }
  const windowMs = ps.plan.windowMins * 60_000;
  if (q?.lastUsed && Date.now() - Date.parse(q.lastUsed) < windowMs) {
    const est = estimate(five, q.lastUsed, windowMs);
    return { big: L.inUse, sub: est ?? L.usedUnknown(hhmm(q.lastUsed)), showsNext: false };
  }
  if (ps.deferred) {
    return { big: hhmm(ps.deferred), sub: L.deferredWake(countdown(ps.deferred)), showsNext: true };
  }
  if (ps.next) {
    const day = whenLabel(ps.next).replace(/\s?\d\d:\d\d$/, "");
    return { big: hhmm(ps.next), sub: L.wakesOn(day, countdown(ps.next)), showsNext: true };
  }
  return { big: "—", sub: L.noSchedule, showsNext: false };
}

/** The quiet bottom line: what happened last, then what happens next. */
function footLine(snap: Snapshot, ps: PlanStatus, showsNext: boolean): { icon: string; tone: string; text: (Node | string)[]; title?: string } {
  const tool = toolFor(snap, ps.plan.tool);
  const next = ps.next && !showsNext ? L.next(whenLabel(ps.next)) : "";
  const join = (a: string) => [a, next].filter(Boolean).join(" · ");

  if (tool && snap.detectedOnce && !tool.found) {
    return { icon: "!", tone: "bad", text: [L.toolMissing(ps.plan.tool)] };
  }
  if (tool?.auth && !tool.auth.loggedIn) {
    return { icon: "!", tone: "bad", text: [L.loginRun[0], h("code", null, LOGIN_COMMAND[ps.plan.tool] ?? `${ps.plan.tool} login`), L.loginRun[1]] };
  }
  if (snap.waking?.[ps.plan.id]) return { icon: "…", tone: "muted", text: [L.waking] };
  if (ps.paused) return { icon: "‖", tone: "muted", text: [join(L.pausedToday)] };

  const last = ps.last;
  if (!last) return { icon: "·", tone: "muted", text: [next || L.noWakesYet] };
  const when = isToday(last.startedAt) ? hhmm(last.slotAt ?? last.startedAt) : whenLabel(last.slotAt ?? last.startedAt);
  switch (last.outcome) {
    case "ok": return { icon: "✓", tone: "ok", text: [join(L.woke(when))] };
    case "skipped":
      if (ps.deferred) {
        return {
          icon: "–", tone: "muted", title: reasonText(last.result?.message),
          text: [L.skippedDeferred(when, hhmm(ps.deferred))],
        };
      }
      // Short here; the full reason is in the tooltip and the log.
      return {
        icon: "–", tone: "muted", title: reasonText(last.result?.message),
        text: [join(last.result?.message?.startsWith("this plan has no") ? L.skippedNoWindow(when) : L.skippedRunning(when))],
      };
    case "offline": case "network_error": case "timeout": case "busy":
      return { icon: "…", tone: "warn", text: [join(L.waitingNetwork(when))] };
    case "paused": return { icon: "‖", tone: "muted", text: [join(L.pausedAt(when))] };
    default: return { icon: "!", tone: "bad", text: [join(L.failedSeeLog(when))] };
  }
}

/** A ring showing how much of the 5-hour window is used. */
function ring(five: QuotaWindow | undefined, weekly: QuotaWindow | undefined): HTMLElement | null {
  if (five?.usedPercent === undefined) return null;
  const v = Math.max(0, Math.min(100, five.usedPercent));
  const r = 19;
  const c = 2 * Math.PI * r;
  const box = h("div", { class: "ring", role: "img", "aria-label": L.ringLabel(Math.round(v)) });
  // Generated from numbers only.
  box.innerHTML = `<svg viewBox="0 0 48 48" width="48" height="48" aria-hidden="true">
    <circle cx="24" cy="24" r="${r}" class="ring-track"/>
    <circle cx="24" cy="24" r="${r}" class="ring-fill${v >= 90 ? " hot" : ""}"
      stroke-dasharray="${((c * v) / 100).toFixed(2)} ${c.toFixed(2)}" transform="rotate(-90 24 24)"/></svg>`;
  box.append(h("span", { class: "ring-num" }, `${Math.round(v)}%`));
  const week = weekly?.usedPercent !== undefined ? h("span", { class: "ring-cap" }, L.week(Math.round(weekly.usedPercent))) : null;
  return h("div", { class: "usage", title: L.ringTitle }, box, week);
}

function card(snap: Snapshot, ps: PlanStatus): HTMLElement {
  const q = snap.quota?.[ps.plan.id];
  const bad = problem(snap, ps, q);
  const head = headline(ps, q);
  const foot = footLine(snap, ps, head.showsNext);
  return h("section", { class: "card" },
    h("div", { class: "card-head" },
      h("div", { class: "card-title" }, logo(ps.plan.tool, 20), h("h2", null, planTitle(snap, ps))),
      bad && chip(bad, "bad")),
    h("div", { class: "card-main" },
      h("div", { class: "when" },
        h("span", { class: head.big.length > 5 ? "big word" : "big" }, head.big),
        h("span", { class: "when-sub" }, head.sub)),
      ring(q?.fiveHour, q?.weekly)),
    h("div", { class: `card-foot ${foot.tone}`, title: foot.title },
      h("span", { class: "foot-icon", "aria-hidden": "true" }, foot.icon),
      h("span", { class: "foot-text" }, ...foot.text)));
}

export function renderStatus(snap: Snapshot, goSettings: () => void): HTMLElement {
  const plans = (snap.plans ?? []).filter((p) => p.schedule.enabled);
  const body = h("div", { class: "body stack-sm scroll" });

  if (snap.storeError || snap.appDirError) {
    body.append(h("section", { class: "card" }, h("h2", null, L.cantSchedule),
      h("span", { class: "detail" }, snap.storeError ?? snap.appDirError ?? "")));
  } else if (plans.length === 0) {
    body.append(h("section", { class: "card empty" },
      h("h2", null, L.noPlans),
      h("span", { class: "detail" }, L.noPlansHint),
      h("button", { type: "button", class: "btn", onclick: goSettings }, L.openSettings)));
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
    const results = await Promise.allSettled(wakeable.map((p) => call<{ outcome: string }>("WakeNow", p.plan.id)));
    const failed = results.filter((r) => r.status === "rejected" || r.value.outcome !== "ok").length;
    if (failed) toast(L.wakeFailedToast(failed), true);
    else toast(L.wokeToast);
  };

  const foot = h("div", { class: "foot grid2" },
    h("button", {
      type: "button", class: "btn primary", disabled: anyWaking || wakeable.length === 0,
      title: L.wakeNowTitle,
      onclick: () => void attempt(wakeAll),
    }, anyWaking ? L.wakingButton : L.wakeNow),
    h("button", {
      type: "button", class: "btn", disabled: plans.length === 0,
      onclick: () => void attempt(() => call("PauseToday", !paused)),
    }, paused ? L.resumeToday : L.pauseToday));

  return h("div", { class: "view" }, body, foot);
}
