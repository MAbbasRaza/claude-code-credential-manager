import * as vscode from "vscode";
import * as ccm from "./ccm";

let statusItem: vscode.StatusBarItem | undefined;
let refreshTimer: NodeJS.Timeout | undefined;
let output: vscode.OutputChannel;

export function activate(context: vscode.ExtensionContext): void {
  output = vscode.window.createOutputChannel("Claude Code Credential Manager");
  context.subscriptions.push(output);

  statusItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
  statusItem.command = "ccm.switchAccount";
  context.subscriptions.push(statusItem);

  context.subscriptions.push(
    vscode.commands.registerCommand("ccm.switchAccount", switchAccount),
    vscode.commands.registerCommand("ccm.captureAccount", captureAccount),
    vscode.commands.registerCommand("ccm.manageAccounts", manageAccounts),
    vscode.commands.registerCommand("ccm.renameProfile", () => renameProfile()),
    vscode.commands.registerCommand("ccm.removeProfile", () => removeProfile()),
    vscode.commands.registerCommand("ccm.doctor", showDoctor),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("ccm")) {
        restartRefresh(context);
      }
    })
  );

  restartRefresh(context);
}

export function deactivate(): void {
  if (refreshTimer) {
    clearInterval(refreshTimer);
  }
}

function restartRefresh(context: vscode.ExtensionContext): void {
  if (refreshTimer) {
    clearInterval(refreshTimer);
    refreshTimer = undefined;
  }
  void refreshStatus();

  const seconds = vscode.workspace.getConfiguration("ccm").get<number>("refreshIntervalSeconds") ?? 60;
  if (seconds > 0) {
    refreshTimer = setInterval(() => void refreshStatus(), seconds * 1000);
    context.subscriptions.push({ dispose: () => refreshTimer && clearInterval(refreshTimer) });
  }
}

async function refreshStatus(): Promise<void> {
  if (!statusItem) {
    return;
  }
  const show = vscode.workspace.getConfiguration("ccm").get<boolean>("showStatusBar") ?? true;
  if (!show) {
    statusItem.hide();
    return;
  }

  try {
    const st = await ccm.status();
    if (!st.loggedIn) {
      statusItem.text = "$(account) Claude: signed out";
      statusItem.tooltip = "No Claude Code subscription login. Run /login in Claude Code.";
    } else {
      const label = st.activeProfile ?? st.emailAddress ?? "unknown";
      statusItem.text = `$(account) ${label}`;
      statusItem.tooltip = new vscode.MarkdownString(
        [
          `**Claude Code account**`,
          ``,
          `- Email: ${st.emailAddress ?? "unknown"}`,
          st.organization ? `- Org: ${st.organization}` : undefined,
          st.subscription ? `- Plan: ${st.subscription}` : undefined,
          `- Config dir: \`${st.configDir}\``,
          `- Resolved from: ${st.configDirSource}`,
          st.activeProfile ? undefined : `- Not yet captured as a profile`,
          ``,
          `Click to switch account.`
        ]
          .filter((l) => l !== undefined)
          .join("\n")
      );
    }
    statusItem.backgroundColor = undefined;
    statusItem.show();
  } catch (err) {
    if (err instanceof ccm.BinaryMissingError) {
      statusItem.text = "$(warning) ccm not found";
      statusItem.tooltip = `The ccm executable was not found (${err.binary}). Set "ccm.binaryPath" or install it.`;
      statusItem.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground");
      statusItem.show();
      return;
    }
    // A broken install should not spam notifications on every timer tick, so
    // the detail goes to the output channel and the status bar just flags it.
    statusItem.text = "$(warning) Claude: error";
    statusItem.tooltip = String(err instanceof Error ? err.message : err);
    statusItem.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground");
    statusItem.show();
    output.appendLine(`[status] ${err instanceof Error ? err.message : String(err)}`);
  }
}

interface ProfileItem extends vscode.QuickPickItem {
  profileName: string;
}

/**
 * Describes a profile's token state, or nothing when there is nothing worth
 * saying.
 *
 * Only the active profile's expiry reflects the credentials Claude Code is
 * actually using. Every parked profile holds a snapshot taken at capture time,
 * whose access token is expected to be expired, and reporting that as a problem
 * previously told users to run /login, which would destroy the refresh token
 * that makes the profile usable at all.
 */
function profileExpiryNote(p: ccm.Profile): string | undefined {
  if (!p.expiryIsLive) {
    return undefined;
  }
  return p.expired ? "access token lapsed, Claude Code will refresh it" : undefined;
}

async function switchAccount(): Promise<void> {
  let listed;
  try {
    listed = await ccm.list();
  } catch (err) {
    await reportError(err, "list accounts");
    return;
  }

  const profiles = listed.profiles ?? [];
  if (profiles.length === 0) {
    const pick = await vscode.window.showInformationMessage(
      "No Claude Code profiles saved yet. Sign in with /login, then capture the account.",
      "Capture current login"
    );
    if (pick) {
      await captureAccount();
    }
    return;
  }

  const items: ProfileItem[] = profiles.map((p) => ({
    profileName: p.name,
    label: p.active ? `$(check) ${p.name}` : `$(account) ${p.name}`,
    description: p.email ?? "",
    detail: [
      p.organization ? `org: ${p.organization}` : undefined,
      p.subscription ? `plan: ${p.subscription}` : undefined,
      profileExpiryNote(p)
    ]
      .filter(Boolean)
      .join("   ")
  }));

  const chosen = await vscode.window.showQuickPick(items, {
    title: "Switch Claude Code account",
    placeHolder: `Active: ${listed.status.emailAddress ?? "none"}`,
    matchOnDescription: true
  });
  if (!chosen) {
    return;
  }

  const active = profiles.find((p) => p.active);
  if (active && active.name === chosen.profileName) {
    vscode.window.showInformationMessage(`${chosen.profileName} is already the active account.`);
    return;
  }

  await performSwitch(chosen.profileName, false);
}

async function performSwitch(name: string, force: boolean): Promise<void> {
  try {
    const res = await vscode.window.withProgress(
      { location: vscode.ProgressLocation.Notification, title: `Switching to ${name}…` },
      () => ccm.use(name, force)
    );

    const parts = [`Switched to ${res.to}${res.toEmail ? ` (${res.toEmail})` : ""}.`];
    if (res.capturedAs) {
      parts.push(
        res.capturedNew
          ? `Previous account saved as new profile "${res.capturedAs}".`
          : `Previous account's tokens refreshed into "${res.capturedAs}".`
      );
    }
    output.appendLine(`[switch] ${parts.join(" ")}`);
    void refreshStatus();

    // Claude Code reads credentials at startup, so an already-running
    // extension host keeps the previous account until the window reloads.
    const choice = await vscode.window.showInformationMessage(
      `${parts.join(" ")} Reload the window for Claude Code to pick it up.`,
      "Reload Window",
      "Later"
    );
    if (choice === "Reload Window") {
      await vscode.commands.executeCommand("workbench.action.reloadWindow");
    }
  } catch (err) {
    if (err instanceof ccm.CcmError && /Claude Code is running/i.test(err.message)) {
      const choice = await vscode.window.showWarningMessage(
        "Claude Code is running. Switching now would be undone when it exits.",
        { modal: true, detail: err.message },
        "Switch anyway"
      );
      if (choice === "Switch anyway") {
        await performSwitch(name, true);
      }
      return;
    }
    await reportError(err, `switch to ${name}`);
  }
}

const renameButton: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("edit"),
  tooltip: "Rename this profile"
};

const removeButton: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("trash"),
  tooltip: "Remove this profile"
};

/**
 * A single place to switch, rename and remove profiles.
 *
 * Built on createQuickPick rather than showQuickPick because only the former
 * supports per-item buttons. That matters: renaming from a tray menu is
 * impossible since tray menus cannot take text input, so this panel is the
 * management surface for anyone who would rather not use the CLI.
 */
async function manageAccounts(): Promise<void> {
  let listed;
  try {
    listed = await ccm.list();
  } catch (err) {
    await reportError(err, "list accounts");
    return;
  }

  const profiles = listed.profiles ?? [];
  if (profiles.length === 0) {
    const pick = await vscode.window.showInformationMessage(
      "No Claude Code profiles saved yet. Sign in with /login, then capture the account.",
      "Capture current login"
    );
    if (pick) {
      await captureAccount();
    }
    return;
  }

  const picker = vscode.window.createQuickPick<ProfileItem>();
  picker.title = "Manage Claude Code accounts";
  picker.placeholder = "Pick an account to switch to, or use the buttons to rename or remove";
  picker.matchOnDescription = true;
  picker.items = profiles.map((p) => ({
    profileName: p.name,
    label: p.active ? `$(check) ${p.name}` : `$(account) ${p.name}`,
    description: p.email ?? "",
    detail: [
      p.organization ? `org: ${p.organization}` : undefined,
      p.subscription ? `plan: ${p.subscription}` : undefined,
      profileExpiryNote(p),
      p.active ? "currently active" : undefined
    ]
      .filter(Boolean)
      .join("   "),
    buttons: [renameButton, removeButton]
  }));

  const done = new Promise<void>((resolve) => {
    picker.onDidTriggerItemButton(async (e) => {
      picker.hide();
      if (e.button === renameButton) {
        await renameProfile(e.item.profileName);
      } else {
        await removeProfile(e.item.profileName);
      }
      resolve();
    });

    picker.onDidAccept(async () => {
      const chosen = picker.selectedItems[0];
      picker.hide();
      if (chosen) {
        const active = profiles.find((p) => p.active);
        if (active && active.name === chosen.profileName) {
          vscode.window.showInformationMessage(`${chosen.profileName} is already the active account.`);
        } else {
          await performSwitch(chosen.profileName, false);
        }
      }
      resolve();
    });

    picker.onDidHide(() => {
      picker.dispose();
      resolve();
    });
  });

  picker.show();
  await done;
}

/** Prompts for a profile when name is omitted, then for the new name. */
async function renameProfile(name?: string): Promise<void> {
  const target = name ?? (await pickProfileName("Rename which profile?"));
  if (!target) {
    return;
  }

  const next = await vscode.window.showInputBox({
    title: `Rename "${target}"`,
    prompt: "New name for this profile. The stored credentials are kept.",
    value: target,
    validateInput: (v) => {
      const t = v.trim();
      if (t.length === 0) {
        return "The name cannot be empty";
      }
      if (t !== v) {
        return "The name cannot start or end with a space";
      }
      if (t.startsWith("-")) {
        return "The name cannot start with a dash";
      }
      if (/\s/.test(t)) {
        return "The name cannot contain spaces";
      }
      return undefined;
    }
  });
  if (!next || next === target) {
    return;
  }

  try {
    await ccm.rename(target, next);
    vscode.window.showInformationMessage(`Renamed "${target}" to "${next}".`);
    void refreshStatus();
  } catch (err) {
    await reportError(err, `rename ${target}`);
  }
}

async function removeProfile(name?: string): Promise<void> {
  const target = name ?? (await pickProfileName("Remove which profile?"));
  if (!target) {
    return;
  }

  // Modal, because this is the one destructive action here. Removing a parked
  // profile discards the only stored copy of its refresh token, and getting it
  // back means signing into that account through a browser again.
  const confirmed = await vscode.window.showWarningMessage(
    `Remove the profile "${target}"?`,
    {
      modal: true,
      detail:
        "This discards the stored credentials for that account. If it is not the account " +
        "you are currently signed into, you will need to sign in through a browser again to restore it."
    },
    "Remove"
  );
  if (confirmed !== "Remove") {
    return;
  }

  try {
    await ccm.remove(target);
    vscode.window.showInformationMessage(`Removed "${target}".`);
    void refreshStatus();
  } catch (err) {
    await reportError(err, `remove ${target}`);
  }
}

async function pickProfileName(title: string): Promise<string | undefined> {
  let listed;
  try {
    listed = await ccm.list();
  } catch (err) {
    await reportError(err, "list accounts");
    return undefined;
  }
  const profiles = listed.profiles ?? [];
  if (profiles.length === 0) {
    vscode.window.showInformationMessage("No Claude Code profiles saved yet.");
    return undefined;
  }
  const chosen = await vscode.window.showQuickPick(
    profiles.map((p) => ({
      profileName: p.name,
      label: p.name,
      description: p.email ?? "",
      detail: p.active ? "currently active" : undefined
    })),
    { title, matchOnDescription: true }
  );
  return chosen?.profileName;
}

async function captureAccount(): Promise<void> {
  const name = await vscode.window.showInputBox({
    title: "Capture the current Claude Code login",
    prompt: "Name for this profile (leave blank to derive it from the account email)",
    placeHolder: "work"
  });
  if (name === undefined) {
    return;
  }

  try {
    const res = await ccm.capture(name.trim() || undefined);
    vscode.window.showInformationMessage(
      `Captured ${res.email ?? "the current account"} as profile "${res.captured}".`
    );
    void refreshStatus();
  } catch (err) {
    await reportError(err, "capture the current login");
  }
}

async function showDoctor(): Promise<void> {
  try {
    const text = await ccm.doctor();
    output.clear();
    output.appendLine(text);
    output.show(true);
  } catch (err) {
    await reportError(err, "run doctor");
  }
}

async function reportError(err: unknown, action: string): Promise<void> {
  if (err instanceof ccm.BinaryMissingError) {
    const choice = await vscode.window.showErrorMessage(
      `Could not ${action}: the ccm executable was not found (${err.binary}).`,
      "Open Settings",
      "Installation Help"
    );
    if (choice === "Open Settings") {
      await vscode.commands.executeCommand("workbench.action.openSettings", "ccm.binaryPath");
    } else if (choice === "Installation Help") {
      await vscode.env.openExternal(
        vscode.Uri.parse("https://github.com/MAbbasRaza/claude-code-credential-manager#install")
      );
    }
    return;
  }

  const message = err instanceof Error ? err.message : String(err);
  output.appendLine(`[error] ${action}: ${message}`);
  const choice = await vscode.window.showErrorMessage(
    `Could not ${action}: ${message.split("\n")[0]}`,
    "Show Details"
  );
  if (choice === "Show Details") {
    output.show(true);
  }
}
