import { cleanVersion, LOGIN_COMMAND, planTitle, toolFor } from "./format";
import { L } from "./i18n";
import { logo } from "./logos";
import { presetFor, scheduleEditor } from "./schedule-ui";
import type { PlanStatus, Snapshot, WakeResult } from "./types";
import { attempt, call, chip, h } from "./ui";


let schedule = { times: ["06:30"], weekdays: [false, true, true, true, true, true, false] };
let startAtLogin = true;
let readStatusline = true; // D24, on by default
let testing = false;
let results: { plan: string; title: string; res?: WakeResult; err?: string }[] = [];
let rerender: () => void = () => {};

function ready(snap: Snapshot, ps: PlanStatus): boolean {
  const t = toolFor(snap, ps.plan.tool);
  return !!t?.found && t.auth?.loggedIn !== false;
}

function toolRow(snap: Snapshot, ps: PlanStatus): HTMLElement {
  const t = toolFor(snap, ps.plan.tool);
  let sub: string;
  let c: HTMLElement;
  if (!snap.detectedOnce) { sub = L.detecting; c = chip(L.checking, "muted"); }
  else if (!t?.found) { sub = L.notFound(ps.plan.tool); c = chip(L.notInstalled, "muted"); }
  else if (t.auth && !t.auth.loggedIn) {
    sub = L.runFirst(LOGIN_COMMAND[ps.plan.tool] ?? ps.plan.tool + " login");
    c = chip(L.notLoggedIn, "bad");
  } else { sub = `${t.name} ${cleanVersion(t.version)}`.trim(); c = chip(L.loggedIn, "ok"); }
  return h("div", { class: t?.found || !snap.detectedOnce ? "row" : "row absent" },
    logo(ps.plan.tool, 20),
    h("div", { class: "row-text" }, h("span", { class: "row-name" }, planTitle(snap, ps)), h("span", { class: "row-sub" }, sub)), c);
}

async function runTest(snap: Snapshot): Promise<void> {
  const plans = (snap.plans ?? []).filter((p) => ready(snap, p));
  testing = true;
  results = [];
  rerender();
  const out = await Promise.allSettled(plans.map((p) => call<WakeResult>("WakeNow", p.plan.id)));
  results = out.map((r, i) => {
    const p = plans[i]!;
    const title = planTitle(snap, p);
    return r.status === "fulfilled"
      ? { plan: p.plan.id, title, res: r.value }
      : { plan: p.plan.id, title, err: r.reason instanceof Error ? r.reason.message : String(r.reason) };
  });
  testing = false;
  rerender();
}

function resultLine(r: (typeof results)[number]): HTMLElement {
  const res = r.res;
  if (res?.outcome === "ok") {
    const parts = [r.title];
    if (res.durationMs) parts.push(`${(res.durationMs / 1000).toFixed(1)}s`);
    if (res.usage) parts.push(`${res.usage.inputTokens.toLocaleString("en-US")} tokens`);
    return h("div", { class: "test-line" }, h("span", { class: "ok-text" }, L.testOk), h("span", { class: "row-sub" }, parts.join(" · ")));
  }
  return h("div", { class: "test-line" }, h("span", { class: "bad-text" }, L.testFailed(r.title)),
    h("span", { class: "row-sub" }, r.err ?? res?.message ?? res?.outcome ?? ""));
}

async function finish(snap: Snapshot): Promise<void> {
  for (const p of snap.plans ?? []) {
    if (ready(snap, p)) {
      const preset = presetFor(schedule.times);
      await call("SetPlan", p.plan.id, {
        ...p.schedule, enabled: true, preset, times: preset === "custom" ? schedule.times : null, weekdays: schedule.weekdays,
      });
    }
  }
  await call("FinishOnboarding", startAtLogin, readStatusline && toolFor(snap, "claude")?.found !== false);
}

export function renderOnboarding(snap: Snapshot, redraw: () => void): HTMLElement {
  rerender = redraw;
  const plans = snap.plans ?? [];
  const anyReady = plans.some((p) => ready(snap, p));
  const test = h("div", { class: "panel pad stack-xs" },
    results.length ? results.map(resultLine) : h("span", { class: "row-sub" }, L.testHint),
    h("button", {
      type: "button", class: "btn small", disabled: testing || !anyReady || !snap.detectedOnce,
      onclick: () => void attempt(() => runTest(snap)),
    }, testing ? L.testing : results.length ? L.testAgain : L.test));

  return h("div", { class: "view onboarding" },
    h("div", { class: "body stack scroll" },
      h("div", { class: "stack-xs" },
        h("div", { class: "brand" }, h("img", { src: "/chick.svg", alt: "", width: "26", height: "26" }), h("h1", null, L.welcome)),
        h("p", { class: "lead" }, L.lead)),
      h("section", { class: "stack-xs" }, h("span", { class: "section-label" }, L.found),
        h("div", { class: "panel" }, plans.map((p) => toolRow(snap, p)))),
      h("section", { class: "stack-xs" }, h("span", { class: "section-label" }, L.when),
        h("div", { class: "panel pad" }, scheduleEditor(schedule, (v) => { schedule = { times: v.times, weekdays: v.weekdays }; rerender(); }))),
      test,
      h("label", { class: "check" },
        h("input", { type: "checkbox", checked: startAtLogin, onchange: (e: Event) => { startAtLogin = (e.target as HTMLInputElement).checked; } }),
        L.startAtLogin),
      toolFor(snap, "claude")?.found !== false && h("label", { class: "check" },
        h("input", { type: "checkbox", checked: readStatusline, onchange: (e: Event) => { readStatusline = (e.target as HTMLInputElement).checked; } }),
        h("span", { class: "stack-xs" }, L.statuslineCheck,
          h("span", { class: "row-sub" }, L.statuslineCheckHint)))),
    h("div", { class: "foot stack-sm" },
      h("button", {
        type: "button", class: "btn primary", disabled: !snap.detectedOnce,
        onclick: () => void attempt(() => finish(snap)),
      }, anyReady || !snap.detectedOnce ? L.start : L.skipForNow),
      h("span", { class: "foot-note" }, L.privacy)));
}
