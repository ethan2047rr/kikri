import { Events } from "/wails/runtime.js";
import { L, setLang } from "./i18n";
import { loadLog, renderLog } from "./log";
import { renderOnboarding } from "./onboarding";
import { renderSettings } from "./settings";
import { renderStatus } from "./status";
import type { Snapshot } from "./types";
import { call, h } from "./ui";

const EVENT_SNAPSHOT = "kikri:snapshot";

// The status cards are home; settings sits behind the gear icon and the
// log behind a row in settings.
type Page = "status" | "settings" | "log";
const PARENT: Record<Page, Page | null> = { status: null, settings: "status", log: "settings" };

let snap: Snapshot | null = null;
let page: Page = "status";
let pending = false; // a render waits for the focused input to blur

function go(p: Page): void {
  page = p;
  render();
}

const GEAR = `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>`;

function header(): HTMLElement {
  const parent = PARENT[page];
  if (!parent) {
    const gear = h("button", { type: "button", class: "icon-btn", "aria-label": L.settingsButton, title: L.settingsShortcut, onclick: () => go("settings") });
    gear.innerHTML = GEAR; // static markup
    return h("header", { class: "top row-between" },
      h("div", { class: "brand" }, h("img", { src: "/chick.svg", alt: "", width: "20", height: "20" }), h("span", { class: "name" }, "Kikri")),
      gear);
  }
  return h("header", { class: "top row-between" },
    h("button", { type: "button", class: "back", "aria-label": L.back(parent === "status" ? L.backStatus : L.title[parent]), onclick: () => go(parent) },
      h("span", { class: "chevron", "aria-hidden": "true" }, "‹"), h("span", { class: "name" }, L.title[page])),
    page === "settings" && h("span", { class: "header-note" }, L.autosave));
}

function render(): void {
  const s = snap;
  if (!s) return;
  setLang(s.lang === "en" ? "en" : "zh");
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
  const prevPage = root.dataset.page;

  if (!s.prefs.onboarded && !s.storeError && !s.appDirError) {
    root.dataset.page = "onboarding";
    root.replaceChildren(renderOnboarding(s, render));
  } else {
    let view: HTMLElement;
    if (page === "log") {
      loadLog(s, render);
      view = renderLog(s);
    } else if (page === "settings") {
      view = renderSettings(s, render, () => go("log"));
    } else {
      view = renderStatus(s, () => go("settings"));
    }
    root.dataset.page = page;
    root.replaceChildren(header(), view);
  }
  const next = root.querySelector(".scroll");
  if (next && prevPage === root.dataset.page) next.scrollTop = scrollTop;
}

async function main(): Promise<void> {
  Events.On(EVENT_SNAPSHOT, (ev) => { snap = ev.data as Snapshot; render(); });
  snap = await call<Snapshot>("Snapshot");
  render();
  // Countdowns and "today" labels.
  setInterval(render, 30_000);
  // ⌘, opens settings; ⌘[ goes back (Esc already hides the popup).
  document.addEventListener("keydown", (e) => {
    if (!(e.metaKey || e.ctrlKey) || !snap?.prefs.onboarded) return;
    if (e.key === ",") go("settings");
    const parent = PARENT[page];
    if (e.key === "[" && parent) go(parent);
  });
}

void main();
