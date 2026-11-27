// Run against the real hosts page after its focused desktop sidebar collapses.
(async () => {
  const results = [];
  const check = (name, condition) => results.push({name, passed: Boolean(condition)});
  const settle = () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  await settle();
  const toggle = document.getElementById("nav-toggle");
  const sidebar = document.getElementById("sidebar");
  const content = document.getElementById("app-content");
  const search = document.getElementById("global-search");
  check("collapsing a focused desktop sidebar preserves keyboard focus", document.activeElement === toggle);
  check("closed mobile navigation is inert", sidebar.inert && !content.inert && !toggle.checked);
  toggle.click();
  await settle();
  check("opening mobile navigation moves focus into the drawer", sidebar.contains(document.activeElement) && !sidebar.inert && content.inert);
  check("opening mobile navigation exposes its expanded state", toggle.getAttribute("aria-expanded") === "true");
  const close = document.querySelector(".nav-close");
  close.focus();
  close.dispatchEvent(new KeyboardEvent("keydown", {key: "Tab", shiftKey: true, bubbles: true, cancelable: true}));
  check("Shift-Tab from the drawer's first control wraps inside it", sidebar.contains(document.activeElement) && document.activeElement !== close);
  document.activeElement.dispatchEvent(new KeyboardEvent("keydown", {key: "Tab", bubbles: true, cancelable: true}));
  check("Tab from the drawer's last control returns to its close button", document.activeElement === close);
  document.dispatchEvent(new KeyboardEvent("keydown", {key: "Escape", bubbles: true}));
  await settle();
  check("Escape closes the drawer and restores its toggle", !toggle.checked && !content.inert && document.activeElement === toggle);
  toggle.click();
  document.dispatchEvent(new KeyboardEvent("keydown", {key: "k", ctrlKey: true, bubbles: true, cancelable: true}));
  await settle();
  check("search shortcut wins over a pending drawer focus change", document.activeElement === search && !toggle.checked && !content.inert);
  // Form controls may be restored independently of JavaScript state.
  toggle.checked = true;
  window.dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true}));
  await settle();
  check("restored open navigation reconciles accessibility and scroll state", !sidebar.inert && content.inert &&
    toggle.getAttribute("aria-expanded") === "true" && document.body.classList.contains("nav-open"));
  check("restored open navigation moves focus out of inert content", sidebar.contains(document.activeElement));
  toggle.checked = false;
  window.dispatchEvent(new PageTransitionEvent("pageshow", {persisted: true}));
  await settle();
  check("restored closed navigation clears stale accessibility and scroll state", sidebar.inert && !content.inert &&
    toggle.getAttribute("aria-expanded") === "false" && !document.body.classList.contains("nav-open"));
  check("restored closed navigation recovers focus from the hidden drawer", document.activeElement === toggle);
  return results;
})()
