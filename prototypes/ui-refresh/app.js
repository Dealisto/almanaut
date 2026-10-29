/* Standalone design prototype. All inventory data below is fictional. */
"use strict";

const icons = {
  grid: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.01M7 17.5h.01M12 6.5h5M12 17.5h5"/>',
  layers: '<path d="m12 3 10 5-10 5L2 8l10-5Zm-10 9 10 5 10-5M2 16l10 5 10-5"/>',
  network: '<rect x="8" y="2" width="8" height="6" rx="1"/><rect x="2" y="16" width="7" height="6" rx="1"/><rect x="15" y="16" width="7" height="6" rx="1"/><path d="M12 8v4M5.5 16v-4h13v4"/>',
  pulse: '<path d="M2 12h4l3-8 6 16 3-8h4"/>',
  radar: '<circle cx="12" cy="12" r="9"/><path d="m12 12 7-7M12 7a5 5 0 1 0 5 5"/><circle cx="12" cy="12" r="1"/>',
  history: '<path d="M3 11a9 9 0 1 1 2.6 7M3 4v7h7M12 7v5l3 2"/>',
  home: '<path d="m3 10 9-7 9 7v10a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1V10Z"/>',
  search: '<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
  moon: '<path d="M21 13a9 9 0 1 1-10-10 7 7 0 0 0 10 10Z"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1 1M18 18l1 1M5 19l1-1M18 6l1-1"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7h.01"/>',
  menu: '<path d="M4 6h16M4 12h16M4 18h16"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  arrow: '<path d="M4 12h16m-6-6 6 6-6 6"/>',
  chevron: '<path d="m9 5 7 7-7 7"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  shield: '<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z"/><path d="m8 12 3 3 5-6"/>',
  alert: '<path d="m10 4-8 14a2 2 0 0 0 2 3h16a2 2 0 0 0 2-3L14 4a2 2 0 0 0-4 0ZM12 9v4M12 17h.01"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  globe: '<circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/>',
  box: '<path d="m12 3 9 5v9l-9 5-9-5V8l9-5ZM3 8l9 5 9-5M12 13v9M7 5.5l10 5.5"/>',
  edit: '<path d="m15 4 5 5M4 20l5-1L21 7a2 2 0 0 0-5-5L4 14l-1 7ZM12 20h9"/>',
  cable: '<path d="M6 3v4m4-4v4M4 7h8v3a4 4 0 0 1-8 0V7ZM8 14v3a4 4 0 0 0 8 0v-3m-2-5h6v3a3 3 0 0 1-6 0V9ZM16 5v4m2-4v4"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  download: '<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>',
  refresh: '<path d="M20 7a9 9 0 0 0-15-2L2 8m0-6v6h6M4 17a9 9 0 0 0 15 2l3-3m0 6v-6h-6"/>',
  link: '<path d="m10 13 4-4M8 16l-1 1a4 4 0 0 1-6-6l5-5a4 4 0 0 1 6 0m0 12a4 4 0 0 0 6 0l5-5a4 4 0 0 0-6-6l-1 1"/>',
};
const icon = (name) => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${icons[name] || icons.box}</svg>`;
const escapeHTML = (value) => String(value ?? "").replace(/[&<>"']/g, (char) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[char]));
const byId = (id) => document.getElementById(id);
const seedHosts = [
  { id: "atlas", name: "atlas", type: "Physical", os: "Proxmox VE 8.4", ip: "192.168.10.10", status: "online", tag: "production", role: "Primary hypervisor", port: 1, cpu: "Intel Core i5-12500", memory: "64 GB", location: "Home / Office / Rack 01 · U4", notes: "Primary virtualization host. Management lives on VLAN 10; virtual machines use the trusted network. Maintenance window: Sunday morning." },
  { id: "docker-01", name: "docker-01", type: "Virtual machine", os: "Debian 12", ip: "192.168.10.20", status: "online", tag: "production", role: "Application server", port: 2, cpu: "4 vCPU", memory: "16 GB", location: "Home / Office / Rack 01", notes: "Runs the main application stack. Hosted on atlas." },
  { id: "storage-01", name: "storage-01", type: "Physical", os: "TrueNAS SCALE", ip: "192.168.10.30", status: "online", tag: "production", role: "Storage & backups", port: 3, cpu: "AMD Ryzen 5 5600G", memory: "32 GB", location: "Home / Office / Rack 01 · U1–U2", notes: "Shared storage and nightly backup destination. Keep the backup dataset separate from shared media." },
  { id: "media-01", name: "media-01", type: "Virtual machine", os: "Ubuntu 24.04", ip: "192.168.10.40", status: "offline", tag: "media", role: "Media server", port: 4, cpu: "4 vCPU", memory: "8 GB", location: "Home / Office / Rack 01", notes: "Runs Jellyfin. Last successful liveness check was 18 minutes before this sample snapshot." },
  { id: "home-assistant", name: "home-assistant", type: "Virtual machine", os: "Home Assistant OS", ip: "192.168.20.10", status: "online", tag: "home", role: "Home automation", port: 5, cpu: "2 vCPU", memory: "4 GB", location: "Home / Office / Rack 01", notes: "Home automation on the isolated IoT network." },
  { id: "dns-01", name: "dns-01", type: "Container", os: "Alpine Linux", ip: "192.168.10.53", status: "online", tag: "production", role: "Local DNS", port: 0, cpu: "1 vCPU", memory: "512 MB", location: "Home / Office / Rack 01", notes: "Local DNS resolver, hosted on atlas." },
  { id: "devbox", name: "devbox", type: "Virtual machine", os: "Debian 12", ip: "192.168.10.60", status: "online", tag: "development", role: "Development sandbox", port: 0, cpu: "4 vCPU", memory: "8 GB", location: "Home / Office / Rack 01", notes: "Disposable development environment, hosted on atlas." },
  { id: "edge", name: "edge", type: "Physical", os: "OPNsense", ip: "192.168.10.1", status: "online", tag: "production", role: "Router & firewall", port: 8, cpu: "Intel N100", memory: "8 GB", location: "Home / Office / Rack 01 · U6", notes: "Routes the trusted, IoT and guest networks. Switch uplink is connected to port 8." },
];
let hosts = structuredClone(seedHosts);
const services = [
  { id: "proxmox", name: "Proxmox", category: "Virtualization", host: "atlas", initials: "Px", color: "orange-logo", status: "online", address: "https://atlas.home.arpa:8006", description: "Manage virtual machines and containers on the primary hypervisor." },
  { id: "home-assistant", name: "Home Assistant", category: "Automation", host: "home-assistant", initials: "HA", color: "", status: "online", address: "http://192.168.20.10:8123", description: "Home automation, devices and routines on the IoT network." },
  { id: "jellyfin", name: "Jellyfin", category: "Media", host: "media-01", initials: "Jf", color: "", status: "offline", address: "https://media.home.arpa", description: "Personal media library. Its host is currently unreachable in this sample." },
  { id: "grafana", name: "Grafana", category: "Monitoring", host: "docker-01", initials: "Gr", color: "orange-logo", status: "online", address: "https://metrics.home.arpa", description: "Dashboards for homelab metrics and monitoring." },
  { id: "vaultwarden", name: "Vaultwarden", category: "Security", host: "docker-01", initials: "Vw", color: "green-logo", status: "online", address: "https://vault.home.arpa", description: "Password vault. Backups are documented on storage-01." },
  { id: "adguard", name: "AdGuard Home", category: "Networking", host: "dns-01", initials: "Ag", color: "green-logo", status: "online", address: "http://192.168.10.53:3000", description: "DNS filtering and name resolution for the trusted network." },
];
const networks = [
  { id: "trusted", name: "Trusted", cidr: "192.168.10.0/24", prefix: "192.168.10.", vlan: 10, purpose: "Servers & personal devices", reserved: 12, color: "green" },
  { id: "iot", name: "IoT", cidr: "192.168.20.0/24", prefix: "192.168.20.", vlan: 20, purpose: "Home automation & smart devices", reserved: 4, color: "orange" },
  { id: "guest", name: "Guest", cidr: "192.168.30.0/24", prefix: "192.168.30.", vlan: 30, purpose: "Isolated guest devices", reserved: 0, color: "" },
];
let events = [
  { title: "media-01 is unreachable", description: "Liveness check · TCP connection failed", time: "18 min ago", icon: "pulse", route: "host/media-01" },
  { title: "docker-01 updated", description: "IP address and operating system", time: "42 min ago", icon: "edit", route: "host/docker-01" },
  { title: "atlas connected to core-switch", description: "eno1 ↔ Port 1 · 1 Gbps", time: "1 hour ago", icon: "cable", route: "network?tab=ports" },
  { title: "Grafana added to inventory", description: "Service · runs on docker-01", time: "2 hours ago", icon: "plus", route: "services" },
];
let source = "network";
let discoveryDone = false;
let scanning = false;
let selectedHosts = new Set();
const candidates = [
  { id: "lab-mini", source: "network", name: "lab-mini", ip: "192.168.10.71", os: "Debian 12", type: "Physical" },
  { id: "backup-node", source: "network", name: "backup-node", ip: "192.168.10.72", os: "Ubuntu 24.04", type: "Physical" },
  { id: "test-vm", source: "proxmox", name: "test-vm", ip: "192.168.10.81", os: "Debian 12", type: "Virtual machine" },
  { id: "lab-lxc", source: "proxmox", name: "lab-lxc", ip: "192.168.10.82", os: "Alpine Linux", type: "Container" },
  { id: "immich", source: "docker", name: "immich", ip: "172.17.0.2", os: "Linux container", type: "Container" },
  { id: "paperless", source: "docker", name: "paperless", ip: "172.17.0.3", os: "Linux container", type: "Container" },
];
let toastTimer;
let modalTrigger;

function hydrateIcons(root = document) {
  root.querySelectorAll("[data-icon]").forEach((element) => { element.innerHTML = icon(element.dataset.icon); });
}
function badge(status) { return `<span class="badge ${status}"><span class="dot"></span>${status === "online" ? "Online" : status === "offline" ? "Offline" : "Not checked"}</span>`; }
function routeState() {
  const [path, query] = location.hash.slice(1).split("?");
  return { path: path || "overview", params: new URLSearchParams(query || "") };
}
function heading(eyebrow, title, subtitle, actions = "") {
  return `<div class="page-heading"><div><span class="eyebrow">${eyebrow}</span><h1>${title}</h1><p class="page-subtitle">${subtitle}</p></div><div class="heading-actions">${actions}</div></div>`;
}
function addButton() { return `<button class="button primary" data-action="add-host">${icon("plus")}Add host</button>`; }
function panelHeader(title, link, count) { return `<div class="panel-head"><div class="panel-title"><h2>${title}</h2>${count !== undefined ? `<span class="pill-count">${count}</span>` : ""}</div>${link || ""}</div>`; }
function textLink(text, route) { return `<a class="text-link" href="#${route}">${text}${icon("arrow")}</a>`; }
function attentionRows() {
  return `<a class="attention-row" href="#host/media-01"><span class="attention-icon critical">${icon("pulse")}</span><div class="attention-copy"><strong>media-01 is unreachable</strong><p>Jellyfin may be unavailable · Last seen 18 min ago</p></div>${icon("chevron")}</a>
  <button class="attention-row" style="width:100%;text-align:left;background:transparent;color:inherit" data-action="certificate"><span class="attention-icon">${icon("shield")}</span><div class="attention-copy"><strong>A certificate expires in 7 days</strong><p>*.home.arpa · Review before November 5</p></div>${icon("chevron")}</button>
  <button class="attention-row" style="width:100%;text-align:left;background:transparent;color:inherit" data-action="backup"><span class="attention-icon">${icon("box")}</span><div class="attention-copy"><strong>Grafana has no documented backup</strong><p>Add a backup relationship to complete its record</p></div>${icon("chevron")}</button>`;
}
function activityList(all = false) {
  return `<div class="activity-list">${events.slice(0, all ? events.length : 4).map((event) => `<a class="activity-item" href="#${event.route}"><span class="activity-icon">${icon(event.icon)}</span><div><strong>${escapeHTML(event.title)}</strong><p>${escapeHTML(event.description)}</p></div><time>${event.time}</time></a>`).join("")}</div>`;
}
function serviceCards(list = services) {
  return list.map((service) => `<button class="service-card" style="text-align:left" data-action="service" data-id="${service.id}"><span class="service-logo ${service.color}">${service.initials}</span><span class="dot ${service.status === "online" ? "green" : "red"}" title="${service.status}"></span><strong>${service.name}</strong><small>${service.category} · ${service.status === "online" ? "Online" : "Offline"}</small><div class="service-host">${service.host}</div></button>`).join("");
}
function overview() {
  const online = hosts.filter((host) => host.status === "online").length;
  const offline = hosts.filter((host) => host.status === "offline").length;
  const unknown = hosts.length - online - offline;
  return heading("YOUR HOMELAB, AT A GLANCE", "Your homelab, in focus.", "What needs your attention, and everything you need to find it.", `<a href="#discovery" class="button secondary-action">${icon("radar")}Discover devices</a>${addButton()}`) +
    `<div class="stats">
      <a class="stat" href="#hosts"><div class="stat-top">Hosts${icon("server")}</div><div class="stat-value">${hosts.length}</div><div class="stat-foot"><span class="dot green"></span><span>${online} online · ${offline} offline${unknown ? ` · ${unknown} unchecked` : ""}</span></div></a>
      <a class="stat" href="#services"><div class="stat-top">Services${icon("layers")}</div><div class="stat-value">6</div><div class="stat-foot"><span class="dot green"></span><span>5 online · 1 offline</span></div></a>
      <a class="stat" href="#network"><div class="stat-top">Networks${icon("network")}</div><div class="stat-value">3</div><div class="stat-foot">Trusted, IoT & guest</div></a>
      <a class="stat" href="#health"><div class="stat-top">Needs attention${icon("alert")}</div><div class="stat-value">3</div><div class="stat-foot"><span class="dot orange"></span><span>1 offline host · 2 reminders</span></div></a>
    </div>
    <div class="dashboard-grid"><div class="dashboard-column">
      <section class="panel">${panelHeader("Let's take care of these", textLink("View all", "health"), 3)}${attentionRows()}<div class="attention-footer">${icon("clock")}Example snapshot · October 29, 2026 at 10:42</div></section>
      <section class="panel services-panel">${panelHeader("Your services", textLink("All services", "services"))}<div class="service-grid">${serviceCards()}</div></section>
    </div><div class="dashboard-column">
      <section class="panel map-panel">${panelHeader("How it connects", textLink("Explore", "network?tab=ports"))}
        <div class="topology"><div class="topology-level"><a class="topology-node" href="#host/edge">${icon("globe")}<div><strong>edge</strong><small>Router · OPNsense</small></div><span class="dot green"></span></a></div><div class="topology-line"></div>
        <div class="topology-level"><a class="topology-node focus" href="#network?tab=ports">${icon("network")}<div><strong>core-switch</strong><small>6 of 8 ports connected</small></div></a></div><div class="topology-line" style="height:17px"></div><div class="topology-branches"></div>
        <div class="topology-level topology-leaves">${["atlas", "storage-01", "media-01"].map((id) => `<a class="topology-node" href="#host/${id}">${icon("server")}<strong>${id}</strong><small><span class="dot ${id === "media-01" ? "red" : "green"}"></span> ${id === "media-01" ? "Offline" : "Online"}</small></a>`).join("")}</div></div><div class="map-legend"><span>Selected physical connections</span></div>
      </section>
      <section class="panel services-panel">${panelHeader("Recent activity", textLink("History", "activity"))}${activityList()}</section>
      <div class="discovery-tip">${icon("radar")}<div><strong>Less typing. More discovering.</strong><p>Find devices on your network and review them before adding them to your inventory.</p>${textLink("Try discovery", "discovery")}</div></div>
    </div></div>`;
}
function hostRows(list) {
  if (!list.length) return `<tr><td colspan="7"><div class="empty-state">${icon("search")}<h3>No hosts match these filters</h3><p>Try another name, IP address, or status.</p><button class="button" data-action="reset-filters">Clear filters</button></div></td></tr>`;
  return list.map((host) => `<tr><td><div class="entity-cell"><span class="entity-icon">${icon(host.type === "Container" ? "box" : "server")}</span><div><a class="entity-name" href="#host/${host.id}">${escapeHTML(host.name)}</a><span class="entity-sub">${escapeHTML(host.role)}</span></div></div></td><td>${escapeHTML(host.type)}</td><td><span class="mono">${escapeHTML(host.ip || "—")}</span></td><td>${badge(host.status)}</td><td>${escapeHTML(host.os || "—")}</td><td><span class="badge">${escapeHTML(host.tag)}</span></td><td><a class="icon-button" href="#host/${host.id}" aria-label="View ${escapeHTML(host.name)}">${icon("chevron")}</a></td></tr>`).join("");
}
function hostsPage(params) {
  const tag = params.get("tag") || "";
  return heading("INVENTORY", "Hosts", "Your machines, their roles, and where to find them.", addButton()) +
    `<section class="panel"><div class="toolbar"><label class="input-wrap">${icon("search")}<input id="host-search" type="search" placeholder="Find a host or IP address…" aria-label="Filter hosts"></label><select id="host-status" aria-label="Filter by status"><option value="">All statuses</option><option value="online" ${params.get("status") === "online" ? "selected" : ""}>Online</option><option value="offline" ${params.get("status") === "offline" ? "selected" : ""}>Offline</option><option value="unknown">Not checked</option></select><select id="host-type" aria-label="Filter by host type"><option value="">All types</option><option>Physical</option><option>Virtual machine</option><option>Container</option></select><select id="host-tag" aria-label="Filter by tag"><option value="">All tags</option>${[...new Set(hosts.map((host) => host.tag))].map((item) => `<option ${item === tag ? "selected" : ""}>${escapeHTML(item)}</option>`).join("")}</select></div><div class="table-scroll"><table><thead><tr><th>HOST</th><th>TYPE</th><th>IP ADDRESS</th><th>LIVENESS</th><th>OPERATING SYSTEM</th><th>TAG</th><th><span aria-label="Actions"></span></th></tr></thead><tbody id="host-rows">${hostRows(hosts)}</tbody></table></div><div class="table-footer"><span id="filter-count" role="status">${hosts.length} hosts</span><span>Select a host to see its connections</span></div></section>
    <div class="discovery-tip">${icon("info")}<div><strong>Your inventory, at your pace.</strong><p>Add a host manually, or discover devices and choose what to import.</p>${textLink("Discover your network", "discovery")}</div></div>`;
}
function filterHosts() {
  if (!byId("host-rows")) return;
  const query = byId("host-search").value.trim().toLowerCase();
  const list = hosts.filter((host) => `${host.name} ${host.ip} ${host.role}`.toLowerCase().includes(query) && (!byId("host-status").value || host.status === byId("host-status").value) && (!byId("host-type").value || host.type === byId("host-type").value) && (!byId("host-tag").value || host.tag === byId("host-tag").value));
  byId("host-rows").innerHTML = hostRows(list);
  byId("filter-count").textContent = `${list.length} of ${hosts.length} hosts`;
}
function hostDetail(id, params) {
  const host = hosts.find((item) => item.id === id);
  if (!host) return missingPage();
  const tab = params.get("tab") || "overview";
  const related = services.filter((service) => service.host === id);
  const network = networks.find((item) => host.ip.startsWith(item.prefix));
  const tabs = [ ["overview", "Overview"], ["connections", "Connections"], ["services", `Services · ${related.length}`], ["history", "History"] ];
  let content;
  const connection = host.port ? `<a class="connection-row" href="#network?tab=ports">${icon("cable")}<div><small>PHYSICAL CONNECTION</small><strong>eno1 ↔ core-switch / Port ${host.port}</strong><small>1 Gbps · Ethernet</small></div>${icon("chevron")}</a>` : `<div class="note">No physical cable is recorded for this ${host.type.toLowerCase()}.</div>`;
  const networkRow = network ? `<button class="connection-row" style="width:100%;text-align:left;background:transparent" data-action="network-detail" data-id="${network.id}">${icon("network")}<div><small>NETWORK</small><strong>${network.name} · VLAN ${network.vlan}</strong><small>${network.cidr}</small></div>${icon("chevron")}</button>` : "";
  if (tab === "connections") content = `<section class="panel">${panelHeader("Connections", "")}${connection}${networkRow}${id !== "atlas" && host.type !== "Physical" ? `<a class="connection-row" href="#host/atlas">${icon("server")}<div><small>RUNS ON</small><strong>atlas</strong><small>Proxmox hypervisor</small></div>${icon("chevron")}</a>` : ""}</section>`;
  else if (tab === "services") content = `<section class="panel">${panelHeader("Services on this host", "")}${related.length ? `<div class="service-grid">${serviceCards(related)}</div>` : `<div class="empty-state">${icon("layers")}<h3>No services linked yet</h3><p>This host has no documented service relationship.</p>${textLink("Browse all services", "services")}</div>`}</section>`;
  else if (tab === "history") { const history = events.filter((event) => event.route === `host/${id}` || event.title.includes(host.name)); content = `<section class="panel">${panelHeader("Host history", "")}<div class="activity-list">${history.map((event) => `<div class="activity-item"><span class="activity-icon">${icon(event.icon)}</span><div><strong>${escapeHTML(event.title)}</strong><p>${escapeHTML(event.description)}</p></div><time>${event.time}</time></div>`).join("") || '<div class="note">No changes recorded for this host in the demo.</div>'}</div></section>`; }
  else content = `<div class="detail-grid"><div class="stack"><section class="panel">${panelHeader("At a glance", "")}<dl class="detail-fields">${[["IP address",host.ip || "Not set"],["Operating system",host.os || "Not set"],["Type",host.type],["Role",host.role],["CPU",host.cpu],["Memory",host.memory],["Location",host.location],["Tag",host.tag]].map(([label,value]) => `<div><dt>${label}</dt><dd>${escapeHTML(value)}</dd></div>`).join("")}</dl></section><section class="panel">${panelHeader("Notes", `<button class="text-link" data-action="edit-host" data-id="${id}">Edit notes${icon("edit")}</button>`)}<p class="note">${escapeHTML(host.notes || "No notes yet.")}</p></section></div><div class="stack"><section class="panel">${panelHeader("Connected to", "")}${connection}${networkRow}</section><section class="panel">${panelHeader("Running here", textLink("View", `host/${id}?tab=services`))}${related.length ? related.map((service) => `<button class="connection-row" style="width:100%;text-align:left;background:transparent" data-action="service" data-id="${service.id}"><span class="service-logo ${service.color}">${service.initials}</span><div><strong>${service.name}</strong><small>${service.category}</small></div><span class="dot ${service.status === "online" ? "green" : "red"}"></span></button>`).join("") : '<p class="note">No services linked to this host.</p>'}</section></div></div>`;
  return `<div class="page-heading"><div><span class="eyebrow"><a href="#hosts">HOSTS</a> / ${escapeHTML(host.type.toUpperCase())}</span><div class="detail-identity"><span class="entity-icon">${icon("server")}</span><div><h1>${escapeHTML(host.name)}</h1><div class="detail-meta">${badge(host.status)}<span>${escapeHTML(host.role)}</span></div></div></div></div><button class="button" data-action="edit-host" data-id="${id}">${icon("edit")}Edit host</button></div>${host.status === "offline" ? `<div class="banner" style="background:var(--red-soft);color:var(--red)">${icon("pulse")}This host is unreachable in the sample snapshot. Check its connection before investigating its services.</div>` : ""}<nav class="tabs" aria-label="Host details">${tabs.map(([key, label]) => `<a href="#host/${id}?tab=${key}" class="${tab === key ? "active" : ""}" ${tab === key ? 'aria-current="page"' : ""}>${label}</a>`).join("")}</nav>${content}`;
}
function networkPage(params) {
  const ports = params.get("tab") === "ports";
  return heading("NETWORK", "A place for every connection.", "Understand your subnets and follow a cable from one end to the other.") +
    `<nav class="tabs" aria-label="Network views"><a href="#network" class="${!ports ? "active" : ""}" ${!ports ? 'aria-current="page"' : ""}>Networks & IPs</a><a href="#network?tab=ports" class="${ports ? "active" : ""}" ${ports ? 'aria-current="page"' : ""}>Ports & cabling</a></nav>` +
    (ports ? `<section class="panel">${panelHeader("core-switch", '<span class="badge online"><span class="dot"></span>6 connections</span>')}<p class="panel-description">Home / Office / Rack 01 · U5</p><div class="switch-enclosure"><div class="switch-label">${icon("network")}8-port managed switch<small>Select a port to trace its connection</small></div><div class="port-grid">${Array.from({length:8}, (_, i) => { const host = hosts.find((item) => item.port === i + 1); return `<button class="port ${host ? "connected" : ""}" data-action="port" data-id="${i + 1}" aria-label="Port ${i + 1}: ${host ? escapeHTML(host.name) : "available"}"><span class="port-socket" aria-hidden="true"></span>${String(i + 1).padStart(2, "0")}</button>`; }).join("")}</div><div class="port-legend"><span><span class="dot green"></span>Connected</span><span><span class="dot"></span>Available</span></div></div><div class="table-scroll"><table><thead><tr><th>SWITCH PORT</th><th>CONNECTED HOST</th><th>HOST INTERFACE</th><th>LINK SPEED</th><th>HOST LIVENESS</th></tr></thead><tbody>${hosts.filter((host) => host.port).sort((a,b) => a.port-b.port).map((host) => `<tr><td class="mono">Port ${host.port}</td><td><a href="#host/${host.id}" class="entity-name">${escapeHTML(host.name)}</a></td><td class="mono">eno1</td><td>1 Gbps</td><td>${badge(host.status)}</td></tr>`).join("")}</tbody></table></div></section>` :
    `<div class="network-cards">${networks.map((network) => { const count = hosts.filter((host) => host.ip.startsWith(network.prefix)).length; const used = count + network.reserved; return `<button class="panel network-card" data-action="network-detail" data-id="${network.id}">${icon("network")}<h2>${network.name}</h2><p><span class="mono">${network.cidr}</span> · VLAN ${network.vlan}</p><div class="capacity-bar"><span style="width:${used / 254 * 100}%"></span></div><footer><span>${count} assigned · ${network.reserved} reserved</span><span>${254 - used} available</span></footer></button>`; }).join("")}</div><section class="panel">${panelHeader("IP assignments", "", hosts.filter((host) => host.ip).length)}<div class="table-scroll"><table><thead><tr><th>HOST</th><th>IP ADDRESS</th><th>NETWORK</th><th>LIVENESS</th></tr></thead><tbody>${hosts.filter((host) => host.ip).map((host) => `<tr><td><a href="#host/${host.id}" class="entity-name">${escapeHTML(host.name)}</a></td><td class="mono">${escapeHTML(host.ip)}</td><td>${networks.find((network) => host.ip.startsWith(network.prefix))?.name || "Unassigned"}</td><td>${badge(host.status)}</td></tr>`).join("")}</tbody></table></div></section>`);
}
function servicesPage() {
  return heading("INVENTORY", "Services", "Know what runs where, and what it depends on.") + `<section class="panel">${panelHeader("Your service directory", '<span class="badge online"><span class="dot"></span>5 online</span>', services.length)}<div class="service-grid">${serviceCards()}</div></section><div class="discovery-tip">${icon("link")}<div><strong>A service is more than a URL.</strong><p>Open a service to find its host, check its status and follow its relationships.</p></div></div>`;
}
function healthPage() {
  return heading("INVENTORY HEALTH", "A short list. A clear next step.", "Start with availability, then take care of upcoming maintenance.") + `<section class="panel">${panelHeader("Needs attention", '<span class="badge warning">3 items to review</span>')}${attentionRows()}</section><div class="banner" style="margin-top:24px">${icon("info")}These are example findings. The prototype does not monitor your real infrastructure.</div>`;
}
function discoveryPage() {
  const available = candidates.filter((candidate) => candidate.source === source && !hosts.some((host) => host.id === candidate.id));
  const sources = [["network","network","Network scan","Find hosts in an IP range."],["proxmox","server","Proxmox VE","Bring in virtual machines and containers."],["docker","box","Docker","Bring containers into your inventory."]];
  return heading("DISCOVERY", "Let your homelab introduce itself.", "Choose a source. Review what was found. Add only what you need.") +
    `<div class="banner">${icon("info")}Demo mode: scans use sample results. No connection is made to your network.</div><div class="source-grid">${sources.map(([id, glyph, title, description]) => `<button class="panel source-card ${source === id ? "selected" : ""}" data-action="source" data-id="${id}" aria-pressed="${source === id}">${icon(glyph)}<h2>${title}</h2><p>${description}</p></button>`).join("")}</div><section class="panel">${panelHeader("1. Choose what to explore", "")}<div class="discovery-controls"><label>${source === "network" ? "Network range" : "Sample source"}<input value="${source === "network" ? "192.168.10.0/24" : source === "proxmox" ? "atlas · Proxmox VE" : "docker-01 · Docker"}" readonly aria-describedby="scan-note"></label><button class="button primary" data-action="scan" ${scanning ? "disabled" : ""}>${icon("radar")}${scanning ? "Scanning sample data…" : "Run demo scan"}</button></div><p class="note" id="scan-note">This concept uses a fixed sample source so you can try the review flow.</p></section>
    ${discoveryDone ? `<section class="panel" style="margin-top:24px">${panelHeader("2. Review discovered hosts", `<button class="button primary" data-action="import" ${available.length ? "" : "disabled"}>${icon("plus")}Add selected</button>`, available.length)}${available.length ? `<div class="table-scroll"><table><thead><tr><th>ADD</th><th>HOST</th><th>IP ADDRESS</th><th>OS</th><th>INVENTORY</th></tr></thead><tbody>${available.map((host) => `<tr><td><input type="checkbox" class="candidate" value="${host.id}" aria-label="Add ${host.name}" checked></td><td class="entity-name">${host.name}</td><td class="mono">${host.ip}</td><td>${host.os}</td><td><span class="badge online">New host</span></td></tr>`).join("")}</tbody></table></div>` : `<div class="empty-state">${icon("check")}<h3>You're all caught up.</h3><p>The sample hosts have been added to your inventory.</p>${textLink("View hosts", "hosts")}</div>`}</section>` : ""}`;
}
function missingPage() { return `<div class="empty-state">${icon("search")}<h1>This page is not in the concept.</h1><p>Explore the overview, inventory, network or discovery flow.</p>${textLink("Back to overview", "overview")}</div>`; }
function render() {
  const { path, params } = routeState();
  const section = path.startsWith("host/") ? "hosts" : path;
  document.querySelectorAll("[data-nav]").forEach((link) => { const active = link.dataset.nav === section; link.classList.toggle("active", active); if (active) link.setAttribute("aria-current", "page"); else link.removeAttribute("aria-current"); });
  const names = {overview:"Overview",hosts:"Hosts",services:"Services",network:"Network",health:"Needs attention",discovery:"Discover",activity:"Activity"};
  byId("breadcrumb").textContent = names[section] || "Host";
  document.title = `${names[section] || "Host"} · Almanaut concept`;
  byId("host-count").textContent = hosts.length;
  let html;
  if (path === "overview") html = overview();
  else if (path === "hosts") html = hostsPage(params);
  else if (path.startsWith("host/")) html = hostDetail(path.split("/")[1], params);
  else if (path === "services") html = servicesPage();
  else if (path === "network") html = networkPage(params);
  else if (path === "health") html = healthPage();
  else if (path === "discovery") html = discoveryPage();
  else if (path === "activity") html = heading("WORKSPACE", "A little history goes a long way.", "Changes, discoveries and checks, in one place.") + `<section class="panel">${panelHeader("Recent activity", '<span class="badge">Sample timeline</span>')}${activityList(true)}</section>`;
  else html = missingPage();
  byId("main").innerHTML = html;
  filterHosts();
}
function toast(message) { clearTimeout(toastTimer); byId("toast").textContent = message; byId("toast").classList.add("visible"); toastTimer = setTimeout(() => byId("toast").classList.remove("visible"), 3800); }
function showModal(title, body, className = "") {
  if (!byId("modal").open) modalTrigger = document.activeElement;
  byId("modal-content").innerHTML = `<div class="modal-header"><h2 id="modal-title">${title}</h2><button class="icon-button" data-action="close-modal" aria-label="Close dialog">${icon("close")}</button></div><div class="modal-body ${className}">${body}</div>`;
  if (!byId("modal").open) byId("modal").showModal();
}
function closeModal() { byId("modal").close(); }
byId("modal").addEventListener("close", () => { if (modalTrigger?.isConnected) modalTrigger.focus(); else byId("main").focus({preventScroll:true}); });
function showHostForm(id) {
  const host = hosts.find((item) => item.id === id);
  showModal(host ? `Edit ${escapeHTML(host.name)}` : "Add a host", `<p>${host ? "Update the record in this demo." : "Start with the essentials. You can fill in the details later."}</p><form id="host-form" data-id="${host?.id || ""}"><div id="form-error" class="form-error" role="alert" hidden></div><div class="form-grid"><label class="form-field full">Name <span class="entity-sub">Required</span><input name="name" value="${escapeHTML(host?.name || "")}" placeholder="e.g. atlas" required maxlength="60" autocomplete="off"></label><label class="form-field">Type<select name="type">${["Physical","Virtual machine","Container"].map((type) => `<option ${type === host?.type ? "selected" : ""}>${type}</option>`).join("")}</select></label><label class="form-field">IP address<input name="ip" value="${escapeHTML(host?.ip || "")}" placeholder="192.168.10.15" inputmode="decimal"><small>Optional · IPv4 for this demo</small></label><label class="form-field">Operating system<input name="os" value="${escapeHTML(host?.os || "")}" placeholder="e.g. Debian 12"></label><label class="form-field">Tag<input name="tag" value="${escapeHTML(host?.tag || "homelab")}" maxlength="30"></label><label class="form-field full">Notes<textarea name="notes" maxlength="2000" placeholder="What should you remember about this host?">${escapeHTML(host?.notes || "")}</textarea></label></div><div class="modal-actions"><button type="button" class="button" data-action="close-modal">Cancel</button><button class="button primary" type="submit">${host ? "Save demo changes" : "Add to demo"}</button></div></form>`);
  byId("host-form").elements.name.focus();
}
function saveHost(form) {
  const data = Object.fromEntries(new FormData(form));
  const name = data.name.trim();
  const ip = data.ip.trim();
  const existing = hosts.find((host) => host.id === form.dataset.id);
  let error = "";
  if (!name) error = "Enter a name for this host.";
  else if (hosts.some((host) => host.id !== existing?.id && host.name.toLowerCase() === name.toLowerCase())) error = "A host with this name already exists. Choose another name.";
  else if (ip && (!/^\d{1,3}(\.\d{1,3}){3}$/.test(ip) || ip.split(".").some((part) => Number(part) > 255 || (part.length > 1 && part.startsWith("0"))))) error = "Enter a valid IPv4 address, such as 192.168.10.15.";
  else if (ip && hosts.some((host) => host.id !== existing?.id && host.ip === ip)) error = "This IP address is already assigned to a host in the demo.";
  if (error) { byId("form-error").hidden = false; byId("form-error").textContent = error; return; }
  const host = existing || { id: `host-${Date.now()}`, status:"unknown", role:"New host", port:0, cpu:"Not documented", memory:"Not documented", location:"Not documented" };
  Object.assign(host, {name, ip, type:data.type, os:data.os.trim(), tag:data.tag.trim() || "homelab", notes:data.notes.trim()});
  if (!existing) hosts.push(host);
  events.unshift({title:`${name} ${existing ? "updated" : "added"}`,description:"Demo inventory · Manual change",time:"Just now",icon:existing ? "edit" : "plus",route:`host/${host.id}`});
  closeModal();
  if (location.hash === `#host/${host.id}`) render(); else location.hash = `host/${host.id}`;
  toast(`${name} ${existing ? "updated" : "added"} in the demo.`);
}
function showService(id) {
  const service = services.find((item) => item.id === id);
  if (!service) return;
  const host = hosts.find((item) => item.id === service.host);
  showModal(service.name, `<p>${service.description}</p>${badge(service.status)}<dl class="detail-fields"><div><dt>Category</dt><dd>${service.category}</dd></div><div><dt>Runs on</dt><dd><a class="text-link" href="#host/${service.host}">${escapeHTML(host?.name || service.host)}${icon("arrow")}</a></dd></div><div style="grid-column:1/-1"><dt>Service address · example only</dt><dd class="mono" style="overflow-wrap:anywhere">${service.address}</dd></div><div><dt>Backup documentation</dt><dd>${service.id === "grafana" ? '<span class="orange">No backup linked</span>' : "Linked to storage-01"}</dd></div></dl><div class="modal-actions"><a class="button primary" href="#host/${service.host}">View host${icon("arrow")}</a></div>`);
}
function showNetwork(id) {
  const network = networks.find((item) => item.id === id);
  if (!network) return;
  const count = hosts.filter((host) => host.ip.startsWith(network.prefix)).length;
  showModal(`${network.name} network`, `<p>${network.purpose}</p><dl class="detail-fields">${[["CIDR",network.cidr],["VLAN",network.vlan],["Assigned addresses",count],["Reserved addresses",network.reserved],["Available addresses",254-count-network.reserved],["Gateway",`${network.prefix}1`]].map(([label,value]) => `<div><dt>${label}</dt><dd>${value}</dd></div>`).join("")}</dl><p>Host assignments and reservations are kept together so you can see what is available.</p><div class="modal-actions"><a class="button primary" href="#network">View IP assignments${icon("arrow")}</a></div>`);
}
function showPort(number) {
  const host = hosts.find((item) => item.port === Number(number));
  showModal(`core-switch · Port ${number}`, host ? `<p>A documented physical connection. Host reachability is shown separately from the cable record.</p><div class="connection-row" style="padding:16px 0">${icon("network")}<div><small>FROM</small><strong>core-switch / Port ${number}</strong></div>${icon("cable")}</div><a class="connection-row" style="padding:16px 0" href="#host/${host.id}">${icon("server")}<div><small>TO</small><strong>${escapeHTML(host.name)} / eno1</strong></div>${badge(host.status)}</a><dl class="detail-fields"><div><dt>Link speed</dt><dd>1 Gbps</dd></div><div><dt>Cable</dt><dd>Ethernet</dd></div></dl><div class="modal-actions"><a class="button primary" href="#host/${host.id}">View connected host${icon("arrow")}</a></div>` : `<p>This port has no documented connection. It is available for another device.</p><div class="empty-state">${icon("cable")}<h3>Available port</h3><p>No host is connected in this sample inventory.</p></div>`);
}
function showSearch() {
  showModal("Find anything", `<label class="modal-search">${icon("search")}<input id="global-search" type="search" placeholder="Search hosts, services or IPs…" aria-label="Search inventory" autocomplete="off"></label><div id="search-results"></div>`);
  searchResults("");
  byId("global-search").focus();
}
function searchResults(query) {
  const q = query.toLowerCase().trim();
  const results = [
    ...hosts.map((host) => ({title:host.name,meta:`Host · ${host.ip || "No IP"}`,route:`host/${host.id}`,icon:"server",match:`${host.name} ${host.ip} ${host.tag}`})),
    ...services.map((service) => ({title:service.name,meta:`Service · ${service.host}`,action:"service",id:service.id,icon:"layers",match:`${service.name} ${service.category} ${service.host}`})),
    ...networks.map((network) => ({title:network.name,meta:`Network · ${network.cidr}`,action:"network-detail",id:network.id,icon:"network",match:`${network.name} ${network.cidr}`})),
  ].filter((item) => item.match.toLowerCase().includes(q)).slice(0, 9);
  byId("search-results").innerHTML = results.length ? results.map((item) => `<${item.route ? `a href="#${item.route}"` : `button data-action="${item.action}" data-id="${item.id}" style="width:100%;text-align:left;background:transparent"`} class="search-result">${icon(item.icon)}<div><strong>${escapeHTML(item.title)}</strong><small>${escapeHTML(item.meta)}</small></div>${icon("arrow")}</${item.route ? "a" : "button"}>`).join("") : `<div class="empty-state"><h3>No results for “${escapeHTML(query)}”</h3><p>Try a host name, service or IP address.</p></div>`;
}
function setMenu(open) {
  document.body.classList.toggle("menu-open", open);
  document.querySelector('[data-action="menu"]').setAttribute("aria-expanded", String(open));
  if (matchMedia("(max-width: 760px)").matches) {
    byId("sidebar").inert = !open;
    document.querySelector(".content-shell").inert = open;
  }
  if (open) byId("sidebar").querySelector("a").focus();
}
function setTheme(theme) {
  document.documentElement.dataset.theme = theme;
  byId("theme-toggle").innerHTML = icon(theme === "dark" ? "sun" : "moon");
  byId("theme-toggle").setAttribute("aria-label", `Switch to ${theme === "dark" ? "light" : "dark"} theme`);
  try { localStorage.setItem("almanaut-concept-theme", theme); } catch { /* File previews may disable storage. */ }
}
document.addEventListener("click", (event) => {
  const link = event.target.closest('a[href^="#"]');
  if (link?.classList.contains("skip-link")) {
    event.preventDefault();
    byId("main").focus();
    return;
  }
  if (link) {
    if (byId("modal").open) closeModal();
    setMenu(false);
    if (link.hash === location.hash) { render(); byId("main").focus({preventScroll:true}); }
  }
  const target = event.target.closest("[data-action]");
  if (!target) return;
  const action = target.dataset.action;
  const id = target.dataset.id;
  if (action === "theme") setTheme(document.documentElement.dataset.theme === "dark" ? "light" : "dark");
  else if (action === "menu") setMenu(!document.body.classList.contains("menu-open"));
  else if (action === "close-menu") { setMenu(false); document.querySelector('[data-action="menu"]').focus(); }
  else if (action === "close-modal") closeModal();
  else if (action === "search") showSearch();
  else if (action === "add-host") showHostForm();
  else if (action === "edit-host") showHostForm(id);
  else if (action === "service") showService(id);
  else if (action === "network-detail") showNetwork(id);
  else if (action === "port") showPort(id);
  else if (action === "reset-filters") { ["host-search","host-status","host-type","host-tag"].forEach((key) => byId(key).value = ""); filterHosts(); }
  else if (action === "source") { source = id; discoveryDone = false; render(); }
  else if (action === "scan" && !scanning) { scanning = true; render(); setTimeout(() => { scanning = false; discoveryDone = true; if (routeState().path === "discovery") render(); toast("Demo scan complete. Review the results before adding them."); }, 1000); }
  else if (action === "import") {
    selectedHosts = new Set([...document.querySelectorAll(".candidate:checked")].map((input) => input.value));
    if (!selectedHosts.size) { toast("Select at least one host to add."); return; }
    candidates.filter((candidate) => selectedHosts.has(candidate.id) && !hosts.some((host) => host.id === candidate.id)).forEach((candidate) => {
      hosts.push({...candidate,status:"online",tag:"discovered",role:"Discovered host",port:0,cpu:"Not documented",memory:"Not documented",location:"Not documented",notes:"Added from the sample discovery results."});
      events.unshift({title:`${candidate.name} discovered`,description:"Demo inventory · Imported from sample scan",time:"Just now",icon:"radar",route:`host/${candidate.id}`});
    });
    render(); toast(`${selectedHosts.size} host${selectedHosts.size > 1 ? "s" : ""} added to the demo inventory.`);
  }
  else if (action === "certificate") showModal("Certificate expires soon", `<p>Review this record before the certificate expires.</p><dl class="detail-fields"><div><dt>Common name</dt><dd>*.home.arpa</dd></div><div><dt>Expires</dt><dd>November 5, 2026</dd></div><div><dt>Issuer</dt><dd>Homelab internal CA</dd></div><div><dt>Renewal</dt><dd>Managed outside Almanaut</dd></div></dl><div class="banner">${icon("info")}Check renewal on your reverse proxy, then update the certificate record in Almanaut.</div>`);
  else if (action === "backup") showModal("Document a backup for Grafana", `<p>Grafana has no backup relationship in this sample inventory. Documenting a backup makes its recovery path easier to find.</p><dl class="detail-fields"><div><dt>Service</dt><dd>Grafana</dd></div><div><dt>Runs on</dt><dd>docker-01</dd></div></dl><p>In the full application, link the service to a backup record and document its schedule and restore procedure.</p><div class="modal-actions"><a class="button primary" href="#host/docker-01">View host${icon("arrow")}</a></div>`);
  else if (action === "about") showModal("A clearer home for your homelab", `<p>This is an interactive design concept for Almanaut, using fictional data.</p><p>Try the host filters, global search, host details, network connections and sample discovery flow. You can also add or edit a demo host.</p><p>Inventory changes last for this browser session. Reloading resets the sample data. The theme preference is saved locally.</p><div class="banner">${icon("info")}This is a proposed interface, not a connection to your Almanaut database. Screens outside this concept retain their existing implementation.</div><div class="modal-actions"><button class="button" data-action="reset-demo">Reset demo data</button><button class="button primary" data-action="close-modal">Explore the concept</button></div>`);
  else if (action === "reset-demo") { location.reload(); }
});
document.addEventListener("input", (event) => { if (event.target.id === "host-search") filterHosts(); if (event.target.id === "global-search") searchResults(event.target.value); });
document.addEventListener("change", (event) => { if (["host-status","host-type","host-tag"].includes(event.target.id)) filterHosts(); });
document.addEventListener("submit", (event) => { if (event.target.id === "host-form") { event.preventDefault(); saveHost(event.target); } });
document.addEventListener("keydown", (event) => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") { event.preventDefault(); setMenu(false); showSearch(); }
  if (event.key === "Escape" && document.body.classList.contains("menu-open")) { setMenu(false); document.querySelector('[data-action="menu"]').focus(); }
  if (event.key === "Tab" && document.body.classList.contains("menu-open") && !byId("modal").open) {
    const focusables = [...byId("sidebar").querySelectorAll("a,button")];
    const first = focusables[0], last = focusables.at(-1);
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }
});
const mobileQuery = matchMedia("(max-width: 760px)");
function updateMobileState() { setMenu(false); byId("sidebar").inert = mobileQuery.matches; document.querySelector(".content-shell").inert = false; }
mobileQuery.addEventListener("change", updateMobileState);
window.addEventListener("hashchange", () => { render(); window.scrollTo(0, 0); byId("main").focus({preventScroll:true}); });
hydrateIcons();
let theme = "light";
try { theme = localStorage.getItem("almanaut-concept-theme") || "light"; } catch { /* The concept also works without local storage. */ }
setTheme(theme === "dark" ? "dark" : "light");
updateMobileState();
render();
