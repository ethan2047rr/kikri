import { Call, Events } from "/wails/runtime.js";
import type { AuthInfo, Snapshot, Source, ToolStatus } from "./types";

const SERVICE = "github.com/ethan2047rr/kikri/internal/app.Service";
const EVENT_SNAPSHOT = "kikri:snapshot";

const SOURCE_LABEL: Record<Source, string> = {
  "override": "手动指定",
  "login-shell": "登录 shell",
  "process-path": "进程 PATH",
  "fallback": "常见路径",
};

// Plan identifiers as reported by the CLIs, shown with their product names.
const PLAN_LABEL: Record<string, string> = {
  pro: "Pro",
  max: "Max",
  team: "Team",
  enterprise: "Enterprise",
  plus: "Plus",
};

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function homeRelative(path: string, root: string): string {
  // Shorten /Users/<name>/... to ~/... using the data dir as a hint.
  const m = root.match(/^(\/Users\/[^/]+|\/home\/[^/]+|[A-Za-z]:\\Users\\[^\\]+)/);
  const home = m?.[1];
  return home && path.startsWith(home) ? "~" + path.slice(home.length) : path;
}

function statusChip(t: ToolStatus, detecting: boolean): HTMLElement {
  if (!t.found) return el("span", "chip", t.error ? "路径无效" : "未安装");
  if (t.error) return el("span", "chip bad", "检测失败");
  if (!t.auth) return el("span", "chip", detecting ? "检测中" : "已安装");
  return t.auth.loggedIn ? el("span", "chip ok", "已登录") : el("span", "chip bad", "未登录");
}

function expiryNote(auth: AuthInfo): string | null {
  if (!auth.expiresAt) return null;
  const ms = Date.parse(auth.expiresAt) - Date.now();
  if (Number.isNaN(ms)) return null;
  const days = Math.floor(ms / 86_400_000);
  if (ms <= 0) return "登录已过期";
  return days < 3 ? `登录将在 ${Math.max(days, 0)} 天内过期` : null;
}

function renderTool(t: ToolStatus, snap: Snapshot): HTMLLIElement {
  const li = el("li", t.found ? "tool" : "tool absent");
  const head = el("div", "tool-head");
  head.append(el("span", "tool-name", t.name), statusChip(t, snap.detecting));
  li.append(head);

  if (t.found) {
    const parts: string[] = [];
    if (t.version) parts.push(t.version);
    if (t.auth?.plan) parts.push(PLAN_LABEL[t.auth.plan] ?? t.auth.plan);
    if (t.auth?.loggedIn && t.auth.method) parts.push(t.auth.method);
    if (parts.length) li.append(el("div", "tool-meta", parts.join(" · ")));

    const path = el("div", "tool-meta", homeRelative(t.path ?? "", snap.appDir.root));
    path.title = `${t.path}（来源：${t.source ? SOURCE_LABEL[t.source] : "—"}）`;
    li.append(path);

    const expiry = t.auth && expiryNote(t.auth);
    if (expiry) li.append(el("div", "tool-error", expiry));
  }
  if (t.error) li.append(el("div", "tool-error", t.error));
  return li;
}

function render(snap: Snapshot): void {
  $("version").textContent = snap.version;
  const refresh = $<HTMLButtonElement>("refresh");
  refresh.disabled = snap.detecting;
  refresh.textContent = snap.detecting ? "检测中…" : "重新检测";

  const tools = $("tools");
  const list = snap.report.tools ?? [];
  if (!snap.detectedOnce) {
    tools.replaceChildren(el("li", "placeholder", "正在检测…"));
  } else {
    // Installed tools first, keeping the built-in order otherwise.
    const sorted = [...list].sort((a, b) => Number(b.found) - Number(a.found));
    tools.replaceChildren(...sorted.map((t) => renderTool(t, snap)));
  }

  const root = snap.appDir.root;
  $("appdir").textContent = root ? homeRelative(root, root) : "—";
  const sandbox = $("sandbox");
  if (snap.appDirError) {
    sandbox.textContent = snap.appDirError;
    sandbox.className = "bad";
  } else {
    sandbox.textContent = snap.appDir.sandbox ? "就绪（空目录）" : "—";
    sandbox.className = "";
  }

  const sp = snap.report.searchPath;
  const dirs = sp.dirs ?? [];
  const pathSource = $("pathsource");
  if (!snap.detectedOnce) {
    pathSource.textContent = "—";
  } else if (sp.loginShellError) {
    pathSource.textContent = `登录 shell 失败，使用回退路径（${sp.loginShellError}）`;
    pathSource.className = "bad";
  } else {
    const n = dirs.filter((d) => d.source === "login-shell").length;
    pathSource.textContent = `登录 shell（${n} 个目录）+ 常见路径`;
    pathSource.className = "";
  }
  $("dircount").textContent = String(dirs.length);
  $("dirs").replaceChildren(
    ...dirs.map((d) => {
      const li = el("li", undefined, homeRelative(d.dir, root));
      li.append(el("span", "src", SOURCE_LABEL[d.source]));
      return li;
    }),
  );

  $("checked").textContent = snap.detectedOnce
    ? `上次检测：${new Date(snap.report.checkedAt).toLocaleString()}`
    : "";
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  return (await Call.ByName(`${SERVICE}.${method}`, ...args)) as T;
}

async function main(): Promise<void> {
  Events.On(EVENT_SNAPSHOT, (ev) => render(ev.data as Snapshot));
  $("refresh").addEventListener("click", () => {
    void call<Snapshot>("Refresh").then(render);
  });
  render(await call<Snapshot>("Snapshot"));
}

void main();
