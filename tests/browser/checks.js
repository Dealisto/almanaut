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
  for (const [modifier, key] of [["altKey", "ArrowLeft"], ["altKey", "ArrowRight"], ["ctrlKey", "Home"], ["metaKey", "ArrowLeft"]]) {
    const event = new KeyboardEvent("keydown", {key, [modifier]: true, bubbles: true, cancelable: true});
    tabs[1].dispatchEvent(event);
    check(`${modifier} ${key} retains browser navigation`, !event.defaultPrevented &&
      document.activeElement === tabs[1] && location.hash === "#connections");
    // Reset between cases so a failure does not obscure the next assertion.
    tabs[1].click();
    tabs[1].focus();
  }
  tabs[1].dispatchEvent(new KeyboardEvent("keydown", {key: "End", bubbles: true, cancelable: true}));
  check("End selects and focuses the last tab", document.activeElement === tabs[2] && !panels[2].hidden);
  tabs[2].dispatchEvent(new KeyboardEvent("keydown", {key: "ArrowRight", bubbles: true, cancelable: true}));
  check("arrow navigation wraps to the first tab", document.activeElement === tabs[0] && !panels[0].hidden);
  location.hash = "deep-heading";
  await settle();
  check("deep links reveal a previously hidden panel", !panels[2].hidden && panels[0].hidden);
  check("deep links scroll to their heading", scrollY > 1000, `scrollY=${scrollY}`);

  const importForm = document.getElementById("discovery-import");
  const changesForm = document.getElementById("discovery-changes");
  const importAll = importForm.querySelector("[data-select-all]");
  const importItems = [...importForm.querySelectorAll("[data-selection-item]")];
  const changesAll = changesForm.querySelector("[data-select-all]");
  const changesItems = [...changesForm.querySelectorAll("[data-selection-item]")];
  check("discovery reflects initially selected imports", importAll.checked && !importAll.indeterminate &&
    importForm.querySelector("[data-selection-count]").textContent === "2 selected" && !importForm.querySelector("button").disabled);
  check("discovery changes require an explicit field selection", !changesAll.checked && changesForm.querySelector("button").disabled);
  importAll.click();
  check("discovery clear-all disables empty imports and preserves host-link preferences", importItems.every((item) => !item.checked) &&
    importForm.querySelector("button").disabled && importForm.elements.link.checked);
  changesAll.click();
  check("discovery select-all stays within the selected resource", changesItems.every((item) => item.checked) &&
    !changesForm.querySelector("button").disabled && importItems.every((item) => !item.checked));
  changesItems[0].click();
  check("discovery partial selections expose an indeterminate select-all", changesAll.indeterminate && !changesAll.checked &&
    changesForm.querySelector("[data-selection-count]").textContent === "1 selected");
  check("discovery submits only selected fields without select-all metadata", JSON.stringify([...new FormData(changesForm)]) ===
    JSON.stringify([["field", "ram"]]));
  importItems[0].checked = true;
  changesItems[1].checked = false;
  window.dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true}));
  check("restored discovery values reconcile each form independently", importAll.indeterminate &&
    !importForm.querySelector("button").disabled && changesForm.querySelector("button").disabled &&
    !changesAll.checked && !changesAll.indeterminate);
  const trackedForm = document.getElementById("discovery-tracked");
  check("already tracked discovery resources cannot be selected", trackedForm.querySelector("[data-select-all]").disabled &&
    trackedForm.querySelector("button").disabled);

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
    const originQuery = new URLSearchParams({q: "Browser", sort: "id", dir: "desc", page: "1", per_page: "1"});
    originQuery.sort();
    const origin = `/${list}?${originQuery}`;
    const filtered = await page(origin);
    const record = [...filtered.querySelectorAll("tbody a")].find((link) =>
      new URL(link.getAttribute("href"), location.href).pathname === `/${list}/1`);
    const recordURL = record && new URL(record.getAttribute("href"), location.href);
    check(`${list} record links carry their originating filters, sort and page`, recordURL?.searchParams.get("return_to") === origin);
    if (recordURL) {
      const detail = await page(recordURL.pathname + recordURL.search);
      check(`${list} server-rendered Back to list preserves list controls without JavaScript`,
        detail.querySelector("[data-list-return]")?.getAttribute("href") === origin);
      const detailEdit = detail.querySelector(".ph-actions a");
      const detailEditURL = detailEdit && new URL(detailEdit.getAttribute("href"), location.href);
      check(`${list} detail Edit carries the originating list`, detailEditURL?.pathname === `/${list}/1/edit` &&
        detailEditURL.searchParams.get("return_to") === origin);
    }
    for (const [label, target, action] of [["New", `/${list}/new`, `/${list}`], ["Edit", `/${list}/1/edit`, `/${list}/1`]]) {
      const link = [...filtered.querySelectorAll("main a")].find((link) =>
        new URL(link.getAttribute("href"), location.href).pathname === target);
      const linkURL = link && new URL(link.getAttribute("href"), location.href);
      check(`${list} list ${label} link preserves list controls`, linkURL?.searchParams.get("return_to") === origin);
      if (linkURL) {
        const formPage = await page(linkURL.pathname + linkURL.search);
        const form = [...formPage.querySelectorAll("main form")].find((form) => form.getAttribute("action") === action);
        check(`${list} ${label} form retains Cancel and submitted context with a canonical action`,
          form?.querySelector('[name="return_to"]')?.value === origin &&
          form.querySelector("[data-list-return]")?.getAttribute("href") === origin);
        if (list === "ports" && label === "New") {
          check("port generation carries the same validated list context", formPage.querySelector(
            'form[action="/ports/generate"] input[name="return_to"]')?.value === origin);
        }
      }
    }
    const query = new URLSearchParams({q: "Browser", sort: "id", dir: "desc", page: "999", per_page: "1"});
    const outOfRange = await page(`/${list}?${query}`);
    const emptyState = outOfRange.querySelector(".empty");
    const first = emptyState?.querySelector("a");
    const firstURL = first && new URL(first.getAttribute("href"), location.href);
    check(`${list} out-of-range state preserves the current filters`, emptyState?.textContent.includes("No records on this page.") &&
      firstURL?.pathname === `/${list}` && firstURL.searchParams.get("q") === "Browser" &&
      firstURL.searchParams.get("sort") === "id" && firstURL.searchParams.get("dir") === "desc" &&
      firstURL.searchParams.get("per_page") === "1" && firstURL.searchParams.get("page") === "1");
  }
  const filteredEmpty = await page("/hosts?q=missing-8dd019e4");
  check("empty inventory filters remain distinct from an out-of-range page", filteredEmpty.querySelector(".empty").textContent.includes("No matching records.") &&
    filteredEmpty.querySelector(".empty a").getAttribute("href") === "/hosts");
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
