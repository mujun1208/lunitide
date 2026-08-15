package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/ontology"
)

// CreateOntologyNode inserts a new ontology node.
func (s *Store) CreateOntologyNode(ctx context.Context, node ontology.Node) (ontology.Node, error) {
	if node.ID == "" {
		var err error
		node.ID, err = s.newULID(time.Now())
		if err != nil {
			return node, err
		}
	}
	node.CreatedAt = time.Now().UTC()
	node.UpdatedAt = node.CreatedAt
	if node.FullPath == "" {
		node.FullPath = ""
	}
	if node.Description == "" {
		node.Description = ""
	}
	if node.MetadataJSON == "" {
		node.MetadataJSON = "{}"
	}
	if node.Version < 1 {
		node.Version = 1
	}
	if err := node.Validate(); err != nil {
		return node, err
	}
	err := s.execWithAudit(ctx, "ontology.node.created", node.ID, "engine",
		map[string]any{"projectId": node.ProjectID, "type": node.Type},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO ontology_nodes(id, project_id, type, name, full_path, description, metadata_json, version, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?)`,
				node.ID, node.ProjectID, node.Type, node.Name, node.FullPath, node.Description, node.MetadataJSON, node.Version,
				formatTime(node.CreatedAt), formatTime(node.UpdatedAt))
			return err
		})
	return node, mapWriteError(err)
}

// GetOntologyNode returns an ontology node by ID.
func (s *Store) GetOntologyNode(ctx context.Context, id string) (*ontology.Node, error) {
	var node ontology.Node
	var createdAt, updatedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, type, name, full_path, description, metadata_json, version, created_at, updated_at
		 FROM ontology_nodes WHERE id=?`, id).Scan(
		&node.ID, &node.ProjectID, &node.Type, &node.Name, &node.FullPath, &node.Description, &node.MetadataJSON,
		&node.Version, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	node.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt.String)
	if err != nil {
		return nil, err
	}
	node.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt.String)
	if err != nil {
		return nil, err
	}
	return &node, nil
}

// ListOntologyNodesByProject returns ontology nodes for a project, optionally filtered by type.
func (s *Store) ListOntologyNodesByProject(ctx context.Context, projectID string, nodeType string, limit int) ([]ontology.Node, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var query string
	var args []interface{}
	if nodeType == "" {
		query = `SELECT id, project_id, type, name, full_path, description, metadata_json, version, created_at, updated_at
			 FROM ontology_nodes WHERE project_id=? ORDER BY created_at DESC LIMIT ?`
		args = []interface{}{projectID, limit}
	} else {
		query = `SELECT id, project_id, type, name, full_path, description, metadata_json, version, created_at, updated_at
			 FROM ontology_nodes WHERE project_id=? AND type=? ORDER BY created_at DESC LIMIT ?`
		args = []interface{}{projectID, nodeType, limit}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ontology.Node
	for rows.Next() {
		var node ontology.Node
		var createdAt, updatedAt sql.NullString
		if err = rows.Scan(
			&node.ID, &node.ProjectID, &node.Type, &node.Name, &node.FullPath, &node.Description, &node.MetadataJSON,
			&node.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		node.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt.String)
		if err != nil {
			return nil, err
		}
		node.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt.String)
		if err != nil {
			return nil, err
		}
		result = append(result, node)
	}
	return result, rows.Err()
}

// UpdateOntologyNode updates the description and metadata_json of an ontology node.
func (s *Store) UpdateOntologyNode(ctx context.Context, id string, description, metadataJSON string) error {
	err := s.execWithAudit(ctx, "ontology.node.updated", id, "engine", nil,
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`UPDATE ontology_nodes SET description=?, metadata_json=?, version=version+1, updated_at=? WHERE id=?`,
				description, metadataJSON, formatTime(time.Now().UTC()), id)
			return err
		})
	return mapWriteError(err)
}

// DeleteOntologyNode deletes an ontology node by ID.
func (s *Store) DeleteOntologyNode(ctx context.Context, id string) error {
	err := s.execWithAudit(ctx, "ontology.node.deleted", id, "engine", nil,
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM ontology_nodes WHERE id=?`, id)
			return err
		})
	return mapWriteError(err)
}

// CreateOntologyEdge inserts a new ontology edge.
func (s *Store) CreateOntologyEdge(ctx context.Context, edge ontology.Edge) (ontology.Edge, error) {
	if edge.ID == "" {
		var err error
		edge.ID, err = s.newULID(time.Now())
		if err != nil {
			return edge, err
		}
	}
	edge.CreatedAt = time.Now().UTC()
	edge.UpdatedAt = edge.CreatedAt
	if edge.Label == "" {
		edge.Label = ""
	}
	if edge.PropertiesJSON == "" {
		edge.PropertiesJSON = "{}"
	}
	if edge.Weight == 0 {
		edge.Weight = 1.0
	}
	if edge.Version < 1 {
		edge.Version = 1
	}
	if err := edge.Validate(); err != nil {
		return edge, err
	}
	err := s.execWithAudit(ctx, "ontology.edge.created", edge.ID, "engine",
		map[string]any{"type": edge.Type, "source": edge.SourceNodeID, "target": edge.TargetNodeID},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO ontology_edges(id, source_node_id, target_node_id, type, label, properties_json, weight, version, created_at, updated_at)
				 VALUES(?,?,?,?,?,?,?,?,?,?)`,
				edge.ID, edge.SourceNodeID, edge.TargetNodeID, edge.Type, edge.Label, edge.PropertiesJSON, edge.Weight, edge.Version,
				formatTime(edge.CreatedAt), formatTime(edge.UpdatedAt))
			return err
		})
	return edge, mapWriteError(err)
}

// GetOntologyEdge returns an ontology edge by ID.
func (s *Store) GetOntologyEdge(ctx context.Context, id string) (*ontology.Edge, error) {
	var edge ontology.Edge
	var createdAt, updatedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, source_node_id, target_node_id, type, label, properties_json, weight, version, created_at, updated_at
		 FROM ontology_edges WHERE id=?`, id).Scan(
		&edge.ID, &edge.SourceNodeID, &edge.TargetNodeID, &edge.Type, &edge.Label, &edge.PropertiesJSON,
		&edge.Weight, &edge.Version, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	edge.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt.String)
	if err != nil {
		return nil, err
	}
	edge.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt.String)
	if err != nil {
		return nil, err
	}
	return &edge, nil
}

// ListOntologyEdgesBySource returns ontology edges originating from a given source node.
func (s *Store) ListOntologyEdgesBySource(ctx context.Context, sourceNodeID string, limit int) ([]ontology.Edge, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_node_id, target_node_id, type, label, properties_json, weight, version, created_at, updated_at
		 FROM ontology_edges WHERE source_node_id=? ORDER BY created_at DESC LIMIT ?`, sourceNodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ontology.Edge
	for rows.Next() {
		var edge ontology.Edge
		var createdAt, updatedAt sql.NullString
		if err = rows.Scan(
			&edge.ID, &edge.SourceNodeID, &edge.TargetNodeID, &edge.Type, &edge.Label, &edge.PropertiesJSON,
			&edge.Weight, &edge.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		edge.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt.String)
		if err != nil {
			return nil, err
		}
		edge.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt.String)
		if err != nil {
			return nil, err
		}
		result = append(result, edge)
	}
	return result, rows.Err()
}

// ListOntologyEdgesByTarget returns ontology edges pointing to a given target node.
func (s *Store) ListOntologyEdgesByTarget(ctx context.Context, targetNodeID string, limit int) ([]ontology.Edge, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_node_id, target_node_id, type, label, properties_json, weight, version, created_at, updated_at
		 FROM ontology_edges WHERE target_node_id=? ORDER BY created_at DESC LIMIT ?`, targetNodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ontology.Edge
	for rows.Next() {
		var edge ontology.Edge
		var createdAt, updatedAt sql.NullString
		if err = rows.Scan(
			&edge.ID, &edge.SourceNodeID, &edge.TargetNodeID, &edge.Type, &edge.Label, &edge.PropertiesJSON,
			&edge.Weight, &edge.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		edge.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt.String)
		if err != nil {
			return nil, err
		}
		edge.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt.String)
		if err != nil {
			return nil, err
		}
		result = append(result, edge)
	}
	return result, rows.Err()
}

// UpdateOntologyEdge updates the weight of an ontology edge.
func (s *Store) UpdateOntologyEdge(ctx context.Context, id string, weight float64) error {
	err := s.execWithAudit(ctx, "ontology.edge.updated", id, "engine",
		map[string]any{"weight": weight},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`UPDATE ontology_edges SET weight=?, version=version+1, updated_at=? WHERE id=?`,
				weight, formatTime(time.Now().UTC()), id)
			return err
		})
	return mapWriteError(err)
}

// DeleteOntologyEdge deletes an ontology edge by ID.
func (s *Store) DeleteOntologyEdge(ctx context.Context, id string) error {
	err := s.execWithAudit(ctx, "ontology.edge.deleted", id, "engine", nil,
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `DELETE FROM ontology_edges WHERE id=?`, id)
			return err
		})
	return mapWriteError(err)
}