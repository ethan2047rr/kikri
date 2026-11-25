// Mirrors the JSON produced by internal/app.Snapshot and internal/detect.

export type Source = "override" | "login-shell" | "process-path" | "fallback";

export interface AuthInfo {
  loggedIn: boolean;
  method?: string;
  plan?: string;
  expiresAt?: string;
  detail?: string;
}

export interface ToolStatus {
  id: string;
  name: string;
  found: boolean;
  path?: string;
  source?: Source;
  version?: string;
  auth?: AuthInfo;
  error?: string;
}

export interface SearchDir {
  dir: string;
  source: Source;
}

export interface Report {
  checkedAt: string;
  searchPath: { dirs: SearchDir[] | null; loginShellError?: string };
  tools: ToolStatus[] | null;
}

export interface Layout {
  root: string;
  sandbox: string;
  codexInstructions: string;
  database: string;
}

export interface Snapshot {
  version: string;
  appDir: Layout;
  appDirError?: string;
  detecting: boolean;
  detectedOnce: boolean;
  report: Report;
}
