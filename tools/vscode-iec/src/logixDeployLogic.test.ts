// Plain-Node tests for the Logix deploy commands' pure half (no vscode
// dependency). Run via `npm test`.

import { test } from "node:test";
import * as assert from "node:assert/strict";
import {
  declaresLogixTarget,
  doneSummary,
  downloadConfirmMessage,
  NdjsonSplitter,
  needsDownload,
  onlineEditConfirmMessage,
  serveCliArgs,
  shellLine,
} from "./logixDeployLogic";

const INFO = { target: "logix", controller: "DemoLine", commPath: "AB_ETH-1\\10.0.0.5\\Backplane\\0", hash: "a1b2c3d4e5f6" };

test("declaresLogixTarget finds target: logix in block and flow form", () => {
  const block = "name: demoline\ntasks:\n  - program: MainProgram.ld\ntarget:\n  # the bench controller\n\n  logix:\n    controller: DemoLine\n";
  assert.equal(declaresLogixTarget(block), true);
  assert.equal(declaresLogixTarget("name: x\ntarget: { logix: { controller: L } }\n"), true);
  assert.equal(declaresLogixTarget("target:\r\n  logix:\r\n    controller: L\r\n"), true);
});

test("declaresLogixTarget ignores logix: outside the target block", () => {
  assert.equal(declaresLogixTarget("name: x\ntasks:\n  - program: a.st\n"), false);
  assert.equal(declaresLogixTarget("target:\n  other: {}\nlogix:\n  x: 1\n"), false);
  assert.equal(declaresLogixTarget("drivers:\n  logix:\n    host: 1.2.3.4\n"), false);
  assert.equal(declaresLogixTarget("# target:\n#   logix:\n"), false);
});

test("the online edit modal names the file, the controller, its path and what it runs", () => {
  const msg = onlineEditConfirmMessage(INFO, "MainProgram.ld");
  assert.match(msg, /MainProgram\.ld/);
  assert.match(msg, /DemoLine at AB_ETH-1\\10\.0\.0\.5/);
  assert.match(msg, /ONLINE EDIT/);
  assert.match(msg, /stays in its mode/);
  assert.match(msg, /a1b2c3d4e5f6/);
});

test("the download modal says the controller stops, resets its tags and stays in Program", () => {
  const msg = downloadConfirmMessage(INFO, "MainProgram.ld");
  assert.match(msg, /^Download MainProgram\.ld to DemoLine at AB_ETH-1/);
  assert.match(msg, /STOPS THE CONTROLLER/);
  assert.match(msg, /tags are reset/);
  assert.match(msg, /never switches it back to Run/);
  assert.match(downloadConfirmMessage({ hash: "x" }, "p.ld"), /to the Logix controller\?/);
});

test("NdjsonSplitter reassembles lines split across chunks", () => {
  const s = new NdjsonSplitter();
  assert.deepEqual(s.feed('{"event":"progress","mess'), []);
  assert.deepEqual(s.feed('age":"imported and built"}\n{"event":"done","applied":"download","verified":true}\n'), [
    { event: "progress", message: "imported and built" },
    { event: "done", applied: "download", verified: true },
  ]);
  assert.deepEqual(s.feed('{"event":"error","status":422,"error":"needs a download"}'), []);
  assert.deepEqual(s.flush(), [{ event: "error", status: 422, error: "needs a download" }]);
});

test("NdjsonSplitter turns a plain error body into an error event and keeps stray text", () => {
  const s = new NdjsonSplitter();
  assert.deepEqual(s.feed('{"error":"a deploy is already in progress"}\nnot json\n'), [
    { event: "error", error: "a deploy is already in progress" },
    { event: "progress", message: "not json" },
  ]);
});

test("doneSummary says what happened, verified or not", () => {
  const online = doneSummary("online", INFO, { event: "done", applied: "online edit", verified: true, replaced: 2, mode: "Run", elapsedMs: 2100 });
  assert.equal(online, "nautilus: online edit live on DemoLine and verified, 2 rungs replaced, controller still in Run in 2.1 s — commit the file to keep it");
  const dl = doneSummary("download", INFO, { event: "done", applied: "download", verified: true, mode: "Program", elapsedMs: 97_000 });
  assert.match(dl, /downloaded to DemoLine and verified in 97 s/);
  assert.match(dl, /in Program mode; put it in Run/);
  assert.match(doneSummary("online", INFO, { event: "done", verified: false, replaced: 1 }), /\(NOT verified\), 1 rung replaced/);
  assert.match(doneSummary("online", INFO, { event: "done", same: true }), /already runs this logic/);
});

test("needsDownload recognises the refusal that a download answers", () => {
  assert.equal(needsDownload({ event: "error", status: 422, error: "the tag set changed (1 difference(s)); ... a download is needed. Download the project" }), true);
  assert.equal(needsDownload({ event: "error", status: 422, error: "outside the Logix v1 subset:\nx (download)" }), false);
  assert.equal(needsDownload({ event: "error", status: 502, error: "logixd: unreachable" }), false);
});

test("serveCliArgs serves the project where nautilus.runtimeUrl points", () => {
  assert.deepEqual(serveCliArgs("/w/demoline", "http://localhost:18080/"), ["logix", "serve", "--project", "/w/demoline", "--listen", "127.0.0.1:18080"]);
  assert.deepEqual(serveCliArgs("/w/d", "not a url").slice(-1), ["127.0.0.1:8080"]);
});

test("shellLine quotes what a shell would split", () => {
  assert.equal(shellLine("naut", ["logix", "serve", "--project", "/w/demo line"]), "naut logix serve --project '/w/demo line'");
  assert.equal(shellLine("C:\\Program Files\\naut.exe", ["logix", "C:\\w\\demo"], true), '& "C:\\Program Files\\naut.exe" logix C:\\w\\demo');
});
