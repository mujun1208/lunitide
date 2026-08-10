// Package ontologyapp coordinates ontology node/edge lifecycle and graph traversal.
package ontologyapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/ontology"
	"github.com/oklog/ulid/v2"
)

var (
	ErrNodeNotFound       = errors.New("ontology node not found")
	ErrEdgeNotFound       = errors.New("ontology edge not found")
	ErrNodeExists         = errors.New("ontology node already exists")
	ErrEdgeExists         = errors.New("ontology edge already exists")
	ErrSelfReference      = errors.New("ontology edge cannot be self-referencing")
	ErrInvalidNodeType    = errors.New("invalid ontology node type")
	ErrInvalidEdgeType    = errors.New("invalid ontology edge type")
	ErrMetadataTooLarge   = errors.New("metadata json too large")
	ErrDescriptionTooLong = errors.New("description too long")
)

// NodeReader reads ontology nodes from storage.
type NodeReader interface {
	GetOntologyNode(ctx context.Context, id string) (*ontology.Node, error)
	ListOntologyNodesByProject(ctx context.Context, projectID string, nodeType string, limit int) ([]ontology.Node, error)
}

// EdgeReader reads ontology edges from storage.
type EdgeReader interface {
	GetOntologyEdge(ctx context.Context, id string) (*ontology.Edge, error)
	ListOntologyEdgesBySource(ctx context.Context, sourceNodeID string, limit int) ([]ontology.Edge, error)
	ListOntologyEdgesByTarget(ctx context.Context, targetNodeID string, limit int) ([]ontology.Edge, error)
}

// NodeWriter writes ontology node updates.
type NodeWriter interface {
	CreateOntologyNode(ctx context.Context, node ontology.Node) (ontology.Node, error)
	UpdateOntologyNode(ctx context.Context, id string, description, metadataJSON string) error
	DeleteOntologyNode(ctx context.Context, id string) error
}

// EdgeWriter writes ontology edge updates.
type EdgeWriter interface {
	CreateOntologyEdge(ctx context.Context, edge ontology.Edge) (ontology.Edge, error)
	UpdateOntologyEdge(ctx context.Context, id string, weight float64) error
	DeleteOntologyEdge(ctx context.Context, id string) error
}

// Clock provides the current time.
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Service coordinates ontology graph lifecycle and traversal.
type Service struct {
	nodes     NodeReader
	edges     EdgeReader
	nodeWrite NodeWriter
	edgeWrite EdgeWriter
	clock     Clock
}

// New creates an ontology service with the given dependencies.
func New(nr NodeReader, er EdgeReader, nw NodeWriter, ew EdgeWriter) *Service {
	return &Service{
		nodes:     nr,
		edges:     er,
		nodeWrite: nw,
		edgeWrite: ew,
		clock:     systemClock{},
	}
}

// GetNode returns an ontology node by ID.
func (s *Service) GetNode(ctx context.Context, id string) (*ontology.Node, error) {
	if s == nil || s.nodes == nil {
		return nil, errors.New("ontology node reader unavailable")
	}
	n, err := s.nodes.GetOntologyNode(ctx, id)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, ErrNodeNotFound
	}
	return n, nil
}

// CreateNode validates and persists a new ontology node.
func (s *Service) CreateNode(ctx context.Context, node ontology.Node) (ontology.Node, error) {
	if s == nil || s.nodeWrite == nil {
		return ontology.Node{}, errors.New("ontology node writer unavailable")
	}
	if !canonicalULID(node.ProjectID) {
		return ontology.Node{}, errors.New("ontology node project_id is not a canonical ULID")
	}
	switch node.Type {
	case ontology.NodeTypeClass, ontology.NodeTypeInterface, ontology.NodeTypeFunction, ontology.NodeTypeModule,
		ontology.NodeTypeTable, ontology.NodeTypeFile, ontology.NodeTypeRequirement, ontology.NodeTypeArtifact,
		ontology.NodeTypeComponent, ontology.NodeTypeEndpoint, ontology.NodeTypeTest:
	default:
		return ontology.Node{}, ErrInvalidNodeType
	}
	if len(node.Name) < 1 || len(node.Name) > 256 {
		return ontology.Node{}, errors.New("ontology node name must be 1-256 characters")
	}
	if len(node.FullPath) > 1024 {
		return ontology.Node{}, errors.New("ontology node full_path too long")
	}
	if len(node.Description) > 4096 {
		return ontology.Node{}, ErrDescriptionTooLong
	}
	if len(node.MetadataJSON) > 65536 {
		return ontology.Node{}, ErrMetadataTooLarge
	}
	if node.MetadataJSON == "" {
		node.MetadataJSON = "{}"
	}
	now := s.clock.Now()
	node.ID = ""
	node.Version = 1
	node.CreatedAt = now
	node.UpdatedAt = now
	return s.nodeWrite.CreateOntologyNode(ctx, node)
}

// ListNodesByProject returns nodes for a project, optionally filtered by type.
func (s *Service) ListNodesByProject(ctx context.Context, projectID string, nodeType ontology.NodeType) ([]ontology.Node, error) {
	if s == nil || s.nodes == nil {
		return nil, errors.New("ontology node reader unavailable")
	}
	typeStr := ""
	if nodeType != "" {
		typeStr = string(nodeType)
	}
	return s.nodes.ListOntologyNodesByProject(ctx, projectID, typeStr, 100)
}

// SearchNodes performs a case-insensitive keyword search across node name, full path, and description.
// Returns nodes matching any of the keywords.
func (s *Service) SearchNodes(ctx context.Context, projectID string, query string) ([]ontology.Node, error) {
	if s == nil || s.nodes == nil {
		return nil, errors.New("ontology node reader unavailable")
	}
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, nil
	}
	all, err := s.nodes.ListOntologyNodesByProject(ctx, projectID, "", 100)
	if err != nil {
		return nil, err
	}
	keywords := strings.Fields(query)
	var results []ontology.Node
	for _, n := range all {
		nameLower := strings.ToLower(n.Name)
		pathLower := strings.ToLower(n.FullPath)
		descLower := strings.ToLower(n.Description)
		matched := false
		for _, kw := range keywords {
			if strings.Contains(nameLower, kw) ||
				strings.Contains(pathLower, kw) ||
				strings.Contains(descLower, kw) {
				matched = true
				break
			}
		}
		if matched {
			results = append(results, n)
		}
	}
	return results, nil
}

// UpdateNode updates the description and metadata of a node.
func (s *Service) UpdateNode(ctx context.Context, id, description, metadataJSON string) error {
	if s == nil || s.nodeWrite == nil {
		return errors.New("ontology node writer unavailable")
	}
	if len(description) > 4096 {
		return ErrDescriptionTooLong
	}
	if len(metadataJSON) > 65536 {
		return ErrMetadataTooLarge
	}
	node, err := s.nodes.GetOntologyNode(ctx, id)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}
	return s.nodeWrite.UpdateOntologyNode(ctx, id, description, metadataJSON)
}

// DeleteNode deletes a node by ID.
func (s *Service) DeleteNode(ctx context.Context, id string) error {
	if s == nil || s.nodeWrite == nil {
		return errors.New("ontology node writer unavailable")
	}
	node, err := s.nodes.GetOntologyNode(ctx, id)
	if err != nil {
		return err
	}
	if node == nil {
		return ErrNodeNotFound
	}
	// Best-effort cleanup of edges referencing this node.
	outgoing, _ := s.edges.ListOntologyEdgesBySource(ctx, id, 100)
	for _, e := range outgoing {
		_ = s.edgeWrite.DeleteOntologyEdge(ctx, e.ID)
	}
	incoming, _ := s.edges.ListOntologyEdgesByTarget(ctx, id, 100)
	for _, e := range incoming {
		_ = s.edgeWrite.DeleteOntologyEdge(ctx, e.ID)
	}
	return s.nodeWrite.DeleteOntologyNode(ctx, id)
}

// GetEdge returns an ontology edge by ID.
func (s *Service) GetEdge(ctx context.Context, id string) (*ontology.Edge, error) {
	if s == nil || s.edges == nil {
		return nil, errors.New("ontology edge reader unavailable")
	}
	e, err := s.edges.GetOntologyEdge(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, ErrEdgeNotFound
	}
	return e, nil
}

// CreateEdge validates and persists a new ontology edge.
func (s *Service) CreateEdge(ctx context.Context, edge ontology.Edge) (ontology.Edge, error) {
	if s == nil || s.edgeWrite == nil {
		return ontology.Edge{}, errors.New("ontology edge writer unavailable")
	}
	if !canonicalULID(edge.SourceNodeID) || !canonicalULID(edge.TargetNodeID) {
		return ontology.Edge{}, errors.New("ontology edge source_node_id or target_node_id is not a canonical ULID")
	}
	if edge.SourceNodeID == edge.TargetNodeID {
		return ontology.Edge{}, ErrSelfReference
	}
	switch edge.Type {
	case ontology.EdgeTypeImplements, ontology.EdgeTypeExtends, ontology.EdgeTypeDependsOn, ontology.EdgeTypeReferences,
		ontology.EdgeTypeContains, ontology.EdgeTypeTests, ontology.EdgeTypeImports, ontology.EdgeTypeSatisfies,
		ontology.EdgeTypeTraces, ontology.EdgeTypeGenerates, ontology.EdgeTypeConfigures,
		ontology.EdgeTypeAuthenticates, ontology.EdgeTypeAuthorizes:
	default:
		return ontology.Edge{}, ErrInvalidEdgeType
	}
	if len(edge.Label) > 256 {
		return ontology.Edge{}, errors.New("ontology edge label too long")
	}
	if len(edge.PropertiesJSON) > 65536 {
		return ontology.Edge{}, errors.New("ontology edge properties_json too large")
	}
	if edge.Weight < 0 || edge.Weight > 1 {
		return ontology.Edge{}, errors.New("ontology edge weight must be between 0.0 and 1.0")
	}
	if edge.PropertiesJSON == "" {
		edge.PropertiesJSON = "{}"
	}
	if edge.Weight == 0 {
		edge.Weight = 1.0
	}
	now := s.clock.Now()
	edge.ID = ""
	edge.Version = 1
	edge.CreatedAt = now
	edge.UpdatedAt = now
	return s.edgeWrite.CreateOntologyEdge(ctx, edge)
}

// ListOutgoingEdges returns edges originating from a node.
func (s *Service) ListOutgoingEdges(ctx context.Context, sourceNodeID string) ([]ontology.Edge, error) {
	if s == nil || s.edges == nil {
		return nil, errors.New("ontology edge reader unavailable")
	}
	return s.edges.ListOntologyEdgesBySource(ctx, sourceNodeID, 100)
}

// ListIncomingEdges returns edges pointing to a node.
func (s *Service) ListIncomingEdges(ctx context.Context, targetNodeID string) ([]ontology.Edge, error) {
	if s == nil || s.edges == nil {
		return nil, errors.New("ontology edge reader unavailable")
	}
	return s.edges.ListOntologyEdgesByTarget(ctx, targetNodeID, 100)
}

// UpdateEdge updates the weight of an edge.
func (s *Service) UpdateEdge(ctx context.Context, id string, weight float64) error {
	if s == nil || s.edgeWrite == nil {
		return errors.New("ontology edge writer unavailable")
	}
	if weight < 0 || weight > 1 {
		return errors.New("ontology edge weight must be between 0.0 and 1.0")
	}
	edge, err := s.edges.GetOntologyEdge(ctx, id)
	if err != nil {
		return err
	}
	if edge == nil {
		return ErrEdgeNotFound
	}
	return s.edgeWrite.UpdateOntologyEdge(ctx, id, weight)
}

// DeleteEdge deletes an edge by ID.
func (s *Service) DeleteEdge(ctx context.Context, id string) error {
	if s == nil || s.edgeWrite == nil {
		return errors.New("ontology edge writer unavailable")
	}
	edge, err := s.edges.GetOntologyEdge(ctx, id)
	if err != nil {
		return err
	}
	if edge == nil {
		return ErrEdgeNotFound
	}
	return s.edgeWrite.DeleteOntologyEdge(ctx, id)
}

// GetNeighbors returns the immediate neighbors of a node (both outgoing and incoming).
// For outgoing edges, the target nodes are returned; for incoming edges, the source nodes.
// The returned node IDs are deduplicated.
func (s *Service) GetNeighbors(ctx context.Context, nodeID string) ([]string, error) {
	if s == nil || s.nodes == nil || s.edges == nil {
		return nil, errors.New("ontology reader unavailable")
	}
	node, err := s.nodes.GetOntologyNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, ErrNodeNotFound
	}
	seen := make(map[string]struct{})
	var neighbors []string
	outgoing, err := s.edges.ListOntologyEdgesBySource(ctx, nodeID, 100)
	if err != nil {
		return nil, err
	}
	for _, e := range outgoing {
		if e.TargetNodeID == nodeID {
			continue // skip self-references defensively
		}
		if _, ok := seen[e.TargetNodeID]; !ok {
			seen[e.TargetNodeID] = struct{}{}
			neighbors = append(neighbors, e.TargetNodeID)
		}
	}
	incoming, err := s.edges.ListOntologyEdgesByTarget(ctx, nodeID, 100)
	if err != nil {
		return nil, err
	}
	for _, e := range incoming {
		if e.SourceNodeID == nodeID {
			continue
		}
		if _, ok := seen[e.SourceNodeID]; !ok {
			seen[e.SourceNodeID] = struct{}{}
			neighbors = append(neighbors, e.SourceNodeID)
		}
	}
	return neighbors, nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}
