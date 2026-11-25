import { dayLabel, hhmm, outcomeChip, planTitle, resultDetail } from "./format";
import type { Entry, Snapshot } from "./types";
import { L } from "./i18n";
import { logo } from "./logos";
import { attempt, call, chip, h } from "./ui";

let cache: { version: number; entries: Entry[] } | null = null;
let loading = false;

/** Loads the log when it changed since the last load; calls done afterwards. */
export function loadLog(snap: Snapshot, done: () => void): void {
  if (loading || (cache && cache.version === snap.logVersion)) return;
  loading = true;
  const version = snap.logVersion;
  void call<Entry[] | null>("Log", "", 100)
    .then((entries) => { cache = { version, entries: entries ?? [] }; })
    .catch(() => { cache = { version, entries: [] }; })
    .finally(() => { loading = false; done(); });
}

export function renderLog(snap: Snapshot): HTMLElement {
  const names = new Map((snap.plans ?? []).map((p) => [p.plan.id, planTitle(snap, p)]));
  const tools = new Map((snap.plans ?? []).map((p) => [p.plan.id, p.plan.tool]));
  const body = h("div", { class: "body stack-sm scroll" });
  const entries = cache?.entries ?? [];

  if (!cache) {
    body.append(h("p", { class: "placeholder" }, L.loading));
  } else if (entries.length === 0) {
    body.append(h("p", { class: "placeholder" }, L.noLog));
  } else {
    const groups = new Map<string, Entry[]>();
    for (const e of entries) {
      const key = new Date(e.startedAt).toDateString();
      groups.set(key, [...(groups.get(key) ?? []), e]);
    }
    for (const list of groups.values()) {
      body.append(h("div", { class: "day" },
        h("span", { class: "section-label" }, dayLabel(new Date(list[0]!.startedAt))),
        h("div", { class: "panel" }, list.map((e) => {
          const c = outcomeChip(e);
          const detail = resultDetail(e);
          return h("div", { class: "row log-row" },
            h("span", { class: "num strong" }, hhmm(e.startedAt)),
            h("div", { class: "row-text" },
              h("span", { class: "row-name with-logo" }, logo(tools.get(e.plan) ?? "", 14), names.get(e.plan) ?? e.plan),
              h("span", { class: "row-sub", title: e.result?.message ?? detail }, detail)),
            chip(c.label, c.tone));
        }))));
    }
  }

  const s = snap.stats;
  const foot = h("div", { class: "foot row-between" },
    h("span", { class: "foot-note" }, L.monthStats(s.monthWakes, s.monthSkipped)),
    h("button", { type: "button", class: "btn small", onclick: () => void attempt(() => call("OpenDataFolder")) }, L.openDataFolder));
  return h("div", { class: "view" }, body, foot);
}
