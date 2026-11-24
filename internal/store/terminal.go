package store

import "context"

// SetCodeRun lets the coding agent run commands in a sandbox (its run
// tool), or not.
func (s *Store) SetCodeRun(ctx context.Context, orgID int64, on bool, by string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO ai_settings (org_id, code_run, updated_by, updated_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (org_id) DO UPDATE SET code_run = $2, updated_by = $3, updated_at = now()`, orgID, on, by)
	return err
}

// NoteEncargoRun adds a command the coding agent ran in its sandbox to its
// encargo's history.
func (s *Store) NoteEncargoRun(ctx context.Context, id int64, who, note string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO encargo_log (encargo_id, who, what, note) VALUES ($1, $2, 'ran', $3)`, id, who, note)
	return err
}
