package ontology

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// NodeType categorizes ontology nodes.
type NodeType string

const (
	NodeTypeClass      NodeType = "class"
	NodeTypeInterface  NodeType = "interface"
	NodeTypeFunction   NodeType = "function"
	NodeTypeModule     NodeType = "module"
	NodeTypeTable      NodeType = "table"
	NodeTypeFile       NodeType = "file"
	NodeTypeRequirement NodeType = "requirement"
	NodeTypeArtifact   NodeType = "artifact"
	NodeTypeComponent  NodeType = "component"
	NodeTypeEndpoint   NodeType = "endpoint"
	NodeTypeTest       NodeType = "test"
)

// EdgeType categorizes ontology relationships.
type EdgeType string

const (
	EdgeTypeImplements    EdgeType = "implements"
	EdgeTypeExtends       EdgeType = "extends"
	EdgeTypeDependsOn     EdgeType = "depends_on"
	EdgeTypeReferences    EdgeType = "references"
	EdgeTypeContains      EdgeType = "contains"
	EdgeTypeTests         EdgeType = "tests"
	EdgeTypeImports       EdgeType = "imports"
	EdgeTypeSatisfies     EdgeType = "satisfies"
	EdgeTypeTraces        EdgeType = "traces"
	EdgeTypeGenerates     EdgeType = "generates"
	EdgeTypeConfigures    EdgeType = "configures"
	EdgeTypeAuthenticates EdgeType = "authenticates"
	EdgeTypeAuthorizes    EdgeType = "authorizes"
)

// Node is a semantic entity in the project ontology graph.
// Nodes represent classes, interfaces, functions, modules, tables,
// files, requirements, artifacts, and other project entities.
type Node struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Type        NodeType  `json:"type"`
	Name        string    `json:"name"`
	FullPath    string    `json:"fullPath"`
	Description string    `json:"description"`
	MetadataJSON string   `json:"metadataJson"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Validate checks invariants for an ontology node.
func (n Node) Validate() error {
	if !canonicalULID(n.ID) || !canonicalULID(n.ProjectID) {
		return errors.New("ontology node id or project_id is not a canonical ULID")
	}
	switch n.Type {
	case NodeTypeClass, NodeTypeInterface, NodeTypeFunction, NodeTypeModule,
		NodeTypeTable, NodeTypeFile, NodeTypeRequirement, NodeTypeArtifact,
		NodeTypeComponent, NodeTypeEndpoint, NodeTypeTest:
	default:
		return errors.New("ontology node type invalid")
	}
	if len(n.Name) < 1 || len(n.Name) > 256 {
		return errors.New("ontology node name must be 1-256 characters")
	}
	if len(n.FullPath) > 1024 {
		return errors.New("ontology node full_path too long")
	}
	if len(n.Description) > 4096 {
		return errors.New("ontology node description too long")
	}
	if len(n.MetadataJSON) > 65536 {
		return errors.New("ontology node metadata_json too large")
	}
	if n.Version < 1 {
		return errors.New("ontology node version must be positive")
	}
	if n.CreatedAt.IsZero() || n.CreatedAt.Location() != time.UTC {
		return errors.New("ontology node created_at must be UTC")
	}
	if n.UpdatedAt.IsZero() || n.UpdatedAt.Location() != time.UTC {
		return errors.New("ontology node updated_at must be UTC")
	}
	if n.UpdatedAt.Before(n.CreatedAt) {
		return errors.New("ontology node updated_at must be >= created_at")
	}
	return nil
}

// Edge is a directed relationship between two ontology nodes.
// Edges are versioned and carry provenance metadata.
type Edge struct {
	ID           string    `json:"id"`
	SourceNodeID string    `json:"sourceNodeId"`
	TargetNodeID string    `json:"targetNodeId"`
	Type         EdgeType  `json:"type"`
	Label        string    `json:"label"`
	PropertiesJSON string  `json:"propertiesJson"`
	Weight       float64   `json:"weight"`
	Version      int64     `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Validate checks invariants for an ontology edge.
func (e Edge) Validate() error {
	if !canonicalULID(e.ID) || !canonicalULID(e.SourceNodeID) || !canonicalULID(e.TargetNodeID) {
		return errors.New("ontology edge id, source_node_id or target_node_id is not a canonical ULID")
	}
	if e.SourceNodeID == e.TargetNodeID {
		return errors.New("ontology edge cannot be self-referencing")
	}
	switch e.Type {
	case EdgeTypeImplements, EdgeTypeExtends, EdgeTypeDependsOn, EdgeTypeReferences,
		EdgeTypeContains, EdgeTypeTests, EdgeTypeImports, EdgeTypeSatisfies,
		EdgeTypeTraces, EdgeTypeGenerates, EdgeTypeConfigures,
		EdgeTypeAuthenticates, EdgeTypeAuthorizes:
	default:
		return errors.New("ontology edge type invalid")
	}
	if len(e.Label) > 256 {
		return errors.New("ontology edge label too long")
	}
	if len(e.PropertiesJSON) > 65536 {
		return errors.New("ontology edge properties_json too large")
	}
	if e.Weight < 0 || e.Weight > 1 {
		return errors.New("ontology edge weight must be between 0.0 and 1.0")
	}
	if e.Version < 1 {
		return errors.New("ontology edge version must be positive")
	}
	if e.CreatedAt.IsZero() || e.CreatedAt.Location() != time.UTC {
		return errors.New("ontology edge created_at must be UTC")
	}
	if e.UpdatedAt.IsZero() || e.UpdatedAt.Location() != time.UTC {
		return errors.New("ontology edge updated_at must be UTC")
	}
	if e.UpdatedAt.Before(e.CreatedAt) {
		return errors.New("ontology edge updated_at must be >= created_at")
	}
	return nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}