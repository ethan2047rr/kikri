import { cleanVersion, LOGIN_COMMAND, planTitle, toolFor } from "./format";
import type { PlanStatus, Preset, Snapshot, WakeResult } from "./types";
import { attempt, call, chip, h, segmented } from "./ui";

const WORKDAYS = [false, true, true, true, true, true, false];

let preset: Preset = "early-bird";
let startAtLogin = true;
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
  if (!snap.detectedOnce) { sub = "正在检测…"; c = chip("检测中", "muted"); }
  else if (!t?.found) { sub = `未找到 ${ps.plan.tool}`; c = chip("未安装", "muted"); }
  else if (t.auth && !t.auth.loggedIn) {
    sub = `请先运行 ${LOGIN_COMMAND[ps.plan.tool] ?? ps.plan.tool + " login"}`;
    c = chip("未登录", "bad");
  } else { sub = `${t.name} ${cleanVersion(t.version)}`.trim(); c = chip("已登录", "ok"); }
  return h("div", { class: t?.found || !snap.detectedOnce ? "row" : "row absent" },
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
    return h("div", { class: "test-line" }, h("span", { class: "ok-text" }, "✓ 测试唤醒成功"), h("span", { class: "row-sub" }, parts.join(" · ")));
  }
  return h("div", { class: "test-line" }, h("span", { class: "bad-text" }, `✗ ${r.title} 没有成功`),
    h("span", { class: "row-sub" }, r.err ?? res?.message ?? res?.outcome ?? ""));
}

async function finish(snap: Snapshot): Promise<void> {
  for (const p of snap.plans ?? []) {
    if (ready(snap, p)) {
      await call("SetPlan", p.plan.id, { ...p.schedule, enabled: true, preset, times: null, weekdays: WORKDAYS });
    }
  }
  await call("FinishOnboarding", startAtLogin);
}

export function renderOnboarding(snap: Snapshot, redraw: () => void): HTMLElement {
  rerender = redraw;
  const plans = snap.plans ?? [];
  const anyReady = plans.some((p) => ready(snap, p));
  const hint = preset === "early-bird"
    ? h("span", { class: "row-sub" }, "工作日 ", h("b", null, "06:30"), " 唤醒，", h("b", null, "11:30"), " 就能用上第 2 轮额度")
    : h("span", { class: "row-sub" }, "工作日 ", h("b", null, "06:00"), " 唤醒；", h("b", null, "11:05"), " 没有窗口在跑时再唤醒一次");

  const test = h("div", { class: "panel pad stack-xs" },
    results.length ? results.map(resultLine) : h("span", { class: "row-sub" }, "可选：现在发一条真实请求试试。会开启这个套餐的 5 小时窗口。"),
    h("button", {
      type: "button", class: "btn small", disabled: testing || !anyReady || !snap.detectedOnce,
      onclick: () => void attempt(() => runTest(snap)),
    }, testing ? "唤醒中…" : results.length ? "再测一次" : "测试唤醒"));

  return h("div", { class: "view onboarding" },
    h("div", { class: "body stack scroll" },
      h("div", { class: "stack-xs" },
        h("div", { class: "brand" }, h("img", { src: "/chick.svg", alt: "", width: "26", height: "26" }), h("h1", null, "欢迎使用 Kikri")),
        h("p", { class: "lead" }, "每天清晨替你发一条极小的请求，让 5 小时额度窗口提前开始。")),
      h("section", { class: "stack-xs" }, h("span", { class: "section-label" }, "在这台电脑上找到"),
        h("div", { class: "panel" }, plans.map((p) => toolRow(snap, p)))),
      h("section", { class: "stack-xs" }, h("span", { class: "section-label" }, "选一个排班"),
        segmented<Preset>([{ value: "early-bird", label: "早鸟" }, { value: "three", label: "三轮" }], preset,
          (v) => { preset = v; rerender(); }, "排班预设"),
        hint,
        h("span", { class: "row-sub" }, "之后可以在设置里改时间和星期")),
      test,
      h("label", { class: "check" },
        h("input", { type: "checkbox", checked: startAtLogin, onchange: (e: Event) => { startAtLogin = (e.target as HTMLInputElement).checked; } }),
        "登录电脑后自动启动 Kikri")),
    h("div", { class: "foot stack-sm" },
      h("button", {
        type: "button", class: "btn primary", disabled: !snap.detectedOnce,
        onclick: () => void attempt(() => finish(snap)),
      }, anyReady || !snap.detectedOnce ? "开始使用" : "先跳过，稍后再设置"),
      h("span", { class: "foot-note" }, "只调用本机已登录的官方 CLI，不读取任何凭证")));
}
