-- Knowledge entries no longer carry an audience (owner decision 2026-10-30,
-- #10512). Applicability lives in Guidance bindings, which keep their own
-- audience. Revisions keep audience_json as history; new revisions leave it null.
alter table den_knowledge.knowledge_entries drop column audience_json;
