/* Progressive enhancements: navigation, search shortcuts, detail tabs and selection.
   Every form and inventory page remains usable without JavaScript. */
"use strict";
(() => {
  document.documentElement.classList.add("js");
  const toggle = document.getElementById("nav-toggle");
  const sidebar = document.getElementById("sidebar");
  const content = document.getElementById("app-content");
  const mobile = matchMedia("(max-width: 860px)");
  const search = document.getElementById("global-search");
  function updateNavigation(moveFocus) {
    if (!toggle || !sidebar || !content) return;
    const open = mobile.matches && toggle.checked;
    sidebar.inert = mobile.matches && !open;
    content.inert = open;
    toggle.setAttribute("aria-expanded", String(open));
    document.body.classList.toggle("nav-open", open);
    if (moveFocus) {
      // Let the checkbox's native click and drawer visibility update finish
      // before placing keyboard focus inside the newly opened navigation.
      requestAnimationFrame(() => {
        if (mobile.matches && toggle.checked) sidebar.querySelector("a")?.focus();
        else toggle.focus();
      });
    }
  }
  toggle?.addEventListener("change", () => updateNavigation(true));
  document.querySelector(".nav-close")?.addEventListener("click", () => {
    toggle.checked = false;
    updateNavigation(true);
  });
  mobile.addEventListener("change", () => { if (toggle) toggle.checked = false; updateNavigation(false); });
  updateNavigation(false);
  const themeForm = document.querySelector(".theme-switch");
  themeForm?.addEventListener("submit", () => {
    themeForm.elements.return_to.value = location.pathname + location.search + location.hash;
  });
  document.addEventListener("keydown", (event) => {
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k" && search) {
      event.preventDefault();
      toggle.checked = false;
      updateNavigation(false);
      search.focus();
      search.select();
    }
    if (event.key === "Escape" && mobile.matches && toggle?.checked) {
      toggle.checked = false;
      updateNavigation(true);
    }
    if (event.key === "Tab" && mobile.matches && toggle?.checked) {
      const controls = [...sidebar.querySelectorAll('a, button, summary, input:not([type="hidden"])')].filter((el) => {
        if (!el.getClientRects().length || el.disabled || el.tabIndex < 0) return false;
        // Collapsed details can retain layout boxes in Chromium. Only their
        // summary belongs in the keyboard order, not the hidden descendants.
        for (let parent = el.parentElement; parent && parent !== sidebar; parent = parent.parentElement) {
          if (parent.tagName === "DETAILS" && !parent.open && !parent.querySelector(":scope > summary")?.contains(el)) return false;
        }
        return getComputedStyle(el).visibility !== "hidden";
      });
      const first = controls[0], last = controls.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }
  });

  const tablist = document.querySelector(".detail-tabs");
  if (tablist) {
    const tabs = [...tablist.querySelectorAll("a")];
    const panels = [...document.querySelectorAll("[data-detail-panel]")];
    tablist.setAttribute("role", "tablist");
    for (const tab of tabs) {
      tab.setAttribute("role", "tab");
      tab.id = "tab-" + tab.hash.slice(1);
      tab.setAttribute("aria-controls", tab.hash.slice(1));
    }
    for (const panel of panels) {
      panel.setAttribute("role", "tabpanel");
      panel.setAttribute("aria-labelledby", "tab-" + panel.id);
      panel.tabIndex = 0;
    }
    function activate() {
      const current = tabs.find((tab) => tab.hash === location.hash) || tabs[0];
      for (const tab of tabs) {
        const selected = tab === current;
        tab.setAttribute("aria-selected", String(selected));
        tab.tabIndex = selected ? 0 : -1;
        tab.classList.toggle("active", selected);
      }
      for (const panel of panels) panel.hidden = "#" + panel.id !== current.hash;
      updateTables();
    }
    for (const tab of tabs) tab.addEventListener("click", (event) => {
      event.preventDefault();
      history.pushState(null, "", tab.hash);
      activate();
    });
    tablist.addEventListener("keydown", (event) => {
      const current = tabs.indexOf(document.activeElement);
      if (current < 0) return;
      let index;
      if (event.key === "ArrowRight") index = (current + 1) % tabs.length;
      else if (event.key === "ArrowLeft") index = (current + tabs.length - 1) % tabs.length;
      else if (event.key === "Home") index = 0;
      else if (event.key === "End") index = tabs.length - 1;
      else return;
      event.preventDefault();
      tabs[index].click();
      tabs[index].focus();
    });
    window.addEventListener("hashchange", activate);
    window.addEventListener("popstate", activate);
    activate();
    window.addEventListener("beforeprint", () => panels.forEach((panel) => { panel.hidden = false; }));
    window.addEventListener("afterprint", activate);
  }

  const bulk = document.getElementById("bulkform");
  const checks = [...document.querySelectorAll('input[name="ids"][form="bulkform"]')];
  const selectAll = document.querySelector("[data-select-all]");
  function updateSelection() {
    const selected = checks.filter((checkbox) => checkbox.checked).length;
    const count = document.querySelector("[data-selection-count]");
    if (count) count.textContent = selected ? `${selected} selected` : "Select rows to apply an action";
    if (bulk) bulk.querySelectorAll('button[type="submit"]').forEach((button) => { button.disabled = selected === 0; });
    if (selectAll) {
      selectAll.checked = selected > 0 && selected === checks.length;
      selectAll.indeterminate = selected > 0 && selected < checks.length;
    }
    for (const checkbox of checks) checkbox.closest("tr")?.classList.toggle("selected", checkbox.checked);
  }
  selectAll?.addEventListener("change", () => { for (const checkbox of checks) checkbox.checked = selectAll.checked; updateSelection(); });
  for (const checkbox of checks) checkbox.addEventListener("change", updateSelection);
  updateSelection();

  // Wide tables can be scrolled with the keyboard without trapping focus.
  function updateTables() {
    for (const wrapper of document.querySelectorAll(".table-wrap")) {
      if (wrapper.scrollWidth > wrapper.clientWidth && wrapper.clientWidth > 0) {
        wrapper.tabIndex = 0;
        wrapper.setAttribute("role", "region");
        wrapper.setAttribute("aria-label", "Scrollable table: " + (wrapper.querySelector("caption")?.textContent || document.querySelector("h1")?.textContent || "Inventory"));
      } else {
        wrapper.removeAttribute("tabindex");
        wrapper.removeAttribute("role");
        wrapper.removeAttribute("aria-label");
      }
    }
  }
  updateTables();
  window.addEventListener("resize", updateTables);
  window.addEventListener("hashchange", updateTables);
  document.addEventListener("toggle", updateTables, true);

  // The value is always available as text, including when clipboard access fails.
  for (const button of document.querySelectorAll("[data-copy-target]")) {
    button.addEventListener("click", async () => {
      const source = document.getElementById(button.dataset.copyTarget);
      const status = button.parentElement.querySelector("[data-copy-status]");
      if (!source || !status) return;
      const values = [...source.querySelectorAll("li code")];
      const value = values.length ? values.map((item) => item.textContent).join("\n") : source.textContent.trim();
      try {
        await navigator.clipboard.writeText(value);
        status.textContent = "Copied to clipboard.";
      } catch {
        const range = document.createRange();
        range.selectNodeContents(source);
        const selection = window.getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        status.textContent = "Automatic copy is unavailable. The text is selected; copy it with your keyboard.";
      }
    });
  }

  const error = document.querySelector('.error[role="alert"]');
  if (error) {
    for (let parent = error.parentElement; parent; parent = parent.parentElement) {
      if (parent.tagName === "DETAILS") parent.open = true;
    }
    error.focus();
  }
})();
