// The pure half of the Logix deploy commands (logixDeploy.ts): the
// confirmation wording, the NDJSON stream `naut logix serve --project`
// answers POST /api/logix/deploy with, the result summaries, the
// `naut logix serve` command line, and whether a nautilus.yaml declares a
// Logix target. No vscode import, so node:test runs it as is.

/** What GET /api/program says about a Logix target (logix/facade). */
export type LogixInfo = {
  target?: string; // "logix" on `naut logix serve --project`
  controller?: string;
  commPath?: string;
  canDownload?: boolean;
  hash: string;
};

/** One line of POST /api/logix/deploy's answer. */
export type DeployEvent =
  | { event: "progress"; message: string }
  | {
      event: "done";
      hash?: string;
      applied?: string; // "online edit" | "download" | "build"
      same?: boolean;
      replaced?: number;
      verified?: boolean;
      modeBefore?: string;
      mode?: string;
      elapsedMs?: number;
    }
  | { event: "error"; status?: number; error: string };

export type DeployMode = "online" | "download";

/** Does this nautilus.yaml declare `target: logix`? A line scan, not a YAML
 * parse: a top-level `target:` whose indented block (or flow mapping) names
 * `logix:`. Comments and other top-level keys end the block. */
export function declaresLogixTarget(yaml: string): boolean {
  const lines = yaml.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const m = /^target\s*:\s*(.*)$/.exec(lines[i]);
    if (!m) continue;
    const rest = m[1].replace(/\s+#.*$/, "").trim();
    if (rest) return /^\{.*\blogix\s*:/.test(rest);
    for (let j = i + 1; j < lines.length; j++) {
      const l = lines[j];
      if (/^\s*(#.*)?$/.test(l)) continue; // blank or comment
      if (!/^\s/.test(l)) break; // the next top-level key
      if (/^\s+logix\s*:/.test(l)) return true;
    }
  }
  return false;
}

function where(info: LogixInfo): string {
  const name = info.controller || "the Logix controller";
  return info.commPath ? `${name} at ${info.commPath}` : name;
}

/** The modal before an online edit. */
export function onlineEditConfirmMessage(info: LogixInfo, programFile: string): string {
  return (
    `Push ${programFile} to ${where(info)} as an ONLINE EDIT? ` +
    "The routine's rungs are replaced and finalized in the running controller, which stays in its mode " +
    `(it runs ${info.hash} now).`
  );
}

/** The modal before a download — always shown, whatever
 * nautilus.confirmControllerWrites says: a download stops a PLC. */
export function downloadConfirmMessage(info: LogixInfo, programFile: string): string {
  return (
    `Download ${programFile} to ${where(info)}? ` +
    "A DOWNLOAD STOPS THE CONTROLLER: it must be in Program mode, its tags are reset to the project's values, " +
    "and it stays in Program mode afterwards — nautilus never switches it back to Run."
  );
}

export const DOWNLOAD = "Download";
export const DOWNLOAD_PROGRAM_MODE = "Switch to Program Mode and Download";
export const PUSH = "Push Online Edit";

/** Splits a byte stream into NDJSON events; a line that is not JSON is
 * passed on as progress so nothing the server says is lost. */
export class NdjsonSplitter {
  private buf = "";
  feed(chunk: string): DeployEvent[] {
    this.buf += chunk;
    const out: DeployEvent[] = [];
    let nl: number;
    while ((nl = this.buf.indexOf("\n")) >= 0) {
      const line = this.buf.slice(0, nl).trim();
      this.buf = this.buf.slice(nl + 1);
      if (line) out.push(parseEvent(line));
    }
    return out;
  }
  /** Whatever is left when the stream ends without a final newline. */
  flush(): DeployEvent[] {
    const line = this.buf.trim();
    this.buf = "";
    return line ? [parseEvent(line)] : [];
  }
}

function parseEvent(line: string): DeployEvent {
  try {
    const v = JSON.parse(line) as Record<string, unknown>;
    if (v.event === "progress" || v.event === "done" || v.event === "error") return v as DeployEvent;
    // A plain error body ({"error": "..."}) — the answers before the stream.
    if (typeof v.error === "string") return { event: "error", error: v.error };
  } catch {
    /* not JSON: shown as it is */
  }
  return { event: "progress", message: line };
}

function seconds(ms?: number): string {
  if (ms === undefined) return "";
  return ms < 10_000 ? ` in ${(ms / 1000).toFixed(1)} s` : ` in ${Math.round(ms / 1000)} s`;
}

/** The notification after a deploy that went through. */
export function doneSummary(mode: DeployMode, info: LogixInfo, ev: Extract<DeployEvent, { event: "done" }>): string {
  const name = info.controller || "the controller";
  if (ev.same) return `nautilus: ${name} already runs this logic — nothing sent`;
  const verified = ev.verified ? " and verified" : " (NOT verified)";
  if (mode === "download") {
    return (
      `nautilus: downloaded to ${name}${verified}${seconds(ev.elapsedMs)} — the controller is in ` +
      `${ev.mode || "Program"} mode; put it in Run when the process is ready`
    );
  }
  const rungs = ev.replaced ? `, ${ev.replaced} rung${ev.replaced === 1 ? "" : "s"} replaced` : "";
  const mode_ = ev.mode ? `, controller still in ${ev.mode}` : "";
  return `nautilus: online edit live on ${name}${verified}${rungs}${mode_}${seconds(ev.elapsedMs)} — commit the file to keep it`;
}

/** Does a refusal say the change needs a download (new tags, no routine)? */
export function needsDownload(ev: Extract<DeployEvent, { event: "error" }>): boolean {
  return ev.status === 422 && /download/i.test(ev.error) && !/outside the Logix v1 subset/.test(ev.error);
}

/** `naut logix serve` for a project, listening where nautilus.runtimeUrl
 * points (its port; ":8080" when it names none). */
export function serveCliArgs(projectDir: string, runtimeUrl: string): string[] {
  let port = "8080";
  try {
    const u = new URL(runtimeUrl);
    port = u.port || (u.protocol === "https:" ? "443" : "80");
  } catch {
    /* the default */
  }
  return ["logix", "serve", "--project", projectDir, "--listen", `127.0.0.1:${port}`];
}

/** A command line for a terminal: each argument quoted when it needs it. */
export function shellLine(cmd: string, args: string[], windows = false): string {
  const q = (a: string) => {
    if (/^[\w@%+=:,./\\-]+$/.test(a)) return a;
    return windows ? `"${a.replace(/"/g, '\\"')}"` : `'${a.replace(/'/g, "'\\''")}'`;
  };
  // PowerShell runs a quoted command only behind the call operator.
  const head = q(cmd);
  return (windows && head.startsWith('"') ? "& " : "") + [head, ...args.map(q)].join(" ");
}
