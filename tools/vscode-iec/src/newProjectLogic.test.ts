// Plain-Node tests for the "nautilus: Create Project…" command's pure
// logic (no vscode dependency — the actual execFile call in newProject.ts
// isn't worth mocking, cliInstall.test.ts-style).

import { test } from "node:test";
import * as assert from "node:assert/strict";
import { NEW_PROJECT_LANGUAGES, NEW_PROJECT_TEMPLATES, templateTakesLanguage, newProjectArgs, validateProjectName, resolveParentInput, parentFolderProblem } from "./newProjectLogic";

test("validateProjectName: rejects empty/whitespace", () => {
  assert.equal(validateProjectName(""), "a name is required");
  assert.equal(validateProjectName("   "), "a name is required");
});

test("validateProjectName: rejects spaces and slashes, matching cmd/naut/new.go", () => {
  assert.equal(validateProjectName("water plant"), "no spaces or slashes");
  assert.equal(validateProjectName("water/plant"), "no spaces or slashes");
  assert.equal(validateProjectName("water\\plant"), "no spaces or slashes");
});

test("validateProjectName: accepts an ordinary name", () => {
  assert.equal(validateProjectName("water-plant"), undefined);
  assert.equal(validateProjectName("  water-plant  "), undefined);
});

test("newProjectArgs: builds naut new's non-interactive argv", () => {
  assert.deepEqual(newProjectArgs("water-plant", "demo"), [
    "new",
    "water-plant",
    "--no-input",
    "--template",
    "demo",
  ]);
});

test("NEW_PROJECT_TEMPLATES: covers every template naut new supports, demo first", () => {
  assert.deepEqual(
    NEW_PROJECT_TEMPLATES.map((t) => t.template),
    ["demo", "minimal", "sdk", "sdk-demo"]
  );
});

test("newProjectArgs: passes --language for Minimal and SDK", () => {
  assert.deepEqual(newProjectArgs("p", "minimal", "ld"), [
    "new", "p", "--no-input", "--template", "minimal", "--language", "ld",
  ]);
  assert.deepEqual(newProjectArgs("p", "sdk", "sfc").slice(-2), ["--language", "sfc"]);
});

test("newProjectArgs: Demo and SDK demo ignore the language", () => {
  assert.ok(!newProjectArgs("p", "demo", "ld").includes("--language"));
  assert.ok(!newProjectArgs("p", "sdk-demo", "ld").includes("--language"));
  assert.ok(!newProjectArgs("p", "minimal").includes("--language"));
});

test("templateTakesLanguage / NEW_PROJECT_LANGUAGES: the four languages naut new accepts", () => {
  assert.deepEqual(NEW_PROJECT_LANGUAGES.map((l) => l.language), ["st", "ld", "fbd", "sfc"]);
  assert.deepEqual(NEW_PROJECT_LANGUAGES.map((l) => l.label), [
    "Structured Text", "Ladder Diagram", "Function Block Diagram", "Sequential Function Chart",
  ]);
  assert.equal(templateTakesLanguage("minimal"), true);
  assert.equal(templateTakesLanguage("demo"), false);
});

test("resolveParentInput: empty is undefined; absolute paths pass through trimmed/unquoted", () => {
  assert.equal(resolveParentInput("  ", "/h", "/b"), undefined);
  assert.equal(resolveParentInput(" /srv/plants ", "/h", "/b"), "/srv/plants");
  assert.equal(resolveParentInput('"/srv/my plants"', "/h", "/b"), "/srv/my plants");
  assert.equal(resolveParentInput("C:\\work", "/h", "/b"), "C:\\work");
});

test("resolveParentInput: ~ expands to home, relative resolves against the default folder", () => {
  assert.equal(resolveParentInput("~", "/home/j", "/b"), "/home/j");
  assert.equal(resolveParentInput("~/dev", "/home/j", "/b"), "/home/j/dev");
  assert.equal(resolveParentInput("sub", "/home/j", "/b/"), "/b/sub");
});

test("parentFolderProblem: only an existing directory is accepted", () => {
  assert.equal(parentFolderProblem("dir"), undefined);
  assert.equal(parentFolderProblem("file"), "that is a file, not a folder");
  assert.equal(parentFolderProblem("missing"), "no such folder");
});
