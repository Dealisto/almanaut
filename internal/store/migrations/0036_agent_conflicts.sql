-- Make clone conflicts displayable. agentReport rejects a contested report from
-- inside store.WithTx, so its transaction rolls back and nothing survives it;
-- the conflict is instead stamped here by a separate write afterwards. The
-- binding row is the right home because the binding is exactly what is being
-- contested: the original machine keeps the host, and a second machine using
-- the same agent id is what gets rejected.
--
-- Both columns are cleared by the next successful report from the rightful
-- agent, so the UI stops warning once the operator has fixed the clone.
ALTER TABLE host_agents ADD COLUMN last_conflict_at TEXT NOT NULL DEFAULT '';
ALTER TABLE host_agents ADD COLUMN last_conflict_hostname TEXT NOT NULL DEFAULT '';
