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
  async function waitForPage(path) {
    const expected = new URL(path, origin);
    for (let attempt = 0; ; attempt++) {
      if (await evaluate(`location.pathname === ${JSON.stringify(expected.pathname)} &&
        location.search === ${JSON.stringify(expected.search)} && document.readyState === "complete"`)) break;
      assert(attempt < 100, `Browser page did not finish loading: ${path}`);
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
  }
  async function navigate(path) {
    await client.send("Page.navigate", {url: `${origin}${path}`}, sessionId);
    await waitForPage(path);
  }
  await navigate("/browser-fixture");
  const results = await evaluate(await readFile(join(directory, "checks.js"), "utf8"));
  const viewport = (width) => client.send("Emulation.setDeviceMetricsOverride", {width, height: 844, deviceScaleFactor: 1, mobile: false}, sessionId);
  await viewport(1200);
  const listOrigin = "/hosts?dir=desc&page=1&per_page=1&q=Browser&sort=id";
  await navigate(listOrigin);
  const recordPath = await evaluate(`(() => {
    const link = document.querySelector('tbody a[href^="/hosts/1?"]');
    const path = link.getAttribute("href");
    link.click();
    return path;
  })()`);
  await waitForPage(recordPath);
  const returnPath = await evaluate(`(() => {
    document.querySelector('.detail-tabs a[href="#connections"]').click();
    const link = document.querySelector("[data-list-return]");
    const path = link.getAttribute("href");
    link.click();
    return path;
  })()`);
  await waitForPage(returnPath);
  results.push({name: "list to detail to another panel to Back preserves filters and sorting", passed:
    returnPath === listOrigin && await evaluate('document.querySelector(\'main input[name="q"]\').value === "Browser"')});
  const clickPath = (selector) => evaluate(`(() => {
    const link = document.querySelector(${JSON.stringify(selector)});
    const path = link.getAttribute("href");
    link.click();
    return path;
  })()`);
  await waitForPage(await clickPath('tbody a[href^="/hosts/1/edit"]'));
  await waitForPage(await clickPath("main [data-list-return]"));
  results.push({name: "Cancel from a row Edit restores the filtered list", passed:
    await evaluate('location.pathname + location.search') === listOrigin});
  await navigate(recordPath);
  await waitForPage(await clickPath(".ph-actions a"));
  const originalHostName = await evaluate('document.querySelector(\'main input[name="name"]\').value');
  await evaluate(`(() => {
    const form = document.querySelector('main form[action="/hosts/1"]');
    form.elements.name.value = " ";
    form.elements.notes.value = "Browser navigation draft";
    form.querySelector('button[type="submit"]').click();
  })()`);
  await waitForPage("/hosts/1");
  results.push({name: "server validation preserves draft values and the original list on Edit", passed:
    await evaluate(`(() => {
      const form = document.querySelector('main form[action="/hosts/1"]');
      return Boolean(document.querySelector('main .error[role="alert"]')) &&
        form.elements.notes.value === "Browser navigation draft" &&
        form.elements.return_to.value === ${JSON.stringify(listOrigin)} &&
        form.querySelector("[data-list-return]").getAttribute("href") === ${JSON.stringify(listOrigin)};
    })()`)});
  await evaluate(`(() => {
    const form = document.querySelector('main form[action="/hosts/1"]');
    form.elements.name.value = ${JSON.stringify(originalHostName)};
    form.querySelector('button[type="submit"]').click();
  })()`);
  await waitForPage(listOrigin);
  results.push({name: "corrected Edit saves and returns to the original filtered page", passed:
    await evaluate('(async () => (await (await fetch("/api/hosts/1")).json()).notes === "Browser navigation draft")()')});
  await waitForPage(await clickPath(".ph-actions a"));
  await evaluate('document.querySelector(\'main input[name="name"]\').value = "Unsaved navigation draft"');
  await waitForPage(await clickPath("main [data-list-return]"));
  results.push({name: "Cancel from New restores the filtered list without creating a record", passed:
    await evaluate('(async () => location.pathname + location.search === ' + JSON.stringify(listOrigin) +
      ' && (await (await fetch("/api/hosts")).json()).length === 1)()')});
  const checkRackLinks = async (scenario) => evaluate(`(async () => {
    document.querySelector('.detail-tabs a[href="#connections"]').click();
    const panel = document.querySelector(".rack-elev").closest("section");
    const results = [];
    for (const path of ["/hosts/1", "/hardware/1"]) {
      let reachable = false;
      for (const link of [...panel.querySelectorAll("a")].filter(link => link.getAttribute("href") === path)) {
        link.scrollIntoView({block: "center"});
        await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
        const box = link.getBoundingClientRect();
        const hit = document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2);
        reachable ||= box.width > 0 && box.height > 0 && (hit === link || link.contains(hit));
      }
      results.push({name: ${JSON.stringify(scenario)} + " rack occupant remains visible and clickable: " + path, passed: reachable});
    }
    return results;
  })()`);
  await navigate("/racks/1");
  results.push(...await checkRackLinks("overlapping"));
  await evaluate(`(async () => {
    for (const [path, position] of [["/api/hosts/1", 42], ["/api/hardware/1", 60]]) {
      const item = await (await fetch(path)).json();
      item.rack_position = position;
      item.u_height = 1;
      const response = await fetch(path, {method: "PUT", headers: {"Content-Type": "application/json"}, body: JSON.stringify(item)});
      if (!response.ok) throw new Error("Could not prepare clamped rack placement: " + response.status);
    }
  })()`);
  await navigate("/racks/1");
  results.push(...await checkRackLinks("clamped out-of-bounds"));
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
  const palette = `JSON.stringify([document.documentElement, document.body, document.querySelector("h1"),
    document.querySelector("th")].map(element => {
      const style = getComputedStyle(element);
      return [style.color, style.backgroundColor, style.colorScheme];
    }))`;
  const lightPrintPalette = await evaluate(palette);
  await evaluate('document.documentElement.dataset.theme = "dark"');
  results.push({name: "explicit dark mode prints with the light document palette", passed: await evaluate(palette) === lightPrintPalette});
  await evaluate('document.documentElement.dataset.theme = "system"');
  await client.send("Emulation.setEmulatedMedia", {media: "print", features: [{name: "prefers-color-scheme", value: "dark"}]}, sessionId);
  results.push({name: "system dark mode prints with the light document palette", passed: await evaluate(palette) === lightPrintPalette});
  await client.send("Emulation.setEmulatedMedia", {media: "screen", features: [{name: "prefers-color-scheme", value: "dark"}]}, sessionId);
  results.push({name: "system dark mode remains active on screen", passed:
    await evaluate('getComputedStyle(document.documentElement).colorScheme') === "dark"});
  await evaluate('document.documentElement.dataset.theme = "dark"');
  await client.send("Emulation.setEmulatedMedia", {media: "", features: []}, sessionId);
  results.push({name: "explicit dark mode remains active on screen", passed:
    await evaluate('getComputedStyle(document.documentElement).colorScheme') === "dark"});
  await evaluate('document.documentElement.dataset.theme = "system"');
  await evaluate('document.querySelector(".nav-close").focus()');
  await viewport(1200);
  results.push(...await evaluate(`(async () => {
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    return [{name: "expanding a focused mobile drawer preserves usable desktop focus", passed:
      document.activeElement === document.querySelector("#sidebar a") && !document.getElementById("app-content").inert}];
  })()`));
  await navigate("/host-types");
  results.push(...await evaluate(await readFile(join(directory, "forms.js"), "utf8")));
  await navigate("/discovery/docker");
  results.push(...await evaluate(`(() => {
    const form = document.querySelector("[data-selection-form]");
    const all = form.querySelector("[data-select-all]");
    const items = [...form.querySelectorAll("[data-selection-item]")];
    const initial = all.checked && items.length === 2 && items.every(item => item.checked) &&
      form.querySelector("[data-selection-count]").textContent === "2 selected";
    all.click();
    const cleared = items.every(item => !item.checked) && form.querySelector('button[type="submit"]').disabled;
    items[1].click();
    const selected = all.indeterminate && !form.querySelector('button[type="submit"]').disabled &&
      JSON.stringify(new FormData(form).getAll("id")) === JSON.stringify(["browser-second"]);
    return [
      {name: "rendered discovery imports initialize selection controls", passed: initial},
      {name: "rendered discovery imports clear all and submit only the chosen resource", passed: cleared && selected}
    ];
  })()`));
  await navigate("/custom-fields");
  let submissionID = 0;
  async function submitForm(selector, values) {
    const marker = `submission-${++submissionID}`;
    await evaluate(`(() => {
      const form = document.querySelector(${JSON.stringify(selector)});
      form.dataset.browserPending = ${JSON.stringify(marker)};
      for (const [name, value] of Object.entries(${JSON.stringify(values)})) form.elements[name].value = value;
      form.requestSubmit();
    })()`);
    // A validation response can have exactly the same URL as its form. Wait
    // for a new document instead of mistaking the old page for completion.
    for (let attempt = 0; ; attempt++) {
      if (await evaluate(`!document.querySelector('[data-browser-pending="${marker}"]') && document.readyState === "complete"`)) break;
      assert(attempt < 100, `Form submission did not finish: ${selector}`);
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
  }
  const createField = '#add-field form';
  await submitForm(createField, {label: "Existing amount", name: "monthly_cost", entity_type: "service", kind: "number"});
  const customDraft = {label: 'Monthly "amount" & <script>window.injected = true</script>', name: "Monthly Cost", entity_type: "service", kind: "number"};
  await submitForm(createField, customDraft);
  results.push(...await evaluate(`(() => {
    const form = document.querySelector(${JSON.stringify(createField)});
    const expected = ${JSON.stringify(customDraft)};
    return [
      {name: "duplicate custom-field errors retain every draft control", passed:
        Object.entries(expected).every(([key, value]) => form.elements[key].value === value)},
      {name: "custom-field errors expose advanced drafts and accessible error focus", passed:
        form.elements.name.closest("details").open && document.activeElement.matches('.error[role="alert"]')},
      {name: "custom-field draft text cannot become executable markup", passed:
        !window.injected && !document.querySelector("main script")}
    ];
  })()`));
  await submitForm(createField, {name: "monthly_cost_override"});
  results.push({name: "correcting only the duplicate name keeps the intended entity and value types", passed:
    await evaluate(`(async () => {
      const load = async path => new DOMParser().parseFromString(await (await fetch(path)).text(), "text/html");
      const service = await load("/services/1/edit");
      const host = await load("/hosts/1/edit");
      return service.querySelector('input[name="cf_monthly_cost_override"]')?.type === "number" &&
        !host.querySelector('[name="cf_monthly_cost_override"]');
    })()`)});
  const editField = await evaluate(`(() => {
    const row = [...document.querySelectorAll(".record-list li")].find(row => row.querySelector("code")?.textContent === "monthly_cost_override");
    row.querySelector("details").open = true;
    return 'form[action="' + row.querySelector("form.inline-editor").getAttribute("action") + '"]';
  })()`);
  await submitForm(editField, {label: "   "});
  results.push({name: "invalid label editing retains its draft and opens only the failed editor", passed:
    await evaluate(`(() => {
      const form = document.querySelector(${JSON.stringify(editField)});
      return form.elements.label.value === "   " && form.closest("details").open &&
        document.querySelectorAll(".record-list details[open]").length === 1 &&
        document.querySelector('#add-field input[name="label"]').value === "";
    })()`)});
  await submitForm(editField, {label: "Corrected amount"});
  results.push({name: "corrected custom-field label saves to the intended definition", passed:
    await evaluate(`document.querySelector(${JSON.stringify(editField)}).elements.label.value === "Corrected amount" && !document.querySelector('main .error')`)});
  await navigate("/relationships");
  const relationshipForm = 'form[action="/relationships"]';
  await submitForm(relationshipForm, {from: "service:1", kind: "depends on", to: "service:1"});
  results.push({name: "relationship errors preserve source, kind and destination with accessible focus", passed:
    await evaluate(`(() => {
      const form = document.querySelector(${JSON.stringify(relationshipForm)});
      return form.elements.from.value === "service:1" && form.elements.kind.value === "depends on" &&
        form.elements.to.value === "service:1" && document.activeElement.matches('.error[role="alert"]');
    })()`)});
  await submitForm(relationshipForm, {to: "host:1"});
  results.push({name: "correcting one relationship endpoint retains the intended source and kind", passed:
    await evaluate(`(async () => {
      const rows = await (await fetch("/api/relationships")).json();
      return rows.length === 1 && rows[0].from_type === "service" && rows[0].from_id === 1 &&
        rows[0].kind === "depends on" && rows[0].to_type === "host" && rows[0].to_id === 1;
    })()`)});
  const temporaryHostID = await evaluate(`(async () => {
    const response = await fetch("/api/hosts", {method: "POST", headers: {"Content-Type": "application/json"},
      body: JSON.stringify({name: "Temporary relationship target", type: "physical"})});
    if (!response.ok) throw new Error("Could not create relationship fixture: " + response.status);
    return (await response.json()).id;
  })()`);
  await navigate("/relationships");
  await evaluate(`(async () => {
    const response = await fetch("/api/hosts/" + ${JSON.stringify(temporaryHostID)}, {method: "DELETE"});
    if (!response.ok) throw new Error("Could not delete relationship fixture: " + response.status);
  })()`);
  await submitForm(relationshipForm, {from: "service:1", kind: "owned by", to: `host:${temporaryHostID}`});
  results.push({name: "an endpoint deleted in another tab remains explicit instead of selecting another record", passed:
    await evaluate(`(() => {
      const form = document.querySelector(${JSON.stringify(relationshipForm)});
      return form.elements.from.value === "service:1" && form.elements.kind.value === "owned by" &&
        form.elements.to.value === ${JSON.stringify(`host:${temporaryHostID}`)} &&
        form.elements.to.selectedOptions[0].textContent.includes("(unavailable)");
    })()`)});
  await submitForm(relationshipForm, {to: "host:1"});
  results.push({name: "replacing an unavailable endpoint saves only the corrected relationship", passed:
    await evaluate(`(async () => {
      const rows = await (await fetch("/api/relationships")).json();
      return rows.length === 2 && rows.every(row => row.from_type === "service" && row.from_id === 1 &&
        row.to_type === "host" && row.to_id === 1) && rows.some(row => row.kind === "owned by");
    })()`)});
  results.push(...await evaluate(`(async () => {
    const results = [];
    for (const missing of ["from", "kind", "to"]) {
      const body = new URLSearchParams(new FormData(document.querySelector('form[action="/relationships"]')));
      for (const [name, value] of Object.entries({from: "service:1", kind: "depends on", to: "host:1"})) body.set(name, value);
      body.set(missing, "");
      const response = await fetch("/relationships", {method: "POST", body});
      if (!response.ok) throw new Error("Relationship validation failed: " + response.status);
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      const control = page.querySelector('form[action="/relationships"]').elements[missing];
      results.push({name: "missing relationship " + missing + " remains an invalid native placeholder", passed:
        control.value === "" && control.validity.valueMissing && !control.checkValidity()});
    }
    return results;
  })()`));
  const portListOrigin = "/ports?dir=desc&q=Draft&sort=name";
  const generatorForm = 'form[action="/ports/generate"]';
  const generatorPrefix = 'Draft "panel" & <script>window.injected = true</script> ';
  await navigate("/ports/new?" + new URLSearchParams({return_to: portListOrigin}));
  // Scientific notation is a valid HTML number but not an integer accepted
  // by the server. This exercises a real submission and validation response.
  await submitForm(generatorForm, {owner: "hardware:1", prefix: generatorPrefix, count: "1e1"});
  results.push(...await evaluate(`(() => {
    const form = document.querySelector(${JSON.stringify(generatorForm)});
    const single = document.querySelector('form[action="/ports"]');
    return [
      {name: "port generation errors retain owner, exact prefix and invalid count", passed:
        form.elements.owner.value === "hardware:1" && form.elements.prefix.value === ${JSON.stringify(generatorPrefix)} && form.elements.count.value === "1e1"},
      {name: "port generation errors retain list provenance and focus the error", passed:
        form.elements.return_to.value === ${JSON.stringify(portListOrigin)} && document.activeElement.matches('[role="alert"]') && !window.injected},
      {name: "generator drafts do not overwrite the separate single-port form", passed:
        single.elements.name.value === "" && single.elements.notes.value === ""}
    ];
  })()`));
  await submitForm(generatorForm, {count: "2"});
  await waitForPage(portListOrigin);
  results.push({name: "correcting only the generation count creates the requested ports and returns to the filtered list", passed:
    await evaluate(`(async () => {
      const rows = await (await fetch("/api/ports")).json();
      return [1, 2].every(n => rows.some(port => port.name === ${JSON.stringify(generatorPrefix)} + n && port.owner_type === "hardware" && port.owner_id === 1));
    })()`)});
  await navigate("/ports/new");
  results.push(...await evaluate(`(async () => {
    const results = [];
    for (const owner of ["", "host:99999"]) {
      const body = new URLSearchParams(new FormData(document.querySelector(${JSON.stringify(generatorForm)})));
      body.set("owner", owner);
      body.set("count", "eight");
      body.set("return_to", "https://example.com/ports");
      const response = await fetch("/ports/generate", {method: "POST", body});
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      const form = page.querySelector(${JSON.stringify(generatorForm)});
      results.push({name: "generation error preserves " + (owner ? "unavailable" : "missing") + " owner without selecting another", passed:
        response.status === 400 && form.elements.owner.value === owner && form.elements.count.value === "eight" && form.elements.return_to.value === "/ports"});
    }
    return results;
  })()`));
  const portFixture = await evaluate(`(async () => {
    async function create(path, data) {
      const response = await fetch("/api/" + path, {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(data)});
      if (!response.ok) throw new Error("Could not create port fixture: " + response.status);
      return response.json();
    }
    const nic = await create("nics", {name: "Temporary draft NIC", host_id: 1, kind: "onboard"});
    const a = await create("ports", {name: "Draft source", owner_type: "host", owner_id: 1, nic_id: nic.id});
    const b = await create("ports", {name: "Original peer", owner_type: "host", owner_id: 1, peer_port_id: a.id});
    const c = await create("ports", {name: "Pending peer", owner_type: "host", owner_id: 1});
    const d = await create("ports", {name: "Concurrent peer", owner_type: "host", owner_id: 1});
    return {nic, a, b, c, d};
  })()`);
  const editPortForm = 'form[action="/ports/' + portFixture.a.id + '"]';
  await navigate("/ports/" + portFixture.a.id + "/edit");
  // Another tab takes C after the form's options were rendered.
  await evaluate(`(async () => {
    const d = ${JSON.stringify(portFixture.d)};
    d.peer_port_id = ${portFixture.c.id};
    const response = await fetch("/api/ports/" + d.id, {method: "PUT", headers: {"Content-Type": "application/json"}, body: JSON.stringify(d)});
    if (!response.ok) throw new Error("Could not connect concurrent peer: " + response.status);
  })()`);
  await submitForm(editPortForm, {peer_port_id: String(portFixture.c.id), notes: "Preserve this connection draft"});
  for (let attempt = 0; attempt < 2; attempt++) {
    results.push(...await evaluate(`(async () => {
      const form = document.querySelector(${JSON.stringify(editPortForm)});
      const a = await (await fetch("/api/ports/" + ${portFixture.a.id})).json();
      const b = await (await fetch("/api/ports/" + ${portFixture.b.id})).json();
      return [
        {name: "occupied peer remains selected on " + ${JSON.stringify(attempt ? "unchanged retry" : "first error")}, passed:
          form.elements.peer_port_id.value === ${JSON.stringify(String(portFixture.c.id))} && form.elements.peer_port_id.selectedOptions[0].textContent.includes("(unavailable)") && form.elements.notes.value === "Preserve this connection draft"},
        {name: "rejected peer change preserves both ends of the original connection on attempt " + ${attempt + 1}, passed:
          a.peer_port_id === ${portFixture.b.id} && b.peer_port_id === ${portFixture.a.id} && a.nic_id === ${portFixture.nic.id}}
      ];
    })()`));
    if (attempt === 0) await submitForm(editPortForm, {});
  }
  await submitForm(editPortForm, {peer_port_id: String(portFixture.b.id)});
  results.push({name: "explicitly restoring the original peer saves the remaining draft", passed:
    await evaluate(`(async () => {
      const port = await (await fetch("/api/ports/" + ${portFixture.a.id})).json();
      return port.peer_port_id === ${portFixture.b.id} && port.notes === "Preserve this connection draft";
    })()`)});
  await navigate("/ports/" + portFixture.a.id + "/edit");
  await evaluate(`(async () => {
    const response = await fetch("/api/nics/" + ${portFixture.nic.id}, {method: "DELETE"});
    if (!response.ok) throw new Error("Could not delete draft NIC: " + response.status);
    document.querySelector(${JSON.stringify(editPortForm)}).noValidate = true;
  })()`);
  await submitForm(editPortForm, {name: ""});
  results.push({name: "a NIC deleted in another tab survives an unrelated name validation error", passed:
    await evaluate(`(() => {
      const form = document.querySelector(${JSON.stringify(editPortForm)});
      return form.elements.name.value === "" && form.elements.nic_id.value === ${JSON.stringify(String(portFixture.nic.id))} &&
        form.elements.nic_id.selectedOptions[0].textContent.includes("(unavailable)") && form.elements.peer_port_id.value === ${JSON.stringify(String(portFixture.b.id))};
    })()`)});
  await submitForm(editPortForm, {name: "Corrected port name"});
  results.push({name: "correcting the name alone does not silently clear an unavailable NIC", passed:
    await evaluate(`(async () => {
      const form = document.querySelector(${JSON.stringify(editPortForm)});
      const port = await (await fetch("/api/ports/" + ${portFixture.a.id})).json();
      return !!form && form.elements.nic_id.value === ${JSON.stringify(String(portFixture.nic.id))} && port.name === "Draft source";
    })()`)});
  await submitForm(editPortForm, {nic_id: "0"});
  results.push({name: "explicitly clearing an unavailable NIC saves the draft without disconnecting its peer", passed:
    await evaluate(`(async () => {
      const port = await (await fetch("/api/ports/" + ${portFixture.a.id})).json();
      return port.name === "Corrected port name" && port.nic_id === 0 && port.peer_port_id === ${portFixture.b.id};
    })()`)});
  await navigate("/hosts/new");
  const retiredTypeHost = await evaluate(`(async () => {
    const csrf = document.querySelector('[name="csrf_token"]').value;
    let response = await fetch("/host-types", {method: "POST", body: new URLSearchParams({csrf_token: csrf, name: "draft-retired", label: "Temporary draft type"})});
    if (!response.ok) throw new Error("Could not create draft host type: " + response.status);
    response = await fetch("/api/hosts", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({name: "Original typed host", type: "draft-retired"})});
    if (!response.ok) throw new Error("Could not create typed host: " + response.status);
    return response.json();
  })()`);
  const editTypedHost = 'form[action="/hosts/' + retiredTypeHost.id + '"]';
  await navigate("/hosts/" + retiredTypeHost.id + "/edit");
  await evaluate(`(async () => {
    const csrf = document.querySelector('[name="csrf_token"]').value;
    const response = await fetch("/host-types/draft-retired/delete", {method: "POST", body: new URLSearchParams({csrf_token: csrf, replacement: "vm"})});
    if (!response.ok) throw new Error("Could not retire draft host type: " + response.status);
  })()`);
  await submitForm(editTypedHost, {name: "Pending typed host", notes: "Preserve host type draft"});
  for (let attempt = 0; attempt < 2; attempt++) {
    results.push(...await evaluate(`(async () => {
      const form = document.querySelector(${JSON.stringify(editTypedHost)});
      const host = await (await fetch("/api/hosts/" + ${retiredTypeHost.id})).json();
      return [
        {name: "retired host type stays selected on " + ${JSON.stringify(attempt ? "unchanged retry" : "first error")}, passed:
          form.elements.type.value === "draft-retired" && form.elements.type.selectedOptions[0].textContent.includes("(unavailable)") &&
          form.elements.name.value === "Pending typed host" && form.elements.notes.value === "Preserve host type draft"},
        {name: "rejected stale type preserves the deliberate reassignment on attempt " + ${attempt + 1}, passed:
          host.type === "vm" && host.name === "Original typed host"}
      ];
    })()`));
    if (attempt === 0) await submitForm(editTypedHost, {});
  }
  await submitForm(editTypedHost, {type: "vm"});
  results.push({name: "explicitly choosing an available host type saves the remaining draft", passed:
    await evaluate(`(async () => {
      const host = await (await fetch("/api/hosts/" + ${retiredTypeHost.id})).json();
      return host.type === "vm" && host.name === "Pending typed host" && host.notes === "Preserve host type draft";
    })()`)});
  await navigate("/hosts/new");
  const numericDraftHosts = await evaluate(`(async () => {
    const csrf = document.querySelector('[name="csrf_token"]').value;
    const definition = await fetch("/custom-fields", {method: "POST", body: new URLSearchParams({csrf_token: csrf, name: "numeric_draft", label: "Numeric draft", kind: "number", entity_type: "host"})});
    if (!definition.ok) throw new Error("Could not create numeric field: " + definition.status);
    const hosts = [];
    for (const [raw, expected] of [["+12", 12], ["1_000", 1000], ["0x1p2", 4], ["1.", 1], [".5", 0.5], ["1e2", 100], ["9007199254740993", 9007199254740992], ["1e-1000", 0]]) {
      const response = await fetch("/api/hosts", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({name: "Numeric representation " + raw, type: "physical", custom_fields: {numeric_draft: raw}})});
      if (!response.ok) throw new Error("Could not store numeric representation: " + raw + " / " + response.status);
      hosts.push({id: (await response.json()).id, raw, expected});
    }
    return hosts;
  })()`);
  for (const host of numericDraftHosts) {
    results.push({name: "editing preserves the accepted custom number representation " + host.raw, passed:
      await evaluate(`(async () => {
        const response = await fetch("/hosts/" + ${host.id} + "/edit");
        const page = new DOMParser().parseFromString(await response.text(), "text/html");
        return page.querySelector('[name="cf_numeric_draft"]').value === ${JSON.stringify(host.raw)};
      })()`)});
  }
  const numericHost = numericDraftHosts[0];
  await navigate("/hosts/" + numericHost.id + "/edit");
  const numericHostForm = 'form[action="/hosts/' + numericHost.id + '"]';
  await submitForm(numericHostForm, {notes: "Edit only these notes"});
  results.push({name: "editing unrelated host notes does not erase a stored custom number", passed:
    await evaluate(`(async () => {
      const host = await (await fetch("/api/hosts/" + ${numericHost.id})).json();
      return host.notes === "Edit only these notes" && host.custom_fields.numeric_draft === 12;
    })()`)});
  await navigate("/hosts/" + numericHost.id + "/edit");
  await submitForm(numericHostForm, {cf_numeric_draft: "twelve", notes: "Retain the invalid numeric draft"});
  results.push({name: "server validation keeps an invalid custom number visible for correction", passed:
    await evaluate(`(() => {
      const form = document.querySelector(${JSON.stringify(numericHostForm)});
      return form.elements.cf_numeric_draft.value === "twelve" && form.elements.notes.value === "Retain the invalid numeric draft" && document.activeElement.matches('[role="alert"]');
    })()`)});
  await submitForm(numericHostForm, {});
  results.push({name: "retrying an invalid custom number does not silently clear the stored value", passed:
    await evaluate(`(async () => {
      const host = await (await fetch("/api/hosts/" + ${numericHost.id})).json();
      return !!document.querySelector(${JSON.stringify(numericHostForm)}) && host.custom_fields.numeric_draft === 12;
    })()`)});
  await submitForm(numericHostForm, {cf_numeric_draft: "13"});
  results.push({name: "correcting only a custom number saves the remaining draft", passed:
    await evaluate(`(async () => {
      const host = await (await fetch("/api/hosts/" + ${numericHost.id})).json();
      return host.custom_fields.numeric_draft === 13 && host.notes === "Retain the invalid numeric draft";
    })()`)});
  const dateDraftFixtures = await evaluate(`(async () => {
    const csrf = document.querySelector('[name="csrf_token"]').value;
    const definition = await fetch("/custom-fields", {method: "POST", body: new URLSearchParams({csrf_token: csrf, name: "date_draft", label: "Date draft", kind: "date", entity_type: "host"})});
    if (!definition.ok) throw new Error("Could not create custom date field: " + definition.status);
    async function create(path, data) {
      const response = await fetch("/api/" + path, {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(data)});
      if (!response.ok) throw new Error("Could not create date fixture: " + response.status);
      return response.json();
    }
    return {
      backup: await create("backups", {source: "Whitespace date backup", last_run: " 2030-02-28 "}),
      normal: await create("backups", {source: "Native date backup", last_run: "2030-02-28"}),
      host: await create("hosts", {name: "Year zero custom date", type: "physical", custom_fields: {date_draft: "0000-01-01"}})
    };
  })()`);
  results.push({name: "ordinary dates retain native date controls", passed:
    await evaluate(`(async () => {
      const response = await fetch("/backups/" + ${dateDraftFixtures.normal.id} + "/edit");
      const input = new DOMParser().parseFromString(await response.text(), "text/html").querySelector('[name="last_run"]');
      return input.type === "date" && input.value === "2030-02-28";
    })()`)});
  const dateBackupForm = 'form[action="/backups/' + dateDraftFixtures.backup.id + '"]';
  await navigate("/backups/" + dateDraftFixtures.backup.id + "/edit");
  results.push({name: "accepted dates containing whitespace remain visible on edit", passed:
    await evaluate(`document.querySelector('[name="last_run"]').value === " 2030-02-28 "`)});
  await submitForm(dateBackupForm, {notes: "Keep the accepted date"});
  results.push({name: "editing unrelated notes preserves an accepted date", passed:
    await evaluate(`(async () => {
      const backup = await (await fetch("/api/backups/" + ${dateDraftFixtures.backup.id})).json();
      return backup.last_run === "2030-02-28" && backup.notes === "Keep the accepted date";
    })()`)});
  await navigate("/backups/" + dateDraftFixtures.backup.id + "/edit");
  // Bypass the original native control to exercise the server's response to
  // an invalid submitted value, then retry its real rendered fallback.
  await evaluate(`document.querySelector('[name="last_run"]').type = "text"`);
  await submitForm(dateBackupForm, {last_run: "not-a-date"});
  results.push({name: "date validation retains the invalid draft", passed:
    await evaluate(`document.querySelector('[name="last_run"]').value === "not-a-date"`)});
  await submitForm(dateBackupForm, {});
  results.push({name: "retrying an invalid optional date does not silently clear its stored value", passed:
    await evaluate(`(async () => {
      const backup = await (await fetch("/api/backups/" + ${dateDraftFixtures.backup.id})).json();
      return !!document.querySelector(${JSON.stringify(dateBackupForm)}) && backup.last_run === "2030-02-28";
    })()`)});
  await submitForm(dateBackupForm, {last_run: "2031-01-02"});
  results.push({name: "correcting a date saves the retained draft", passed:
    await evaluate(`(async () => {
      const backup = await (await fetch("/api/backups/" + ${dateDraftFixtures.backup.id})).json();
      return backup.last_run === "2031-01-02" && backup.notes === "Keep the accepted date";
    })()`)});
  await navigate("/hosts/" + dateDraftFixtures.host.id + "/edit");
  results.push({name: "year zero custom dates accepted by the domain remain visible in the browser", passed:
    await evaluate(`document.querySelector('[name="cf_date_draft"]').value === "0000-01-01"`)});
  await submitForm('form[action="/hosts/' + dateDraftFixtures.host.id + '"]', {notes: "Keep year zero"});
  results.push({name: "editing unrelated host notes does not erase a year zero custom date", passed:
    await evaluate(`(async () => {
      const host = await (await fetch("/api/hosts/" + ${dateDraftFixtures.host.id})).json();
      return host.custom_fields.date_draft === "0000-01-01" && host.notes === "Keep year zero";
    })()`)});
  const integerDraftHost = await evaluate(`(async () => {
    const response = await fetch("/api/hosts", {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify({name: "Integer draft host", type: "physical", u_height: 2, rack_position: 5})});
    if (!response.ok) throw new Error("Could not create integer draft host: " + response.status);
    return response.json();
  })()`);
  const integerHostForm = 'form[action="/hosts/' + integerDraftHost.id + '"]';
  await navigate("/hosts/" + integerDraftHost.id + "/edit");
  await submitForm(integerHostForm, {u_height: "1e1", notes: "Retain integer draft"});
  results.push({name: "integer validation retains scientific notation instead of replacing it with zero", passed:
    await evaluate(`document.querySelector('[name="u_height"]').value === "1e1"`)});
  await submitForm(integerHostForm, {});
  results.push({name: "retrying an invalid integer does not silently save zero", passed:
    await evaluate(`(async () => {
      const host = await (await fetch("/api/hosts/" + ${integerDraftHost.id})).json();
      return !!document.querySelector(${JSON.stringify(integerHostForm)}) && host.u_height === 2 && host.rack_position === 5;
    })()`)});
  await submitForm(integerHostForm, {u_height: "10"});
  results.push({name: "correcting only the integer saves the remaining draft", passed:
    await evaluate(`(async () => {
      const host = await (await fetch("/api/hosts/" + ${integerDraftHost.id})).json();
      return host.u_height === 10 && host.rack_position === 5 && host.notes === "Retain integer draft";
    })()`)});
  await navigate("/hosts/" + integerDraftHost.id + "/edit");
  // A direct client can submit values that native number inputs cannot hold.
  await evaluate(`document.querySelector('[name="u_height"]').type = "text"`);
  const invalidInteger = 'NaN "value" & <script>window.injected = true</script>';
  await submitForm(integerHostForm, {u_height: invalidInteger});
  results.push({name: "non-numeric integer drafts use an escaped editable text fallback", passed:
    await evaluate(`(() => {
      const input = document.querySelector('[name="u_height"]');
      return input.type === "text" && input.value === ${JSON.stringify(invalidInteger)} && !window.injected;
    })()`)});
  await navigate("/hosts/" + integerDraftHost.id + "/edit");
  await evaluate(`document.querySelector(${JSON.stringify(integerHostForm)}).noValidate = true`);
  await submitForm(integerHostForm, {name: "", u_height: ""});
  results.push({name: "an explicitly blank optional integer stays blank after another field fails", passed:
    await evaluate(`document.querySelector('[name="u_height"]').value === ""`)});
  await navigate("/hosts/new");
  await evaluate(`(() => {
    document.querySelector('form[action="/hosts"]').elements.name.value = "Unsaved appearance draft";
    document.querySelector('form[action="/hosts"]').elements.notes.value = "Keep this page and its input";
    document.documentElement.dataset.appearanceDraftPage = "original";
    document.querySelector('.theme-switch button[value="dark"]').focus();
    document.querySelector('.theme-switch button[value="dark"]').click();
  })()`);
  for (let attempt = 0; ; attempt++) {
    if (await evaluate(`document.readyState === "complete" && document.documentElement.dataset.theme === "dark"`)) break;
    assert(attempt < 100, "Theme update did not finish");
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  results.push({name: "changing appearance keeps the current document and unsaved form draft", passed:
    await evaluate(`(() => {
      const form = document.querySelector('form[action="/hosts"]');
      return document.documentElement.dataset.appearanceDraftPage === "original" && form.elements.name.value === "Unsaved appearance draft" && form.elements.notes.value === "Keep this page and its input";
    })()`)});
  results.push({name: "appearance updates pressed state, persists the cookie and keeps button focus", passed:
    await evaluate(`(async () => {
      const selected = document.querySelector('.theme-switch button[value="dark"]');
      const response = await fetch("/hosts/new");
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      return document.activeElement === selected && selected.classList.contains("active") && selected.getAttribute("aria-pressed") === "true" &&
        document.querySelectorAll('.theme-switch [aria-pressed="true"]').length === 1 && page.documentElement.dataset.theme === "dark";
    })()`)});
  results.push(...await evaluate(`(async () => {
    const originalFetch = window.fetch;
    const form = document.querySelector(".theme-switch");
    const button = form.querySelector('[value="light"]');
    const results = [];
    async function idle() {
      for (let i = 0; form.hasAttribute("aria-busy"); i++) {
        if (i > 100) throw new Error("Appearance request did not settle");
        await new Promise(resolve => setTimeout(resolve, 10));
      }
    }
    try {
      for (const failure of ["network", "forbidden", "login HTML", "redirect", "broken JSON", "unknown theme"]) {
        window.fetch = async () => {
          if (failure === "network") throw new TypeError("Offline");
          if (failure === "redirect") return {ok: true, redirected: true, headers: new Headers({"Content-Type": "application/json"}), json: async () => ({theme: "light"})};
          if (failure === "forbidden") return new Response("Forbidden", {status: 403});
          if (failure === "login HTML") return new Response("<form>Sign in</form>", {headers: {"Content-Type": "text/html"}});
          return new Response(failure === "broken JSON" ? "{" : '{"theme":"unknown"}', {headers: {"Content-Type": "application/json"}});
        };
        form.requestSubmit(button);
        await idle();
        results.push({name: "appearance " + failure + " failure preserves the document, theme and draft", passed:
          document.documentElement.dataset.appearanceDraftPage === "original" && document.documentElement.dataset.theme === "dark" &&
          document.querySelector('form[action="/hosts"]').elements.name.value === "Unsaved appearance draft" &&
          document.getElementById("theme-status").textContent.includes("Could not save") && !button.hasAttribute("aria-disabled")});
      }
      let resolveRequest, calls = 0;
      window.fetch = async () => {calls++; return new Promise(resolve => {resolveRequest = resolve;});};
      button.focus();
      form.requestSubmit(button);
      form.requestSubmit(form.querySelector('[value="system"]'));
      const pending = calls === 1 && form.getAttribute("aria-busy") === "true" && button.getAttribute("aria-disabled") === "true" &&
        document.activeElement === button && document.getElementById("theme-status").textContent === "Saving appearance…";
      resolveRequest(new Response('{"theme":"light"}', {headers: {"Content-Type": "application/json"}}));
      await idle();
      results.push({name: "pending appearance changes prevent overlapping requests without stealing focus", passed:
        pending && calls === 1 && document.documentElement.dataset.theme === "light" && document.activeElement === button &&
        button.getAttribute("aria-pressed") === "true" && document.getElementById("theme-status").textContent === "Appearance saved."});
    } finally {
      window.fetch = originalFetch;
    }
    return results;
  })()`));
  await evaluate(`document.querySelector('.theme-switch button[value="system"]').focus()`);
  await client.send("Input.dispatchKeyEvent", {type: "keyDown", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13, text: "\r", unmodifiedText: "\r"}, sessionId);
  await client.send("Input.dispatchKeyEvent", {type: "keyUp", key: "Enter", code: "Enter", windowsVirtualKeyCode: 13}, sessionId);
  for (let attempt = 0; ; attempt++) {
    if (await evaluate(`document.documentElement.dataset.theme === "system" && !document.querySelector(".theme-switch").hasAttribute("aria-busy")`)) break;
    assert(attempt < 100, "Keyboard appearance update did not finish: " + await evaluate(`JSON.stringify({theme: document.documentElement.dataset.theme, focus: document.activeElement.outerHTML, status: document.getElementById("theme-status").textContent})`));
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  results.push({name: "keyboard activation saves the chosen appearance without replacing the draft", passed:
    await evaluate(`(async () => {
      const response = await fetch("/hosts/new");
      const page = new DOMParser().parseFromString(await response.text(), "text/html");
      return document.activeElement.value === "system" && page.documentElement.dataset.theme === "system" &&
        document.documentElement.dataset.appearanceDraftPage === "original" && document.querySelector('form[action="/hosts"]').elements.notes.value === "Keep this page and its input";
    })()`)});
  const actionListOrigin = "/hosts?dir=desc&q=Browser&sort=name";
  const actionDetailPath = "/hosts/1?" + new URLSearchParams({return_to: actionListOrigin});
  await navigate(actionDetailPath);
  await submitForm('form[action="/tags"]', {tag: "navigation-retained"});
  results.push({name: "adding a detail tag preserves the originating list filters", passed:
    await evaluate(`document.querySelector('[data-list-return]').getAttribute("href") === ${JSON.stringify(actionListOrigin)}`)});
  await navigate(actionDetailPath + "#activity");
  await submitForm('form[action="/hosts/1/journal"]', {body: "Navigation context journal entry"});
  results.push({name: "adding a journal entry preserves list filters and the activity section", passed:
    await evaluate(`location.hash === "#activity" && document.querySelector('[data-list-return]').getAttribute("href") === ${JSON.stringify(actionListOrigin)}`)});
  const journalDeletePath = await evaluate(`(() => {
    const entry = [...document.querySelectorAll(".journal-entry")].find(row => row.textContent.includes("Navigation context journal entry"));
    window.confirm = () => true;
    return entry.querySelector("form").getAttribute("action");
  })()`);
  await submitForm('form[action="' + journalDeletePath + '"]', {});
  results.push({name: "deleting a journal entry retains list context and the activity panel", passed:
    await evaluate(`location.hash === "#activity" && !document.getElementById("activity").hidden && document.querySelector('[data-list-return]').getAttribute("href") === ${JSON.stringify(actionListOrigin)}`)});
  await evaluate(`(() => {
    const files = new DataTransfer();
    files.items.add(new File(["Navigation context attachment"], "navigation-context.txt", {type: "text/plain"}));
    document.querySelector('form[action="/hosts/1/attachments"]').elements.file.files = files.files;
  })()`);
  await submitForm('form[action="/hosts/1/attachments"]', {});
  results.push({name: "uploading an attachment retains list context and the activity panel", passed:
    await evaluate(`location.hash === "#activity" && !document.getElementById("activity").hidden && document.querySelector('[data-list-return]').getAttribute("href") === ${JSON.stringify(actionListOrigin)}`)});
  const attachmentDeletePath = await evaluate(`(() => {
    const link = [...document.querySelectorAll('a[href^="/attachments/"]')].find(link => link.textContent === "navigation-context.txt");
    window.confirm = () => true;
    return link.closest("li").querySelector("form").getAttribute("action");
  })()`);
  await submitForm('form[action="' + attachmentDeletePath + '"]', {});
  results.push({name: "deleting an attachment retains list context and the activity panel", passed:
    await evaluate(`location.hash === "#activity" && !document.getElementById("activity").hidden && document.querySelector('[data-list-return]').getAttribute("href") === ${JSON.stringify(actionListOrigin)}`)});
  await evaluate(`(() => {
    document.querySelector('.detail-tabs a[href="#overview"]').click();
    const tag = [...document.querySelectorAll(".tag")].find(tag => tag.textContent.includes("navigation-retained"));
    tag.querySelector("form").id = "delete-context-tag";
  })()`);
  await submitForm("#delete-context-tag", {});
  results.push({name: "removing a detail tag retains the originating list context", passed:
    await evaluate(`document.querySelector('[data-list-return]').getAttribute("href") === ${JSON.stringify(actionListOrigin)}`)});
  await evaluate(`document.querySelector('[data-list-return]').click()`);
  await waitForPage(actionListOrigin);
  results.push({name: "Back to list restores filters after child record mutations", passed:
    await evaluate(`location.pathname + location.search === ${JSON.stringify(actionListOrigin)} && document.querySelector('.list-controls [name="q"]').value === "Browser"`)});
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
