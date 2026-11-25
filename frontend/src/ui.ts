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
