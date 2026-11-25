import { cleanVersion, LOGIN_COMMAND, planTitle, toolFor } from "./format";
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
    return h("div", { class: "panel pad" }, h("span", { class: "row-sub" }, "先在上面打开一个套餐"));
  }
  if (!editing || !enabled.some((p) => p.plan.id === editing)) editing = enabled[0]!.plan.id;
  const ps = enabled.find((p) => p.plan.id === editing)!;
  const p = ps.schedule;

  const pickPlan = enabled.length > 1
    ? segmented(enabled.map((x) => ({ value: x.plan.id, label: planTitle(snap, x) })), ps.plan.id,
      (id) => { editing = id; rerender(); }, "选择套餐")
    : null;

  const editor = scheduleEditor({ times: ps.times ?? [], weekdays: p.weekdays }, (v) =>
    save(ps.plan.id, { ...p, preset: v.preset, times: v.preset === "custom" ? v.times : null, weekdays: v.weekdays }),
  ps.plan.windowMins);

  return h("div", { class: "panel pad stack" }, pickPlan, editor);
}

function planRows(snap: Snapshot, plans: PlanStatus[]): HTMLElement {
  return h("div", { class: "panel" }, plans.map((ps) => {
    const t = toolFor(snap, ps.plan.tool);
    let detail: string;
    if (!snap.detectedOnce) detail = "正在检测…";
    else if (!t?.found) detail = `未找到 ${ps.plan.tool}，安装后点「重新检测」`;
    else if (t.auth && !t.auth.loggedIn) detail = `未登录 · 请运行 ${LOGIN_COMMAND[ps.plan.tool] ?? ps.plan.tool + " login"}`;
    else detail = `自动识别 · ${t.name} ${cleanVersion(t.version)}`.trim();
    const missing = snap.detectedOnce && !t?.found;
    const agents = ps.plan.tool === "codex" && snap.agentsMd
      ? h("span", { class: "row-sub warn-text" },
        `全局 AGENTS.md 有 ${Math.round(snap.agentsMd.bytes / 1024)} KB，每次唤醒多约 ${(snap.agentsMd.tokens / 1000).toFixed(1)}k tokens`)
      : null;
    return h("div", { class: missing ? "row absent" : "row" },
      logo(ps.plan.tool, 20),
      h("div", { class: "row-text" }, h("span", { class: "row-name" }, planTitle(snap, ps)), h("span", { class: "row-sub" }, detail), agents),
      toggle(planTitle(snap, ps), ps.schedule.enabled, (on) => save(ps.plan.id, { ...ps.schedule, enabled: on }), missing && !ps.schedule.enabled));
  }));
}

function option(name: string, detail: string, on: boolean, set: (on: boolean) => Promise<unknown>, disabled = false): HTMLElement {
  return h("div", { class: "row" },
    h("div", { class: "row-text" }, h("span", { class: "row-name" }, name), h("span", { class: "row-sub" }, detail)),
    toggle(name, on, (v) => void attempt(() => set(v)), disabled));
}

export function renderSettings(snap: Snapshot, redraw: () => void, openLog: () => void): HTMLElement {
  rerender = redraw;
  const plans = snap.plans ?? [];
  const st = snap.settings;

  const cutoff = timeField(st?.cutoff ?? "09:00", (v) => void attempt(() => call("SetCutoff", v)), "补发截止时间", "cutoff");

  const notifyDetail = snap.notifyUnavailable
    ? "直接运行程序时系统不允许发通知；从 Kikri.app 启动后可用"
    : "只在未登录或多次唤醒失败时提醒";

  const body = h("div", { class: "body stack scroll" },
    h("section", { class: "stack-xs" },
      h("div", { class: "row-between" }, h("h2", { class: "section-label" }, "套餐"),
        h("button", {
          type: "button", class: "link", disabled: snap.detecting,
          onclick: () => void attempt(() => call("Refresh")),
        }, snap.detecting ? "检测中…" : "重新检测")),
      planRows(snap, plans)),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, "排班"), scheduleCard(snap, plans)),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, "补发"),
      h("div", { class: "panel" }, h("div", { class: "row" },
        h("div", { class: "row-text" },
          h("label", { class: "row-name", for: "cutoff" }, "补发截止时间"),
          h("span", { class: "row-sub" }, "错过的早上排班，开盖联网后在这个时间之前补发；再晚补发会把下午的刷新往后推")),
        cutoff))),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, "选项"),
      h("div", { class: "panel" },
        option("参考你自己的使用", "你 5 小时内用过就不再唤醒；只看会话文件的修改时间，不读内容",
          st?.useActivity ?? true, (v) => call("SetUseActivity", v)),
        option("开机自启", "登录后在菜单栏常驻，到点才能唤醒", snap.autostart, (v) => call("SetAutostart", v)),
        option("通知", notifyDetail, snap.prefs.notify && !snap.notifyUnavailable, (v) => call("SetNotify", v), !!snap.notifyUnavailable))),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, "记录"),
      h("div", { class: "panel" },
        h("button", { type: "button", class: "row nav-row", onclick: openLog },
          h("div", { class: "row-text" },
            h("span", { class: "row-name" }, "唤醒记录"),
            h("span", { class: "row-sub" }, `本月提前唤醒 ${snap.stats.monthWakes} 次 · 跳过 ${snap.stats.monthSkipped} 次`)),
          h("span", { class: "chevron", "aria-hidden": "true" }, "›")))));

  const foot = h("div", { class: "foot row-between" },
    h("span", { class: "foot-note" }, `Kikri ${snap.version}`),
    h("button", { type: "button", class: "btn small", onclick: () => void call("Quit") }, "退出 Kikri"));
  return h("div", { class: "view" }, body, foot);
}
