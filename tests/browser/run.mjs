// Dependency-free browser regression runner. See docs/testing.md.
import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {once} from "node:events";
import {mkdtemp, readFile, rm} from "node:fs/promises";
import {createServer, request} from "node:http";
import {tmpdir} from "node:os";
import {dirname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";

const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, "../..");
const temporary = await mkdtemp(join(tmpdir(), "almanaut-browser-"));
const children = [];
let proxy, client, cleanupPromise;

function child(command, args) {
  const process = spawn(command, args, {cwd: root, stdio: ["ignore", "pipe", "pipe"]});
  children.push(process);
  // Keep startup errors available even when they precede the caller's wait.
  process.finished = new Promise((resolve) => {
    process.once("error", (error) => resolve({error}));
    process.once("close", (code) => resolve({code}));
  });
  process.diagnostics = "";
  process.stderr.on("data", (chunk) => {
    process.diagnostics = (process.diagnostics + chunk).slice(-16000);
  });
  return process;
}

async function ready(process, stream, pattern) {
  let buffer = "";
  let onData;
  const output = new Promise((resolve) => {
    onData = (chunk) => {
      buffer += chunk;
      const match = buffer.match(pattern);
      if (match) resolve(match[1]);
    };
    stream.on("data", onData);
  });
  try {
    return await Promise.race([output, process.finished.then(({error, code}) => {
      throw new Error(`Process stopped before startup (${error?.message ?? code}).\n${process.diagnostics}`);
    })]);
  } finally {
    stream.off("data", onData);
  }
}

class DevTools {
  constructor(socket) {
    this.socket = socket;
    this.nextID = 0;
    this.pending = new Map();
    socket.addEventListener("message", ({data}) => {
      const message = JSON.parse(data);
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      clearTimeout(pending.timer);
      if (message.error) pending.reject(new Error(JSON.stringify(message.error)));
      else pending.resolve(message.result);
    });
    socket.addEventListener("close", () => this.rejectPending("Chromium disconnected"));
  }
  rejectPending(message) {
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(new Error(message));
    }
    this.pending.clear();
  }
  send(method, params = {}, sessionId) {
    const id = ++this.nextID;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`DevTools timed out: ${method}`));
      }, 30000);
      this.pending.set(id, {resolve, reject, timer});
      this.socket.send(JSON.stringify({id, method, params, sessionId}));
    });
  }
  close() {
    this.rejectPending("Browser test finished");
    this.socket.close();
  }
}

async function cleanup() {
  cleanupPromise ??= (async () => {
    client?.close();
    proxy?.closeAllConnections();
    if (proxy?.listening) await new Promise((resolve) => proxy.close(resolve));
    for (const process of children.reverse()) {
      if (process.exitCode !== null || process.signalCode !== null) continue;
      process.kill("SIGTERM");
      const timer = setTimeout(() => process.kill("SIGKILL"), 3000);
      await process.finished;
      clearTimeout(timer);
    }
    await rm(temporary, {recursive: true, force: true});
  })();
  return cleanupPromise;
}

for (const [signal, code] of [["SIGINT", 130], ["SIGTERM", 143]]) {
  process.once(signal, () => {
    cleanup().finally(() => process.exit(code));
  });
}
const deadline = setTimeout(() => {
  console.error("Browser tests exceeded the 120-second deadline.");
  cleanup().finally(() => process.exit(1));
}, 120000);

try {
  assert.equal(typeof WebSocket, "function", "Node.js 22.4 or newer is required for its built-in WebSocket client.");
  const binary = join(temporary, "server");
  const build = child("go", ["build", "-o", binary, "./tests/browser/server"]);
  const built = await build.finished;
  assert.equal(built.code, 0, `Could not build browser-test server: ${built.error?.message ?? build.diagnostics}`);
  const backend = child(binary, [temporary]);
  const upstream = await ready(backend, backend.stdout, /^(http:\/\/127\.0\.0\.1:\d+)\s/m);
  const fixture = await readFile(join(directory, "fixture.html"));
  proxy = createServer((incoming, outgoing) => {
    if (incoming.url === "/browser-fixture") {
      outgoing.writeHead(200, {"Content-Type": "text/html; charset=utf-8"});
      outgoing.end(fixture);
      return;
    }
    // Keep fixtures and the real app on one origin without changing its CSP.
    const forwarded = request(new URL(incoming.url, upstream), {method: incoming.method, headers: incoming.headers}, (response) => {
      outgoing.writeHead(response.statusCode, response.headers);
      response.pipe(outgoing);
    });
    forwarded.on("error", (error) => {
      outgoing.writeHead(502);
      outgoing.end(error.message);
    });
    incoming.pipe(forwarded);
  });
  proxy.listen(0, "127.0.0.1");
  await once(proxy, "listening");
  const origin = `http://127.0.0.1:${proxy.address().port}`;
  const args = ["--headless", "--disable-gpu", "--disable-dev-shm-usage", "--no-first-run",
    "--no-default-browser-check", "--disable-background-networking", "--disable-component-update",
    "--disable-sync", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0",
    `--user-data-dir=${join(temporary, "chromium-profile")}`, "about:blank"];
  if (process.env.CHROMIUM_NO_SANDBOX === "1") args.unshift("--no-sandbox");
  const browser = child(process.env.CHROMIUM || "chromium", args);
  const endpoint = await ready(browser, browser.stderr, /DevTools listening on (ws:\/\/\S+)/);
  const socket = new WebSocket(endpoint);
  await once(socket, "open");
  client = new DevTools(socket);
  const {targetId} = await client.send("Target.createTarget", {url: "about:blank"});
  const {sessionId} = await client.send("Target.attachToTarget", {targetId, flatten: true});
  const evaluate = async (expression) => {
    const reply = await client.send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true}, sessionId);
    if (reply.exceptionDetails) throw new Error(reply.exceptionDetails.exception?.description || reply.exceptionDetails.text);
    return reply.result.value;
  };
  async function navigate(path) {
    await client.send("Page.navigate", {url: `${origin}${path}`}, sessionId);
    for (let attempt = 0; ; attempt++) {
      if (await evaluate(`location.pathname === ${JSON.stringify(path)} && document.readyState === "complete"`)) break;
      assert(attempt < 100, `Browser page did not finish loading: ${path}`);
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
  }
  await navigate("/browser-fixture");
  const results = await evaluate(await readFile(join(directory, "checks.js"), "utf8"));
  const viewport = (width) => client.send("Emulation.setDeviceMetricsOverride", {width, height: 844, deviceScaleFactor: 1, mobile: false}, sessionId);
  await viewport(1200);
  await navigate("/hosts");
  await evaluate('document.querySelector("#sidebar a").focus()');
  await viewport(390);
  results.push(...await evaluate(await readFile(join(directory, "navigation.js"), "utf8")));
  await evaluate('document.getElementById("nav-toggle").click()');
  await client.send("Emulation.setEmulatedMedia", {media: "print"}, sessionId);
  results.push(...await evaluate(`(() => {
    const overlay = getComputedStyle(document.querySelector(".nav-overlay"));
    return [
      {name: "printing an open mobile drawer hides its overlay", passed: overlay.display === "none"},
      {name: "printing an open mobile drawer allows the document to flow", passed: getComputedStyle(document.body).overflow !== "hidden"}
    ];
  })()`));
  await client.send("Emulation.setEmulatedMedia", {media: ""}, sessionId);
  await evaluate('document.querySelector(".nav-close").focus()');
  await viewport(1200);
  results.push(...await evaluate(`(async () => {
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    return [{name: "expanding a focused mobile drawer preserves usable desktop focus", passed:
      document.activeElement === document.querySelector("#sidebar a") && !document.getElementById("app-content").inert}];
  })()`));
  assert(Array.isArray(results) && results.length > 0, "Browser returned no assertions");
  for (const result of results) {
    console.log(`${result.passed ? "PASS" : "FAIL"} ${result.name}${!result.passed && result.details ? `: ${result.details}` : ""}`);
  }
  assert(results.every((result) => result.passed), "Browser regression checks failed");
  console.log(`Passed ${results.length} browser checks.`);
} catch (error) {
  console.error(error.stack || error);
  process.exitCode = 1;
} finally {
  clearTimeout(deadline);
  await cleanup();
}
