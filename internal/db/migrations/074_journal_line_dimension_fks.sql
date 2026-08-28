-- journal_lines.cost_center_id and project_id had no foreign key, so an
-- accounting dimension could be deleted out from under a posted line, silently
-- orphaning it. Verified zero orphans across 2,969 lines before constraining.
--
-- RESTRICT, not CASCADE: deleting a dimension a posted line depends on should
-- fail, not quietly take the line with it.
ALTER TABLE finance.journal_lines
  DROP CONSTRAINT IF EXISTS journal_lines_cost_center_id_fkey;
ALTER TABLE finance.journal_lines
  ADD CONSTRAINT journal_lines_cost_center_id_fkey FOREIGN KEY (cost_center_id)
  REFERENCES finance.cost_centers (id) ON DELETE RESTRICT;

ALTER TABLE finance.journal_lines
  DROP CONSTRAINT IF EXISTS journal_lines_project_id_fkey;
ALTER TABLE finance.journal_lines
  ADD CONSTRAINT journal_lines_project_id_fkey FOREIGN KEY (project_id)
  REFERENCES finance.projects (id) ON DELETE RESTRICT;
