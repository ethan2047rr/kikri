import { DAY_ORDER, DAY_SHORT, PRESET_LABEL } from "./format";
import type { Preset } from "./types";
import { h, timeField } from "./ui";

// The schedule editor shared by settings and onboarding.
//
// - The list of times is the setting; presets only fill it in.
// - Each time shows the refresh it buys ("→ 11:30 刷新"): that is what the
//   user actually cares about (cf. iOS Sleep, which derives bedtime from
//   the wake-up time).
// - A 24-hour strip draws each 5-hour window, so gaps and overlaps are
//   visible. It is a picture, not a control: dragging is hard to aim.
// - Times are listed earliest first and edited independently. A new time
//   defaults to the window plus 5 minutes after the last one, so it lands
//   just after the refresh.
// - Steppers move a time by an hour (15 minutes with Shift).
// - Weekday shortcuts, and a preview of the next wake.

export const PRESET_TIMES: Record<Exclude<Preset, "custom">, string[]> = {
  "early-bird": ["06:30"],
  three: ["06:00", "11:05"],
};

const MAX_TIMES = 4; // more than four 5-hour windows do not fit in a day
/** Wake this long after the previous window refreshes (as the 三轮 preset does). */
const AFTER_REFRESH = 5;
const WORKDAYS = [false, true, true, true, true, true, false];
const EVERYDAY = [true, true, true, true, true, true, true];
const WEEKEND = [true, false, false, false, false, false, true];
const WEEKDAY_NAME = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];

export interface ScheduleValue {
  times: string[];
  weekdays: boolean[]; // Sunday = 0
}

export function normalize(times: string[]): string[] {
  return [...new Set(times.filter(Boolean))].sort();
}

/** The preset whose times equal these, else "custom". */
export function presetFor(times: string[]): Preset {
  const t = normalize(times).join(",");
  for (const [name, pt] of Object.entries(PRESET_TIMES)) {
    if (pt.join(",") === t) return name as Preset;
  }
  return "custom";
}

function toMin(hhmm: string): number {
  const [hh, mm] = hhmm.split(":").map(Number);
  return hh! * 60 + mm!;
}

function fromMin(total: number): string {
  const t = ((total % 1440) + 1440) % 1440;
  return `${String(Math.floor(t / 60)).padStart(2, "0")}:${String(t % 60).padStart(2, "0")}`;
}

function sameDays(a: boolean[], b: boolean[]): boolean {
  return a.every((v, i) => !!v === b[i]);
}

/** The next wake after now, e.g. "明天（周四）06:30". */
export function nextWakeLabel(v: ScheduleValue, now = new Date()): string | null {
  const times = normalize(v.times);
  if (times.length === 0 || !v.weekdays.some(Boolean)) return null;
  const nowMin = now.getHours() * 60 + now.getMinutes();
  for (let d = 0; d < 8; d++) {
    const day = (now.getDay() + d) % 7;
    if (!v.weekdays[day]) continue;
    const t = times.find((x) => d > 0 || toMin(x) > nowMin);
    if (!t) continue;
    const name = WEEKDAY_NAME[day]!;
    const when = d === 0 ? "今天" : d === 1 ? `明天（${name}）` : name;
    return `${when} ${t}`;
  }
  return null;
}

/** Explains the first wake that falls inside an earlier window. */
function overlapNote(times: string[], skip: Map<string, number | null>): string | null {
  for (const [t, at] of skip) {
    return at === null
      ? `${t} 时上一轮窗口还没结束，这次会被跳过；隔 5 小时以上才能接上下一轮`
      : `${t} 时上一轮窗口还没结束，会等它刷新后 ${fromMin(at)} 再唤醒`;
  }
  return null;
}

/**
 * Times that fall inside the window opened by an earlier one, mapped to when
 * they wake instead (the refresh plus 5 minutes), or null when they are just
 * skipped: deferral off, past midnight, or not before the next time. Mirrors
 * the scheduler's deferTo.
 */
function skippedTimes(times: string[], windowMins: number, defer: boolean): Map<string, number | null> {
  const out = new Map<string, number | null>();
  let openUntil = -1;
  times.forEach((t, i) => {
    const m = toMin(t);
    if (m >= openUntil) { openUntil = m + windowMins; return; }
    const at = openUntil + AFTER_REFRESH;
    const next = times[i + 1] ? toMin(times[i + 1]!) : 1440;
    if (defer && at < 1440 && at < next) {
      out.set(t, at);
      openUntil = at + windowMins;
    } else out.set(t, null);
  });
  return out;
}

function stepper(label: string, onStep: (mins: number) => void): HTMLElement {
  const btn = (dir: 1 | -1, glyph: string) => h("button", {
    type: "button", class: "step", "aria-label": `${label}${dir > 0 ? "推后" : "提前"} 1 小时（按住 Shift 为 15 分钟）`,
    title: `${dir > 0 ? "推后" : "提前"} 1 小时，按住 Shift 为 15 分钟`,
    onclick: (e: MouseEvent) => onStep(dir * (e.shiftKey ? 15 : 60)),
  }, glyph);
  return h("span", { class: "stepper" }, btn(1, "⌃"), btn(-1, "⌄"));
}

/** A 24-hour strip with one block per window. */
function timeline(times: string[], windowMins: number, skip: Map<string, number | null>): HTMLElement {
  const bar = h("div", { class: "tl-bar", role: "img", "aria-label": times.map((t) => `${t} 到 ${fromMin(toMin(t) + windowMins)}`).join("，") });
  for (const t of times) {
    const at = skip.get(t);
    const start = at ?? toMin(t); // a deferred wake opens its window later
    const end = start + windowMins;
    const skipped = at === null;
    // A window past midnight is drawn in two parts.
    const parts: [number, number][] = end <= 1440 ? [[start, end]] : [[start, 1440], [0, end - 1440]];
    for (const [a, b] of parts) {
      bar.append(h("span", {
        class: skipped ? "tl-win skip" : "tl-win",
        style: `left:${(a / 1440) * 100}%;width:${((b - a) / 1440) * 100}%`,
        title: skipped ? `${t} 会被跳过` : `${fromMin(start)}–${fromMin(end)}`,
      }));
    }
  }
  const now = new Date();
  bar.append(h("span", { class: "tl-now", style: `left:${((now.getHours() * 60 + now.getMinutes()) / 1440) * 100}%`, title: "现在" }));
  const ticks = h("div", { class: "tl-ticks", "aria-hidden": "true" },
    [0, 6, 12, 18, 24].map((x) => h("span", { style: `left:${(x / 24) * 100}%` }, String(x))));
  return h("div", { class: "timeline" }, bar, ticks);
}

function refreshLabel(t: string, deferred: number | null | undefined, windowMins: number): HTMLElement {
  if (deferred === null) return h("span", { class: "refresh warn-text" }, "会被跳过");
  if (deferred !== undefined) {
    return h("span", { class: "refresh", title: `${t} 时窗口还在跑，等刷新后再唤醒` },
      "顺延 ", h("b", null, fromMin(deferred)), " → ", h("b", null, fromMin(deferred + windowMins)));
  }
  return h("span", { class: "refresh" }, "→ ", h("b", null, fromMin(toMin(t) + windowMins)), " 刷新");
}

/** Times, timeline, templates, weekdays and the next-wake preview. */
export function scheduleEditor(value: ScheduleValue, onChange: (v: ScheduleValue & { preset: Preset }) => void, windowMins = 300, deferSkipped = false): HTMLElement {
  const times = normalize(value.times);
  const weekdays = value.weekdays;
  const emit = (next: Partial<ScheduleValue>) => {
    const t = normalize(next.times ?? times);
    if (t.length > 0) onChange({ times: t, weekdays: next.weekdays ?? weekdays, preset: presetFor(t) });
  };
  const skip = skippedTimes(times, windowMins, deferSkipped);
  const gap = windowMins + AFTER_REFRESH;
  const move = (i: number, v: string) => emit({ times: times.map((x, j) => (j === i ? v : x)) });
  const last = times[times.length - 1];
  // After the last window: a deferred last wake opens it later.
  const nextAdd = last ? (skip.get(last) ?? toMin(last)) + gap : toMin("06:30");

  const rows = h("div", { class: "wake-rows" }, times.map((t, i) => h("div", { class: "wake-row" },
    h("span", { class: "time-edit" },
      timeField(t, (v) => move(i, v), `唤醒时间 ${i + 1}`),
      stepper(`唤醒时间 ${t} `, (m) => move(i, fromMin(toMin(t) + m)))),
    refreshLabel(t, skip.get(t), windowMins),
    times.length > 1 && h("button", {
      type: "button", class: "x", "aria-label": `删除 ${t}`,
      onclick: () => emit({ times: times.filter((_, j) => j !== i) }),
    }, "×"))));
  if (times.length < MAX_TIMES && nextAdd < 1440) {
    rows.append(h("button", {
      type: "button", class: "add", title: `在 ${fromMin(nextAdd)} 再唤醒一次，接上下一轮`,
      onclick: () => emit({ times: [...times, fromMin(nextAdd)] }),
    }, `＋ 再接一轮（${fromMin(nextAdd)}）`));
  }

  const applied = presetFor(times);
  const templates = h("div", { class: "chips-line" },
    h("span", { class: "row-sub" }, "快速填入"),
    (Object.keys(PRESET_TIMES) as (keyof typeof PRESET_TIMES)[]).map((v) => h("button", {
      type: "button", class: "template", disabled: applied === v,
      title: applied === v ? "已经是这个时间" : `把时间设为 ${PRESET_TIMES[v].join("、")}`,
      onclick: () => emit({ times: PRESET_TIMES[v] }),
    }, h("b", null, PRESET_LABEL[v]), ` ${PRESET_TIMES[v].join(" / ")}`)));

  const dayShortcuts: [string, boolean[]][] = [["工作日", WORKDAYS], ["每天", EVERYDAY], ["周末", WEEKEND]];
  const days = h("div", { class: "stack-xs" },
    h("div", { class: "chips-line" },
      h("span", { class: "row-sub" }, "哪几天"),
      dayShortcuts.map(([label, w]) => h("button", {
        type: "button", class: "template", disabled: sameDays(weekdays, w),
        onclick: () => emit({ weekdays: w }),
      }, label))),
    h("div", { class: "days" }, DAY_ORDER.map((i) => {
      const on = !!weekdays[i];
      return h("button", {
        type: "button", class: on ? "day-btn on" : "day-btn", "aria-pressed": String(on), "aria-label": `周${DAY_SHORT[i]}`,
        onclick: () => emit({ weekdays: weekdays.map((x, j) => (j === i ? !on : !!x)) }),
      }, DAY_SHORT[i]);
    })));

  const overlap = overlapNote(times, skip);
  const next = nextWakeLabel({ times, weekdays });
  return h("div", { class: "stack" },
    h("div", { class: "stack-sm" }, rows, timeline(times, windowMins, skip),
      overlap && h("span", { class: overlap.includes("跳过") ? "row-sub warn-text" : "row-sub" }, overlap),
      templates),
    days,
    h("div", { class: "next-wake" }, next ? ["下次唤醒：", h("b", null, next)] : "没有选星期，不会唤醒"));
}
