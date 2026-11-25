import { cleanVersion, DAY_ORDER, DAY_SHORT, LOGIN_COMMAND, PRESET_LABEL, planTitle, toolFor } from "./format";
import type { PlanStatus, Preset, SchedulePlan, Snapshot } from "./types";
import { attempt, call, h, segmented, toggle } from "./ui";

let editing: string | null = null; // plan whose schedule is shown
let adding = false; // the "add time" input is open

function save(planID: string, p: SchedulePlan): void {
  void attempt(() => call("SetPlan", planID, p));
}

function sortedTimes(ts: string[]): string[] {
  return [...new Set(ts)].sort();
}

function scheduleCard(snap: Snapshot, plans: PlanStatus[]): HTMLElement {
  if (!editing || !plans.some((p) => p.plan.id === editing)) {
    editing = (plans.find((p) => p.schedule.enabled) ?? plans[0])?.plan.id ?? null;
  }
  const ps = plans.find((p) => p.plan.id === editing);
  if (!ps) return h("div", { class: "panel pad" }, "没有可排班的套餐");
  const p = ps.schedule;
  const times = ps.times ?? [];
  const custom = p.preset === "custom";

  const pickPlan = plans.length > 1
    ? segmented(plans.map((x) => ({ value: x.plan.id, label: planTitle(snap, x) })), ps.plan.id,
      (id) => { editing = id; adding = false; rerender(); }, "选择套餐")
    : null;

  const presets = segmented<Preset>(
    (["early-bird", "three", "custom"] as Preset[]).map((v) => ({ value: v, label: PRESET_LABEL[v] })),
    p.preset,
    (v) => save(ps.plan.id, { ...p, preset: v, times: v === "custom" ? times : null }),
    "排班预设");

  const chips = h("div", { class: "times" },
    times.map((t) => h("span", { class: "time-chip num" }, t,
      custom && times.length > 1 && h("button", {
        type: "button", class: "x", "aria-label": `删除 ${t}`,
        onclick: () => save(ps.plan.id, { ...p, times: times.filter((x) => x !== t) }),
      }, "×"))));
  if (custom) {
    if (adding) {
      const input = h("input", { type: "time", class: "time-input", value: "11:35", "aria-label": "新的唤醒时间" });
      const commit = () => {
        adding = false;
        if (input.value) save(ps.plan.id, { ...p, times: sortedTimes([...times, input.value]) });
        else rerender();
      };
      input.addEventListener("keydown", (e) => {
        if (e.key === "Enter") commit();
        if (e.key === "Escape") { adding = false; rerender(); }
      });
      chips.append(input, h("button", { type: "button", class: "btn small", onclick: commit }, "添加"));
      queueMicrotask(() => input.focus());
    } else {
      chips.append(h("button", { type: "button", class: "add", onclick: () => { adding = true; rerender(); } }, "＋ 添加时间"));
    }
  }
  const hint = p.preset === "three"
    ? "11:05 那次只在没有窗口在跑时才发"
    : p.preset === "early-bird" ? "06:30 唤醒，11:30 就能用上第 2 轮额度" : null;

  const days = h("div", { class: "days" }, DAY_ORDER.map((i) => {
    const on = !!p.weekdays[i];
    return h("button", {
      type: "button", class: on ? "day-btn on" : "day-btn", "aria-pressed": String(on),
      "aria-label": `周${DAY_SHORT[i]}`,
      onclick: () => {
        const w = [...p.weekdays];
        w[i] = !on;
        save(ps.plan.id, { ...p, weekdays: w });
      },
    }, DAY_SHORT[i]);
  }));

  return h("div", { class: "panel pad stack-sm" },
    pickPlan, presets, chips,
    hint && h("span", { class: "row-sub" }, hint),
    h("div", { class: "stack-xs" }, h("span", { class: "row-sub ink-soft" }, "哪几天唤醒"), days),
    !p.enabled && h("span", { class: "row-sub" }, "这个套餐还没打开，在下面「套餐」里打开后才会唤醒"));
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
    return h("div", { class: missing ? "row absent" : "row" },
      h("div", { class: "row-text" }, h("span", { class: "row-name" }, planTitle(snap, ps)), h("span", { class: "row-sub" }, detail)),
      toggle(planTitle(snap, ps), ps.schedule.enabled, (on) => save(ps.plan.id, { ...ps.schedule, enabled: on }), missing && !ps.schedule.enabled));
  }));
}

function option(name: string, detail: string, on: boolean, set: (on: boolean) => Promise<unknown>, disabled = false): HTMLElement {
  return h("div", { class: "row" },
    h("div", { class: "row-text" }, h("span", { class: "row-name" }, name), h("span", { class: "row-sub" }, detail)),
    toggle(name, on, (v) => void attempt(() => set(v)), disabled));
}

let rerender: () => void = () => {};

export function renderSettings(snap: Snapshot, redraw: () => void): HTMLElement {
  rerender = redraw;
  const plans = snap.plans ?? [];
  const st = snap.settings;

  const cutoff = h("input", {
    id: "cutoff", type: "time", class: "time-input", value: st?.cutoff ?? "09:00",
    onchange: (e: Event) => {
      const v = (e.target as HTMLInputElement).value;
      if (v) void attempt(() => call("SetCutoff", v));
    },
  });

  const notifyDetail = snap.notifyUnavailable
    ? "直接运行程序时系统不允许发通知；从 Kikri.app 启动后可用"
    : "只在未登录或多次唤醒失败时提醒";

  const body = h("div", { class: "body stack scroll" },
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, "排班"), scheduleCard(snap, plans)),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, "补发"),
      h("div", { class: "panel" }, h("div", { class: "row" },
        h("div", { class: "row-text" },
          h("label", { class: "row-name", for: "cutoff" }, "补发截止时间"),
          h("span", { class: "row-sub" }, "错过的早上排班，开盖联网后在这个时间之前补发；再晚补发会把下午的刷新往后推")),
        cutoff))),
    h("section", { class: "stack-xs" },
      h("div", { class: "row-between" }, h("h2", { class: "section-label" }, "套餐"),
        h("button", {
          type: "button", class: "link", disabled: snap.detecting,
          onclick: () => void attempt(() => call("Refresh")),
        }, snap.detecting ? "检测中…" : "重新检测")),
      planRows(snap, plans)),
    h("section", { class: "stack-xs" }, h("h2", { class: "section-label" }, "选项"),
      h("div", { class: "panel" },
        option("参考你自己的使用", "你 5 小时内用过就不再唤醒；只看会话文件的修改时间，不读内容",
          st?.useActivity ?? true, (v) => call("SetUseActivity", v)),
        option("开机自启", "登录后在菜单栏常驻，到点才能唤醒", snap.autostart, (v) => call("SetAutostart", v)),
        option("通知", notifyDetail, snap.prefs.notify && !snap.notifyUnavailable, (v) => call("SetNotify", v), !!snap.notifyUnavailable))));

  const foot = h("div", { class: "foot row-between" },
    h("span", { class: "foot-note" }, `Kikri ${snap.version}`),
    h("button", { type: "button", class: "btn small", onclick: () => void call("Quit") }, "退出 Kikri"));
  return h("div", { class: "view" }, body, foot);
}
