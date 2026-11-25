import { Call } from "/wails/runtime.js";

const SERVICE = "github.com/ethan2047rr/kikri/internal/app.Service";

export async function call<T = void>(method: string, ...args: unknown[]): Promise<T> {
  return (await Call.ByName(`${SERVICE}.${method}`, ...args)) as T;
}

type Child = Node | string | null | undefined | false;
type Props = Record<string, unknown> & { class?: string };

/** h("div", { class: "x", onclick: f }, child, ...) */
export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props?: Props | null,
  ...children: (Child | Child[])[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props ?? {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === "class") node.className = String(v);
    else if (k.startsWith("on") && typeof v === "function") {
      node.addEventListener(k.slice(2), v as EventListener);
    } else if (k in node && typeof v !== "string") {
      (node as unknown as Record<string, unknown>)[k] = v;
    } else node.setAttribute(k, v === true ? "" : String(v));
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    node.append(c);
  }
  return node;
}

/** An on/off switch (role=switch). */
export function toggle(label: string, on: boolean, onChange: (on: boolean) => void, disabled = false): HTMLButtonElement {
  return h("button", {
    type: "button", class: "switch", role: "switch",
    "aria-checked": String(on), "aria-label": label, disabled,
    onclick: () => onChange(!on),
  }, h("span", { class: "knob" }));
}

/** A segmented control; returns the container. */
export function segmented<T extends string>(
  options: { value: T; label: string }[],
  current: T,
  onPick: (v: T) => void,
  ariaLabel: string,
): HTMLDivElement {
  return h("div", { class: "seg", role: "radiogroup", "aria-label": ariaLabel },
    options.map((o) => h("button", {
      type: "button", role: "radio", "aria-checked": String(o.value === current),
      class: o.value === current ? "on" : "",
      onclick: () => { if (o.value !== current) onPick(o.value); },
    }, o.label)));
}

export function chip(label: string, tone: string): HTMLSpanElement {
  return h("span", { class: `chip ${tone}` }, label);
}

/** Shows a short message at the bottom of the popup. */
export function toast(msg: string, bad = false): void {
  document.querySelector(".toast")?.remove();
  const t = h("div", { class: bad ? "toast bad" : "toast", role: "status" }, msg);
  document.body.append(t);
  setTimeout(() => t.remove(), 4000);
}

/** Runs an action and reports its error, if any. */
export async function attempt(fn: () => Promise<unknown>): Promise<void> {
  try {
    await fn();
  } catch (err) {
    toast(err instanceof Error ? err.message : String(err), true);
  }
}

/** Parses "6:30", "06:30", "630", "0630", "18" or "18.30" into "HH:MM" (24-hour). */
export function parseTime(raw: string): string | null {
  const s = raw.trim().replace(/[：.\s]/g, ":");
  let hh: number;
  let mm: number;
  const m = s.match(/^(\d{1,2}):(\d{1,2})$/);
  if (m) {
    hh = Number(m[1]);
    mm = Number(m[2]);
  } else if (/^\d{1,4}$/.test(s)) {
    if (s.length <= 2) { hh = Number(s); mm = 0; } else { hh = Number(s.slice(0, -2)); mm = Number(s.slice(-2)); }
  } else return null;
  if (hh > 23 || mm > 59) return null;
  return `${String(hh).padStart(2, "0")}:${String(mm).padStart(2, "0")}`;
}

/**
 * A 24-hour time field. Native <input type="time"> follows the system's
 * 12/24-hour setting, which a page cannot override. Commits on Enter or
 * blur; ↑/↓ step an hour (15 minutes with Shift); invalid input reverts.
 */
export function timeField(value: string, onCommit: (hhmm: string) => void, ariaLabel: string, id?: string): HTMLInputElement {
  const input = h("input", {
    type: "text", class: "time-input", value, id, "aria-label": ariaLabel,
    inputmode: "numeric", autocomplete: "off", spellcheck: false, maxlength: "5", placeholder: "HH:MM",
  });
  let committed = value;
  const commit = () => {
    const t = parseTime(input.value);
    if (!t) { input.value = committed; return; }
    input.value = t;
    if (t !== committed) { committed = t; onCommit(t); }
  };
  input.addEventListener("focus", () => input.select());
  input.addEventListener("blur", commit);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); input.blur(); }
    else if (e.key === "Escape") { input.value = committed; input.blur(); e.stopPropagation(); }
    else if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      e.preventDefault();
      const t = parseTime(input.value) ?? committed;
      const [hh, mm] = t.split(":").map(Number);
      const step = (e.shiftKey ? 15 : 60) * (e.key === "ArrowUp" ? 1 : -1);
      const total = (((hh! * 60 + mm! + step) % 1440) + 1440) % 1440;
      input.value = `${String(Math.floor(total / 60)).padStart(2, "0")}:${String(total % 60).padStart(2, "0")}`;
    }
  });
  return input;
}
