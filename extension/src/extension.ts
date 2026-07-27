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
      p.expired ? "access token expired (a /login may be needed)" : undefined
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
