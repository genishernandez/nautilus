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
  needsProgramMode,
  onlineEditConfirmMessage,
  readDeployStream,
  refusalButtons,
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

test("the download modal says the controller stops, what happens to the values, and that it stays in Program", () => {
  const msg = downloadConfirmMessage(INFO, "MainProgram.ld");
  assert.match(msg, /^Download MainProgram\.ld to DemoLine at AB_ETH-1/);
  assert.match(msg, /STOPS THE CONTROLLER/);
  assert.match(msg, /tag values are kept/);
  assert.match(msg, /presets come from the project/);
  assert.doesNotMatch(msg, /tags are reset/);
  assert.match(msg, /never switches it back to Run/);
  const reset = downloadConfirmMessage(INFO, "MainProgram.ld", true);
  assert.match(reset, /tags are reset to the project's values \(nautilus\.logix\.resetValuesOnDownload is on\)/);
  assert.doesNotMatch(reset, /kept/);
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
  // An online edit's done has no valuesKept/resets; an older naut sent -1 and [].
  assert.equal(doneSummary("online", INFO, { event: "done", applied: "online edit", verified: true, replaced: 2, mode: "Run", elapsedMs: 2100, valuesKept: -1, resets: [] }), online);
  const dl = doneSummary("download", INFO, { event: "done", applied: "download", verified: true, mode: "Program", elapsedMs: 97_000 });
  assert.match(dl, /downloaded to DemoLine and verified in 97 s/);
  assert.match(dl, /in Program mode; put it in Run/);
  assert.doesNotMatch(dl, /kept/); // an older naut says nothing about values
  const kept = doneSummary("download", INFO, {
    event: "done", applied: "download", verified: true, mode: "Program", elapsedMs: 120_000,
    valuesKept: 11, resets: ["SpareSP: DINT → REAL"],
  });
  assert.equal(
    kept,
    "nautilus: downloaded to DemoLine and verified in 120 s, 11 tag values kept, reset to the project's: SpareSP: DINT → REAL — the controller is in Program mode; put it in Run when the process is ready"
  );
  assert.match(doneSummary("download", INFO, { event: "done", verified: true, valuesKept: -1 }), /, tags at the project's values — /);
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

/** A stream the test feeds by hand: read() waits for the next push. */
function handStream() {
  const queue: (string | undefined)[] = [];
  let wake: (() => void) | undefined;
  return {
    push(chunk: string | undefined) {
      queue.push(chunk);
      wake?.();
    },
    async read(): Promise<string | undefined> {
      while (queue.length === 0) await new Promise<void>((r) => (wake = r));
      wake = undefined;
      return queue.shift();
    },
  };
}

test("readDeployStream answers with a download's result before its stream ends", async () => {
  const s = handStream();
  const seen: [string, boolean][] = [];
  s.push('{"event":"progress","message":"downloaded"}\n{"event":"progress","message":"verified"}\n');
  s.push('{"event":"done","applied":"download","verified":true,"elapsedMs":120000}\n{"event":"progress","message":"opening the online session for the next edit"}\n');
  // Nothing more yet: the session is still opening. The result must not
  // wait for it (#379: the summary came ~50 s late).
  const { result, rest } = await readDeployStream(s.read, (m, after) => seen.push([m, after]));
  assert.equal(result?.event, "done");
  assert.deepEqual(seen, [
    ["downloaded", false],
    ["verified", false],
    ["opening the online session for the next edit", true],
  ]);
  let ended = false;
  void rest.then(() => (ended = true));
  s.push('{"event":"progress","message":"session open and online"}');
  s.push(undefined);
  await rest;
  assert.equal(ended, true);
  assert.deepEqual(seen[3], ["session open and online", true]);
});

test("readDeployStream: an error is the result; a stream with none ends with none", async () => {
  const s = handStream();
  s.push('{"event":"progress","message":"starting a download"}\n{"event":"error","status":409,"error":"in Run","needsProgramMode":true}\n');
  s.push(undefined);
  const { result } = await readDeployStream(s.read, () => {});
  assert.deepEqual(result, { event: "error", status: 409, error: "in Run", needsProgramMode: true });

  const t = handStream();
  const seen: string[] = [];
  t.push('{"event":"progress","message":"a"}\n{"event":"progress","mess');
  t.push('age":"b"}');
  t.push(undefined);
  const { result: none, rest } = await readDeployStream(t.read, (m) => seen.push(m));
  await rest;
  assert.equal(none, undefined);
  assert.deepEqual(seen, ["a", "b"]);
});

test("needsProgramMode: the server's flag, or an older server's bare SDK text", () => {
  assert.equal(needsProgramMode({ event: "error", status: 409, error: "the controller is in Run mode…", needsProgramMode: true }), true);
  assert.equal(
    needsProgramMode({
      event: "error",
      status: 502,
      error: "download: logixd: operation_failed (OperationFailedException): RxE_INVALID_START_STATE - Invalid start state.  Internal error.",
    }),
    true
  );
  assert.equal(needsProgramMode({ event: "error", status: 422, error: "the tag set changed; a download is needed" }), false);
  assert.equal(needsProgramMode({ event: "error", error: "fetch failed" }), false);
});

test("a summary's time is the whole wait the server reports", () => {
  // The first edit of a session: 47 s opening it, 3 s editing.
  const msg = doneSummary("online", INFO, { event: "done", applied: "online edit", verified: true, replaced: 2, mode: "Run", elapsedMs: 50_200, deployMs: 3_000 });
  assert.match(msg, / in 50 s /);
  assert.doesNotMatch(msg, /3\.0 s/);
});

test("a plain download refused in Run offers the Program-mode download; nothing else does", () => {
  const inRun = { event: "error" as const, status: 409, error: "the controller is in Run mode…", needsProgramMode: true };
  assert.deepEqual(refusalButtons("download", inRun), ["Switch to Program Mode and Download", "Show Output"]);
  // Already asked for Program mode: offering it again is no way out.
  assert.deepEqual(refusalButtons("download", inRun, true), ["Show Output"]);
  assert.deepEqual(refusalButtons("download", { event: "error", status: 502, error: "verification failed" }), ["Show Output"]);
  // The online edit's ways out are as they were.
  assert.deepEqual(refusalButtons("online", { event: "error", status: 422, error: "the tag set changed; a download is needed" }), [
    "Download Project…",
    "Show Output",
  ]);
  assert.deepEqual(refusalButtons("online", { event: "error", status: 409, error: "controller program changed since your base" }), [
    "Push Anyway",
    "Show Output",
  ]);
});
