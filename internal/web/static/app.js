/* Progressive enhancements: navigation, search shortcuts, detail tabs and selection.
   Every form and inventory page remains usable without JavaScript. */
"use strict";
(() => {
  function revealDetails(element) {
    for (let parent = element.parentElement; parent; parent = parent.parentElement) {
      if (parent.tagName === "DETAILS") parent.open = true;
    }
  }
  // Native constraint validation cannot focus a control inside a collapsed
  // disclosure. Reveal it synchronously, then let the browser show its message.
  document.addEventListener("invalid", (event) => revealDetails(event.target), true);
  // Use the submitter so mixed-action forms confirm only destructive actions,
  // including submissions triggered by the keyboard or requestSubmit().
  document.addEventListener("submit", (event) => {
    const message = event.submitter?.dataset.confirm || event.target.dataset.confirm;
    if (message && !window.confirm(message)) event.preventDefault();
  });
  document.documentElement.classList.add("js");
  const toggle = document.getElementById("nav-toggle");
  const sidebar = document.getElementById("sidebar");
  const content = document.getElementById("app-content");
  const mobile = matchMedia("(max-width: 860px)");
  const search = document.getElementById("global-search");
  let navigationFocusFrame;
  function updateNavigation(moveFocus) {
    if (!toggle || !sidebar || !content) return;
    // A newer action (such as the search shortcut) owns focus now.
    cancelAnimationFrame(navigationFocusFrame);
    const open = mobile.matches && toggle.checked;
    sidebar.inert = mobile.matches && !open;
    content.inert = open;
    toggle.setAttribute("aria-expanded", String(open));
    document.body.classList.toggle("nav-open", open);
    if (moveFocus) {
      // Let the checkbox's native click and drawer visibility update finish
      // before placing keyboard focus inside the newly opened navigation.
      navigationFocusFrame = requestAnimationFrame(() => {
        if (!mobile.matches || toggle.checked) sidebar.querySelector("a")?.focus();
        else toggle.focus();
      });
    }
  }
  toggle?.addEventListener("change", () => updateNavigation(true));
  document.querySelector(".nav-close")?.addEventListener("click", () => {
    toggle.checked = false;
    updateNavigation(true);
  });
  // A browser may blur CSS-hidden controls before its media-query callback.
  // Recover at that point while the element losing focus is still known.
  sidebar?.addEventListener("focusout", (event) => {
    if (event.relatedTarget) return;
    if (mobile.matches && !toggle.checked && getComputedStyle(event.target).visibility === "hidden") {
      toggle.focus({preventScroll: true});
    } else if (!mobile.matches && event.target.matches(".nav-close")) {
      sidebar.querySelector("a")?.focus({preventScroll: true});
    }
  });
  toggle?.addEventListener("focusout", (event) => {
    if (!event.relatedTarget && !mobile.matches) sidebar?.querySelector("a")?.focus({preventScroll: true});
  });
  mobile.addEventListener("change", () => {
    // Responsive navigation must not leave focus in a newly hidden control.
    const moveFocus = mobile.matches ? sidebar?.contains(document.activeElement)
      : document.activeElement === toggle || document.activeElement?.matches(".nav-close");
    if (toggle) toggle.checked = false;
    updateNavigation(moveFocus);
  });
  updateNavigation(false);
  // History navigation may restore the checkbox after initialization without
  // a change event. Reconcile inert regions before restoring usable focus.
  window.addEventListener("pageshow", () => {
    const focusInSidebar = sidebar?.contains(document.activeElement);
    const open = mobile.matches && toggle?.checked;
    updateNavigation(open ? !focusInSidebar : mobile.matches && focusInSidebar);
  });
  const themeForm = document.querySelector(".theme-switch");
  let themePending = false;
  themeForm?.addEventListener("submit", async (event) => {
    themeForm.elements.return_to.value = location.pathname + location.search + location.hash;
    if (!window.fetch || !window.AbortController) return;
    event.preventDefault();
    if (themePending) return;
    const status = document.getElementById("theme-status");
    const buttons = [...themeForm.querySelectorAll('button[name="theme"]')];
    const body = new URLSearchParams(new FormData(themeForm));
    body.set("theme", event.submitter?.value || "system");
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    themePending = true;
    themeForm.setAttribute("aria-busy", "true");
    // Keep the focused button focusable while ignoring further submissions.
    buttons.forEach(button => button.setAttribute("aria-disabled", "true"));
    if (status) status.textContent = "Saving appearance…";
    try {
      const response = await fetch(themeForm.action, {
        method: "POST", body, headers: {Accept: "application/json"},
        credentials: "same-origin", mode: "same-origin", redirect: "error", signal: controller.signal
      });
      if (!response.ok || response.redirected || response.headers.get("Content-Type")?.split(";")[0] !== "application/json") {
        throw new Error("Appearance was not confirmed");
      }
      const result = await response.json();
      if (!["system", "light", "dark"].includes(result.theme)) throw new Error("Invalid appearance response");
      document.documentElement.dataset.theme = result.theme;
      for (const button of buttons) {
        const selected = button.value === result.theme;
        button.classList.toggle("active", selected);
        button.setAttribute("aria-pressed", String(selected));
      }
      if (status) status.textContent = "Appearance saved.";
    } catch {
      // Never fall back to navigation here: it would discard the draft that
      // this enhancement protects, including after a session expires.
      if (status) status.textContent = "Could not save appearance. Your unsaved changes are still here. Try again.";
    } finally {
      clearTimeout(timeout);
      themePending = false;
      themeForm.removeAttribute("aria-busy");
      buttons.forEach(button => button.removeAttribute("aria-disabled"));
    }
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
      let target;
      try { target = document.getElementById(decodeURIComponent(location.hash.slice(1))); } catch { /* Ignore malformed fragments. */ }
      const panel = target?.closest("[data-detail-panel]");
      const revealTarget = panel?.hidden && target !== panel;
      const current = tabs.find((tab) => tab.hash === location.hash || tab.hash === "#" + panel?.id)
        || (location.hash && tabs.find((tab) => tab.getAttribute("aria-selected") === "true")) || tabs[0];
      const previousFocus = document.activeElement;
      for (const tab of tabs) {
        const selected = tab === current;
        tab.setAttribute("aria-selected", String(selected));
        tab.tabIndex = selected ? 0 : -1;
        tab.classList.toggle("active", selected);
      }
      for (const panel of panels) panel.hidden = "#" + panel.id !== current.hash;
      // A section link inside the old panel must not leave keyboard focus in
      // content that has just become hidden.
      if (previousFocus.closest("[data-detail-panel]")?.hidden) {
        document.getElementById(current.hash.slice(1)).focus({preventScroll: true});
      }
      if (revealTarget) {
        // Native fragment scrolling can run while the destination is hidden.
        // Retry once its panel is visible, unless navigation moved on already.
        const hash = location.hash;
        requestAnimationFrame(() => {
          if (location.hash === hash && !panel.hidden) target.scrollIntoView();
        });
      }
      updateTables();
    }
    for (const tab of tabs) tab.addEventListener("click", (event) => {
      // Section links retain normal browser behavior for a new tab or window.
      if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      if (location.hash === tab.hash) return;
      history.pushState(null, "", tab.hash);
      activate();
    });
    tablist.addEventListener("keydown", (event) => {
      // Modified arrows/Home/End belong to browser and OS navigation (for
      // example Alt+Left for Back), not to the record's tab selection.
      if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      const current = tabs.indexOf(document.activeElement);
      if (current < 0) return;
      if (event.key === " ") {
        event.preventDefault();
        tabs[current].click();
        return;
      }
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

  function enhanceSelection(form, checks, selectAll, count) {
    if (!form) return;
    function updateSelection() {
      const selected = checks.filter((checkbox) => checkbox.checked).length;
      if (count) count.textContent = selected ? `${selected} selected` : "Select rows on this page to apply an action";
      form.querySelectorAll('button[type="submit"]').forEach((button) => { button.disabled = selected === 0; });
      if (selectAll) {
        selectAll.disabled = checks.length === 0;
        selectAll.checked = selected > 0 && selected === checks.length;
        selectAll.indeterminate = selected > 0 && selected < checks.length;
      }
      for (const checkbox of checks) checkbox.closest("tr")?.classList.toggle("selected", checkbox.checked);
    }
    selectAll?.addEventListener("change", () => { for (const checkbox of checks) checkbox.checked = selectAll.checked; updateSelection(); });
    for (const checkbox of checks) checkbox.addEventListener("change", updateSelection);
    updateSelection();
    // History can restore values without dispatching change events.
    window.addEventListener("pageshow", updateSelection);
  }
  enhanceSelection(document.getElementById("bulkform"),
    [...document.querySelectorAll('input[name="ids"][form="bulkform"]')],
    document.querySelector("[data-select-all]"), document.querySelector("[data-selection-count]"));
  for (const form of document.querySelectorAll("[data-selection-form]")) {
    enhanceSelection(form, [...form.querySelectorAll("[data-selection-item]")],
      form.querySelector("[data-select-all]"), form.querySelector("[data-selection-count]"));
  }

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
    revealDetails(error);
    error.focus();
  }
})();
