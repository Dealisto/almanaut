-- Maps an almanaut service to the Uptime Kuma monitor almanaut created for it.
-- Rows are owned by the kuma reconciler: a monitor is "managed" iff its id is
-- here, and managed monitors are the only ones the sync will ever touch.
CREATE TABLE kuma_monitors (
    service_id INTEGER PRIMARY KEY,
    monitor_id INTEGER NOT NULL
);
