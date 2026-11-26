// Evaluated in Chromium by run.mjs after the fixture and production app.js load.
(async () => {
  const results = [];
  function check(name, condition, details = "") {
    results.push({name, passed: Boolean(condition), details});
  }
  const settle = () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const tabs = [...document.querySelectorAll('.detail-tabs a')];
  const panels = [...document.querySelectorAll('[data-detail-panel]')];

  check("tabs expose one selected panel", tabs[0].getAttribute("aria-selected") === "true" &&
    !panels[0].hidden && panels.slice(1).every((panel) => panel.hidden));
  tabs[0].click();
  const historyLength = history.length;
  tabs[0].click();
  check("clicking the active tab does not add history", history.length === historyLength);
  tabs[1].click();
  check("changing tabs updates the fragment and accessible state", location.hash === "#connections" &&
    tabs[1].getAttribute("aria-selected") === "true" && tabs[1].tabIndex === 0 &&
    tabs[0].tabIndex === -1 && panels[0].hidden && !panels[1].hidden);
  for (const [step, hash, panel] of [["back", "#overview", panels[0]], ["forward", "#connections", panels[1]]]) {
    const navigation = new Promise((resolve) => window.addEventListener("popstate", resolve, {once: true}));
    history[step]();
    await navigation;
    check(`browser ${step} restores the selected panel`, location.hash === hash && !panel.hidden);
  }
  for (const modifier of ["ctrlKey", "metaKey", "shiftKey", "altKey"]) {
    let prevented;
    // Observe app.js, then suppress the real navigation to keep this test page.
    tabs[2].addEventListener("click", (event) => {
      prevented = event.defaultPrevented;
      event.preventDefault();
    }, {once: true});
    tabs[2].dispatchEvent(new MouseEvent("click", {bubbles: true, cancelable: true, [modifier]: true}));
    check(`${modifier} click retains browser behavior`, prevented === false && location.hash === "#connections");
  }
  tabs[1].focus();
  tabs[1].dispatchEvent(new KeyboardEvent("keydown", {key: "End", bubbles: true, cancelable: true}));
  check("End selects and focuses the last tab", document.activeElement === tabs[2] && !panels[2].hidden);
  tabs[2].dispatchEvent(new KeyboardEvent("keydown", {key: "ArrowRight", bubbles: true, cancelable: true}));
  check("arrow navigation wraps to the first tab", document.activeElement === tabs[0] && !panels[0].hidden);
  location.hash = "deep-heading";
  await settle();
  check("deep links reveal a previously hidden panel", !panels[2].hidden && panels[0].hidden);
  check("deep links scroll to their heading", scrollY > 1000, `scrollY=${scrollY}`);

  const checks = [...document.querySelectorAll('input[name="ids"]')];
  const selectAll = document.querySelector("[data-select-all]");
  const submit = document.querySelector("#bulkform button");
  const count = document.querySelector("[data-selection-count]");
  check("empty selection disables bulk actions", submit.disabled);
  checks[0].checked = true;
  // Emulate values restored by the browser without input/change events.
  window.dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true}));
  check("restored partial selection updates actions and count", !submit.disabled && count.textContent === "1 selected");
  check("restored partial selection updates checkbox and row state", selectAll.indeterminate &&
    !selectAll.checked && checks[0].closest("tr").classList.contains("selected"));
  checks[1].checked = true;
  window.dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true}));
  check("restored full selection checks select-all", selectAll.checked && !selectAll.indeterminate && count.textContent === "2 selected");
  selectAll.checked = false;
  selectAll.dispatchEvent(new Event("change", {bubbles: true}));
  check("clearing select-all clears every selected row", submit.disabled && checks.every((input) =>
    !input.checked && !input.closest("tr").classList.contains("selected")));

  async function page(path) {
    const response = await fetch(path);
    if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`);
    return new DOMParser().parseFromString(await response.text(), "text/html");
  }
  const name = 'Browser "quoted" & <script>window.injected = true</script>';
  const lists = ["hosts", "services", "networks", "domains", "certificates", "backups", "hardware",
    "subscriptions", "accounts", "sites", "locations", "racks", "contacts", "vlans", "reservations", "nics", "ports"];
  for (const list of lists) {
    const doc = await page(`/${list}`);
    let expected = `Select ${name}`;
    if (list === "domains") expected = "Select browser.example.test";
    if (list === "vlans") expected = `Select VLAN 42 (${name})`;
    if (list === "nics" || list === "ports") expected = `Select ${name} on ${name}`;
    const label = doc.querySelector('input[name="ids"]')?.getAttribute("aria-label");
    check(`${list} selection identifies the record and escapes its name`, label === expected &&
      !doc.querySelector("script:not([src])"), `label=${label}`);
  }
  const beyond = await page("/search?q=Browser&page=999&per_page=1");
  const resultsPanel = beyond.querySelector(".search-results");
  const first = resultsPanel.querySelector("a");
  const firstURL = first && new URL(first.getAttribute("href"), location.href);
  check("out-of-range search explains that matches still exist", resultsPanel.textContent.includes("No results on this page.") &&
    firstURL?.pathname === "/search" && firstURL.searchParams.get("q") === "Browser" && firstURL.searchParams.get("page") === "1");
  const empty = await page("/search?q=missing-8dd019e4");
  check("empty search remains distinct from an out-of-range page", empty.querySelector(".search-results").textContent.includes('No results for "missing-8dd019e4".') &&
    !empty.querySelector(".search-results a"));
  return results;
})()
