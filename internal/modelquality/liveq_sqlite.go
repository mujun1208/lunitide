package modelquality

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func ReadInstalledProviders(ctx context.Context, dbPath string) ([]InstalledProvider, error) {
	if dbPath == "" || !filepath.IsAbs(dbPath) {
		return nil, fmt.Errorf("database path must be absolute")
	}
	db, err := sql.Open("sqlite", sqliteRODSN(dbPath))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `SELECT id, name, protocol, base_url, COALESCE(credential_ref,''), COALESCE(status,''), COALESCE(credential_state,'') FROM providers WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InstalledProvider
	index := map[string]int{}
	for rows.Next() {
		var p InstalledProvider
		var credState string
		if err := rows.Scan(&p.ID, &p.Name, &p.Protocol, &p.BaseURL, &p.CredentialRef, &p.Status, &credState); err != nil {
			return nil, err
		}
		_ = credState
		index[p.ID] = len(out)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	models, err := db.QueryContext(ctx, `SELECT provider_id, model_id, COALESCE(kind,'llm'), is_default FROM provider_models`)
	if err != nil {
		models, err = db.QueryContext(ctx, `SELECT provider_id, model_id, 'llm', is_default FROM provider_models`)
		if err != nil {
			return out, nil
		}
	}
	defer models.Close()
	for models.Next() {
		var providerID, modelID, kind string
		var def int
		if err := models.Scan(&providerID, &modelID, &kind, &def); err != nil {
			return nil, err
		}
		i, ok := index[providerID]
		if !ok {
			continue
		}
		out[i].Models = append(out[i].Models, InstalledModel{ModelID: modelID, Kind: kind, IsDefault: def == 1})
	}
	return out, models.Err()
}

func sqliteRODSN(dbPath string) string {
	p := filepath.ToSlash(filepath.Clean(dbPath))
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p + "?mode=ro"
}

func InstalledDBPath(dataRoot string) string {
	return filepath.Join(dataRoot, "lunitide.db")
}
