import { Events } from "/wails/runtime.js";
import { scheduleSummary } from "./format";
import { loadLog, renderLog } from "./log";
import { renderOnboarding } from "./onboarding";
import { renderSettings } from "./settings";
import { renderStatus } from "./status";
import type { Snapshot } from "./types";
import { call, h } from "./ui";

const EVENT_SNAPSHOT = "kikri:snapshot";

type Tab = "status" | "log" | "settings";
const TABS: { id: Tab; label: string }[] = [
  { id: "status", label: "状态" },
  { id: "log", label: "日志" },
  { id: "settings", label: "设置" },
];

let snap: Snapshot | null = null;
let tab: Tab = "status";
let pending = false; // a render waits for the focused input to blur

function headerNote(s: Snapshot): string {
  if (tab === "settings") return "设置会自动保存";
  const on = (s.plans ?? []).filter((p) => p.schedule.enabled);
  if (on.length === 0) return "";
  const summaries = new Set(on.map((p) => scheduleSummary(p.schedule, p.times)));
  return summaries.size === 1 ? [...summaries][0]! : `${on.length} 个套餐`;
}

function header(s: Snapshot): HTMLElement {
  return h("header", { class: "top" },
    h("div", { class: "row-between" },
      h("div", { class: "brand" }, h("img", { src: "/chick.svg", alt: "", width: "20", height: "20" }), h("span", { class: "name" }, "Kikri")),
      h("span", { class: "header-note" }, headerNote(s))),
    h("nav", { class: "seg tabs", "aria-label": "视图" }, TABS.map((t) => h("button", {
      type: "button", class: t.id === tab ? "on" : "", "aria-current": t.id === tab ? "page" : undefined,
      onclick: () => { tab = t.id; render(); },
    }, t.label))));
}

function render(): void {
  const s = snap;
  if (!s) return;
  const root = document.getElementById("app")!;
  // Re-rendering replaces the inputs; wait while the user is typing in one.
  const active = document.activeElement;
  if (active instanceof HTMLInputElement && root.contains(active) && active.type !== "checkbox") {
    if (!pending) {
      pending = true;
      active.addEventListener("blur", () => { pending = false; setTimeout(render, 0); }, { once: true });
    }
    return;
  }
  const scroller = root.querySelector(".scroll");
  const scrollTop = scroller?.scrollTop ?? 0;
  const prevTab = root.dataset.tab;

  if (!s.prefs.onboarded && !s.storeError && !s.appDirError) {
    root.dataset.tab = "onboarding";
    root.replaceChildren(renderOnboarding(s, render));
  } else {
    let view: HTMLElement;
    if (tab === "log") {
      loadLog(s, render);
      view = renderLog(s);
    } else if (tab === "settings") {
      view = renderSettings(s, render);
    } else {
      view = renderStatus(s, () => { tab = "settings"; render(); });
    }
    root.dataset.tab = tab;
    root.replaceChildren(header(s), view);
  }
  const next = root.querySelector(".scroll");
  if (next && prevTab === root.dataset.tab) next.scrollTop = scrollTop;
}

async function main(): Promise<void> {
  Events.On(EVENT_SNAPSHOT, (ev) => { snap = ev.data as Snapshot; render(); });
  snap = await call<Snapshot>("Snapshot");
  render();
  // Countdowns and "today" labels.
  setInterval(render, 30_000);
  document.addEventListener("keydown", (e) => {
    if (!e.metaKey && !e.ctrlKey) return;
    const i = ["1", "2", "3"].indexOf(e.key);
    if (i >= 0 && snap?.prefs.onboarded) { tab = TABS[i]!.id; render(); }
  });
}

void main();
