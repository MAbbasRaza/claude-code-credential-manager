// Thin wrapper over the ccm binary.
//
// The extension deliberately owns no switching logic of its own. Everything
// goes through `ccm --json`, so the CLI, the tray app and this extension share
// one implementation and one set of safety checks. Duplicating the merge here
// would be the fastest way to end up with a version that clobbers MCP logins.

import { execFile } from "child_process";
import * as vscode from "vscode";

export interface Profile {
  name: string;
  email?: string;
  organization?: string;
  subscription?: string;
  active: boolean;
  expiresAt?: string;
  expired: boolean;
  lastUsedAt?: string;
}

export interface Status {
  configDir: string;
  configDirSource: string;
  backend: string;
  credentialsPath?: string;
  configJsonPath: string;
  activeProfile?: string;
  emailAddress?: string;
  accountUuid?: string;
  organization?: string;
  subscription?: string;
  expiresAt?: string;
  loggedIn: boolean;
}

export interface ListResult {
  profiles: Profile[] | null;
  status: Status;
}

export interface SwitchResult {
  switched: boolean;
  from?: string;
  fromEmail?: string;
  to: string;
  toEmail?: string;
  capturedAs?: string;
  capturedNew: boolean;
  backupDir?: string;
  restartWarning?: string;
}

/** Raised when the ccm binary cannot be found, so the UI can offer install help. */
export class BinaryMissingError extends Error {
  constructor(public readonly binary: string) {
    super(`ccm executable not found: ${binary}`);
  }
}

/** Raised when ccm ran but refused or failed; message is ccm's own stderr. */
export class CcmError extends Error {}

function config() {
  return vscode.workspace.getConfiguration("ccm");
}

function binaryPath(): string {
  const p = config().get<string>("binaryPath")?.trim();
  return p && p.length > 0 ? p : "ccm";
}

function baseArgs(): string[] {
  const dir = config().get<string>("claudeConfigDir")?.trim();
  return dir ? ["--config-dir", dir] : [];
}

function run(args: string[]): Promise<string> {
  const bin = binaryPath();
  return new Promise((resolve, reject) => {
    execFile(bin, [...baseArgs(), ...args], { timeout: 20000 }, (err, stdout, stderr) => {
      if (err) {
        const code = (err as NodeJS.ErrnoException).code;
        if (code === "ENOENT") {
          reject(new BinaryMissingError(bin));
          return;
        }
        // ccm writes actionable messages to stderr, for example the guard that
        // refuses to switch while Claude Code is running. Surface it verbatim.
        const msg = (stderr || stdout || err.message).trim();
        reject(new CcmError(msg));
        return;
      }
      resolve(stdout);
    });
  });
}

async function runJson<T>(args: string[]): Promise<T> {
  const out = await run([...args, "--json"]);
  try {
    return JSON.parse(out) as T;
  } catch {
    throw new CcmError(`ccm returned output that is not JSON:\n${out.slice(0, 500)}`);
  }
}

export async function list(): Promise<ListResult> {
  const res = await runJson<ListResult>(["list"]);
  // ccm omits an empty profile list rather than emitting [], so normalize.
  return { profiles: res.profiles ?? [], status: res.status };
}

export async function status(): Promise<Status> {
  return runJson<Status>(["status"]);
}

export async function use(name: string, force: boolean): Promise<SwitchResult> {
  const args = ["use", name];
  if (force) {
    args.push("--force");
  }
  return runJson<SwitchResult>(args);
}

export async function capture(name?: string): Promise<{ captured: string; email?: string }> {
  const args = name ? ["add", name] : ["add"];
  return runJson<{ captured: string; email?: string }>(args);
}

export async function rename(oldName: string, newName: string): Promise<void> {
  await runJson<{ renamed: string; to: string }>(["rename", oldName, newName]);
}

export async function remove(name: string): Promise<void> {
  await runJson<{ removed: string }>(["rm", name]);
}

export async function doctor(): Promise<string> {
  return run(["doctor"]);
}
