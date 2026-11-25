import { cleanVersion, LOGIN_COMMAND, planTitle, toolFor, whenLabel } from "./format";
import { L } from "./i18n";
import { logo } from "./logos";
import { scheduleEditor } from "./schedule-ui";
import type { PlanStatus, SchedulePlan, Snapshot } from "./types";
import { attempt, call, h, segmented, timeField, toggle } from "./ui";

let editing: string | null = null; // plan whose schedule is shown
let rerender: () => void = () => {};

function save(planID: string, p: SchedulePlan): void {
  void attempt(() => call("SetPlan", planID, p));
}

function scheduleCard(snap: Snapshot, plans: PlanStatus[]): HTMLElement {
  const enabled = plans.filter((p) => p.schedule.enabled);
  if (enabled.length === 0) {
    return h("div", { class: "panel pad" }, h("span", { class: "row-sub" }, L.turnOnPlanFirst));
  }
  if (!editing || !enabled.some((p) => p.plan.id === editing)) editing = enabled[0]!.plan.id;
  const ps = enabled.find((p) => p.plan.id === editing)!;
  const p = ps.schedule;

  const pickPlan = enabled.length > 1
    ? segmented(enabled.map((x) => ({ value: x.plan.id, label: planTitle(snap, x) })), ps.plan.id,
      (id) => { editing = id; rerender(); }, L.pickPlan)
    : null;

  const editor = scheduleEditor({ times: ps.times ?? [], weekdays: p.weekdays }, (v) =>
    save(ps.plan.id, { ...p, preset: v.preset, times: v.preset === "custom" ? v.times : null, weekdays: v.weekdays }),
  ps.plan.windowMins, snap.settings?.deferSkipped ?? false);

  return h("div", { class: "panel pad stack" }, pickPlan, editor);
}

function planRows(snap: Snapshot, plans: PlanStatus[]): HTMLElement {
  return h("div", { class: "panel" }, plans.map((ps) => {
    const t = toolFor(snap, ps.plan.tool);
    let detail: string;
    if (!snap.detectedOnce) detail = L.detecting;
    else if (!t?.found) detail = L.notFoundRedetect(ps.plan.tool);
    else if (t.auth && !t.auth.loggedIn) detail = L.notLoggedInRun(LOGIN_COMMAND[ps.plan.tool] ?? ps.plan.tool + " login");
    else detail = L.autoDetected(t.name, cleanVersion(t.version));
    const missing = snap.detectedOnce && !t?.found;
    const agents = ps.plan.tool === "codex" && snap.agentsMd
      ? h("span", { class: "row-sub warn-text" },
        L.agentsMd(Math.round(snap.agentsMd.bytes / 1024), (snap.agentsMd.tokens / 1000).toFixed(1)))
      : null;
    return h("div", { class: missing ? "row absent" : "row" },
      logo(ps.plan.tool, 20),
      h("div", { class: "row-text" }, h("span", { class: "row-name" }, planTitle(snap, ps)), h("span", { class: "row-sub" }, detail), agents),
      toggle(planTitle(snap, ps), ps.schedule.enabled, (on) => save(ps.plan.id, { ...ps.schedule, enabled: on }), missing && !ps.schedule.enabled));
  }));
}

function option(name: string, detail: string | Node, on: boolean, set: (on: boolean) => Promise<unknown>, disabled = false): HTMLElement {
  return h("div", { class: "row" },
    h("div", { class: "row-text" }, h("span", { class: "row-name" }, name), typeof detail === "string" ? h("span", { class: "row-sub" }, detail) : detail),
    toggle(name, on, (v) => void attempt(() => set(v)), disabled));
}

export function renderSettings(snap: Snapshot, redraw: () => void, openLog: () => void): HTMLElement {
  rerender = redraw;
  const plans = snap.plans ?? [];
  const st = snap.settings;

  const cutoff = timeField(st?.cutoff ?? "09:00", (v) => void attempt(() => call("SetCutoff", v)), L.cutoff, "cutoff");

  const sl = snap.claudeStatusline;
  const claudeFound = toolFor(snap, "claude")?.found !== false;
  const slDetail = sl?.error
    ? h("span", { class: "row-sub warn-text" }, sl.error)
    : sl?.installed
      ? h("span", { class: "row-sub" }, sl.at ? L.statuslineOn(whenLabel(sl.at)) : L.statuslineWaiting)
      : h("span", { class: "row-sub" }, L.statuslineOff[0], h("code", null, "~/.claude/settings.json"), L.statuslineOff[1]);

  const notifyDetail = snap.notifyUnavailable
    ? L.notifyUnavailable
    : L.notifyDetail;

  const language = segmented(
    [{ value: "", label: L.languageSystem }, { value: "zh", label: "中文" }, { value: "en", label: "English" }],
    snap.prefs.language ?? "", (v) => void attempt(() => call("SetLanguage", v)), L.section.language);

  const body = h("div", { class: "body stack scroll" },
    h("section", { class: "stack-xs" },
      h("div", { class: "row-between" }, h("h2", { class: "section-label" }, L.section.plans),
        h("button", {
          type: "button", class: "link", disabled: snap.detecting,
          onclick: () => void attempt(() => call("Refresh")),
        }, snap.detecting ? L.detectingShort : L.redetect)),
      planRows(snap, plans)),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, L.section.schedule), scheduleCard(snap, plans)),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, L.section.makeup),
      h("div", { class: "panel" }, h("div", { class: "row" },
        h("div", { class: "row-text" },
          h("label", { class: "row-name", for: "cutoff" }, L.cutoff),
          h("span", { class: "row-sub" }, L.cutoffHint)),
        cutoff))),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, L.section.options),
      h("div", { class: "panel" },
        option(L.useActivity, L.useActivityHint,
          st?.useActivity ?? true, (v) => call("SetUseActivity", v)),
        claudeFound && option(L.statusline, slDetail, !!sl?.installed, (v) => call("SetClaudeStatusline", v)),
        option(L.deferSkipped, L.deferSkippedHint,
          st?.deferSkipped ?? false, (v) => call("SetDeferSkipped", v)),
        option(L.autostart, L.autostartHint, snap.autostart, (v) => call("SetAutostart", v)),
        option(L.notify, notifyDetail, snap.prefs.notify && !snap.notifyUnavailable, (v) => call("SetNotify", v), !!snap.notifyUnavailable))),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, L.section.language), language),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, L.section.records),
      h("div", { class: "panel" },
        h("button", { type: "button", class: "row nav-row", onclick: openLog },
          h("div", { class: "row-text" },
            h("span", { class: "row-name" }, L.wakeLog),
            h("span", { class: "row-sub" }, L.monthStats(snap.stats.monthWakes, snap.stats.monthSkipped))),
          h("span", { class: "chevron", "aria-hidden": "true" }, "›")))));

  const foot = h("div", { class: "foot row-between" },
    h("span", { class: "foot-note" }, `Kikri ${snap.version}`),
    h("button", { type: "button", class: "btn small", onclick: () => void call("Quit") }, L.quit));
  return h("div", { class: "view" }, body, foot);
}
