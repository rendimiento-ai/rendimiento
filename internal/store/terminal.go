package store

import "context"

// SetCodeRun lets the coding agent run commands in a sandbox (its run
// tool), or not: its sandbox permission (Ruta 184), always or off, and the
// old switch the previous version reads.
func (s *Store) SetCodeRun(ctx context.Context, orgID int64, on bool, by string) error {
	modo := ModoOff
	if on {
		modo = ModoAlways
	}
	return s.SetPermiso(ctx, orgID, SujetoCoder, Permiso{Permiso: PermSandbox, Modo: modo}, by)
}

// NoteEncargoRun adds a command the coding agent ran in its sandbox to its
// encargo's history.
func (s *Store) NoteEncargoRun(ctx context.Context, id int64, who, note string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO encargo_log (encargo_id, who, what, note) VALUES ($1, $2, 'ran', $3)`, id, who, note)
	return err
}
