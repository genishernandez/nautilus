// "nautilus: Create Project…" — wraps `naut new <name> --no-input --template
// <template>` for the Getting Started walkthrough, so a first run needs no
// terminal.
//
// This is a thin wrapper, not a reimplementation: `--no-input` is the CLI's
// own non-interactive path (the same one CI/docker one-liners use), so this
// command supplies the same answers the interactive form would ask for
// through a couple of quick picks, then runs the CLI exactly as documented.
// It's worth having because `naut new` is cheap (it only writes files and
// `git init`s) and fully scriptable — there's no reason a first-time user
// needs a terminal open before they've seen anything run.
//
// The reusable pure logic (name validation, the template list, argv
// building) lives in newProjectLogic.ts, which has no vscode import and is
// plain-Node testable.

import { execFile } from "node:child_process";
import * as os from "node:os";
import * as vscode from "vscode";
import { cliCommand, cliExecOptions, cliMissingMessage, isMissing } from "./cli";
import {
  NEW_PROJECT_LANGUAGES,
  NEW_PROJECT_TEMPLATES,
  newProjectArgs,
  parentFolderProblem,
  resolveParentInput,
  templateTakesLanguage,
  validateProjectName,
} from "./newProjectLogic";

/** `naut new` itself is fast (file writes + `git init`), but give it real
 * room on a slow disk or an antivirus scanner in the way. */
export const NEW_PROJECT_TIMEOUT_MS = 30_000;

export function registerNewProjectCommand(): vscode.Disposable {
  return vscode.commands.registerCommand("nautilus.newProject", async () => {
    const parent = await pickParentFolder();
    if (!parent) return;

    // Every step after the first ignores focus-out. When the parent-folder box
    // closes, VS Code hands focus back to whatever had it before the command
    // ran; on a fresh window that is the Welcome page, whose step media is a
    // webview that takes focus a moment later and would close a focus-sensitive
    // step as soon as it opened (rig smoke 17, palette flow on a cold window).
    const name = await vscode.window.showInputBox({
      title: "nautilus: Create Project",
      prompt: "Directory and program name for the new controller",
      placeHolder: "water-plant",
      validateInput: validateProjectName,
      ignoreFocusOut: true,
    });
    if (!name) return;

    const template = await vscode.window.showQuickPick(NEW_PROJECT_TEMPLATES, {
      title: "nautilus: Create Project — template",
      placeHolder: "Demo is the fastest way to see everything working",
      ignoreFocusOut: true,
    });
    if (!template) return;

    let language: string | undefined;
    if (templateTakesLanguage(template.template)) {
      const picked = await vscode.window.showQuickPick(NEW_PROJECT_LANGUAGES, {
        title: "nautilus: Create Project — program language",
        placeHolder: "The language of the first program (you can add the others later)",
        ignoreFocusOut: true,
      });
      if (!picked) return;
      language = picked.language;
    }

    await createProject(parent, name.trim(), template.template, language);
  });
}

/** The parent-folder step is an input box (default folder prefilled, Enter
 * accepts, a folder button opens the system picker), NOT showOpenDialog:
 * with `files.simpleDialog.enable` (VS Code for the web, serve-web, remote
 * setups) the open dialog is a plain filter input where Enter navigates
 * instead of confirming "Create project here" (#86), and the extension has
 * no hook into that. An input box behaves the same in every dialog mode. */
async function pickParentFolder(): Promise<vscode.Uri | undefined> {
  const home = os.homedir();
  const base = vscode.workspace.workspaceFolders?.[0]?.uri.scheme === "file"
    ? vscode.workspace.workspaceFolders[0].uri.fsPath
    : home;
  const browse: vscode.QuickInputButton = {
    iconPath: new vscode.ThemeIcon("folder-opened"),
    tooltip: "Browse for a folder…",
  };
  const problemFor = async (text: string): Promise<string | undefined> => {
    const resolved = resolveParentInput(text, home, base);
    if (!resolved) return "choose a folder";
    let kind: "dir" | "file" | "missing";
    try {
      const st = await vscode.workspace.fs.stat(vscode.Uri.file(resolved));
      kind = st.type & vscode.FileType.Directory ? "dir" : "file";
    } catch {
      kind = "missing";
    }
    return parentFolderProblem(kind);
  };

  const box = vscode.window.createInputBox();
  box.title = "nautilus: Create Project — parent folder";
  box.prompt = "Folder to create the project in (Enter to accept, or browse)";
  box.value = base;
  box.buttons = [browse];
  box.ignoreFocusOut = true;
  return new Promise((resolve) => {
    let done = false;
    const finish = (uri: vscode.Uri | undefined) => {
      if (done) return;
      done = true;
      box.dispose();
      resolve(uri);
    };
    let seq = 0;
    box.onDidChangeValue(async (text) => {
      const mine = ++seq;
      const problem = await problemFor(text);
      if (mine === seq) box.validationMessage = problem;
    });
    box.onDidAccept(async () => {
      const problem = await problemFor(box.value);
      if (problem) {
        box.validationMessage = problem;
        return;
      }
      finish(vscode.Uri.file(resolveParentInput(box.value, home, base)!));
    });
    box.onDidTriggerButton(async () => {
      const picked = await vscode.window.showOpenDialog({
        canSelectFiles: false,
        canSelectFolders: true,
        canSelectMany: false,
        defaultUri: vscode.Uri.file(resolveParentInput(box.value, home, base) ?? base),
        openLabel: "Create project here",
        title: "nautilus: choose a parent folder for the new project",
      });
      if (picked?.[0]) finish(picked[0]);
    });
    box.onDidHide(() => finish(undefined));
    box.show();
  });
}

async function createProject(parent: vscode.Uri, name: string, template: string,
  language?: string
): Promise<void> {
  const cli = cliCommand();
  const args = newProjectArgs(name, template, language);
  const projectUri = vscode.Uri.joinPath(parent, name);

  const ok = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: `nautilus: creating ${name}…` },
    () =>
      new Promise<boolean>((resolve) => {
        execFile(
          cli,
          args,
          cliExecOptions({ cwd: parent.fsPath, timeoutMs: NEW_PROJECT_TIMEOUT_MS }),
          (err, _stdout, stderr) => {
            if (err) {
              if (isMissing(err)) {
                void vscode.window.showErrorMessage(cliMissingMessage(cli));
              } else {
                void vscode.window.showErrorMessage(
                  `nautilus: naut new failed — ${stderr.trim() || err.message}`
                );
              }
              resolve(false);
              return;
            }
            resolve(true);
          }
        );
      })
  );
  if (!ok) return;

  const OPEN = "Open in New Window";
  const OPEN_HERE = "Open Here";
  const actions = vscode.workspace.workspaceFolders?.length ? [OPEN, OPEN_HERE] : [OPEN_HERE, OPEN];
  const pick = await vscode.window.showInformationMessage(`nautilus: created ${name}/`, ...actions);
  if (pick === OPEN) {
    await vscode.commands.executeCommand("vscode.openFolder", projectUri, { forceNewWindow: true });
  } else if (pick === OPEN_HERE) {
    await vscode.commands.executeCommand("vscode.openFolder", projectUri, { forceNewWindow: false });
  }
}
