package sqlite

import "context"

func (s *Store) AssetTemplateFileReferenced(ctx context.Context, name string) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM asset_templates WHERE file_path=?)`, name).Scan(&found)
	return found, err
}
