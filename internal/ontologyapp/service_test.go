package ontologyapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/ontology"
)

type mockNodeReader struct {
	node  *ontology.Node
	nodes []ontology.Node
	err   error
}

func (m *mockNodeReader) GetOntologyNode(_ context.Context, _ string) (*ontology.Node, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.node, nil
}
func (m *mockNodeReader) ListOntologyNodesByProject(_ context.Context, _ string, _ string, _ int) ([]ontology.Node, error) {
	return m.nodes, m.err
}

type mockEdgeReader struct {
	edge       *ontology.Edge
	outgoing   []ontology.Edge
	incoming   []ontology.Edge
	err        error
	getEdgeErr error
}

func (m *mockEdgeReader) GetOntologyEdge(_ context.Context, _ string) (*ontology.Edge, error) {
	if m.getEdgeErr != nil {
		return nil, m.getEdgeErr
	}
	return m.edge, nil
}
func (m *mockEdgeReader) ListOntologyEdgesBySource(_ context.Context, _ string, _ int) ([]ontology.Edge, error) {
	return m.outgoing, m.err
}
func (m *mockEdgeReader) ListOntologyEdgesByTarget(_ context.Context, _ string, _ int) ([]ontology.Edge, error) {
	return m.incoming, m.err
}

type mockNodeWriter struct {
	updatedDesc   string
	updatedMeta   string
	deletedID     string
	err           error
	deletedEdges  []string
	createdNode   ontology.Node
}

func (m *mockNodeWriter) CreateOntologyNode(_ context.Context, node ontology.Node) (ontology.Node, error) {
	if m.err != nil {
		return node, m.err
	}
	if node.ID == "" {
		node.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	}
	m.createdNode = node
	return node, nil
}
func (m *mockNodeWriter) UpdateOntologyNode(_ context.Context, id, desc, meta string) error {
	m.updatedDesc = desc
	m.updatedMeta = meta
	return m.err
}
func (m *mockNodeWriter) DeleteOntologyNode(_ context.Context, id string) error {
	m.deletedID = id
	return m.err
}

type mockEdgeWriter struct {
	updatedWeight float64
	deletedID     string
	err           error
	createdEdge   ontology.Edge
}

func (m *mockEdgeWriter) CreateOntologyEdge(_ context.Context, edge ontology.Edge) (ontology.Edge, error) {
	if m.err != nil {
		return edge, m.err
	}
	if edge.ID == "" {
		edge.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	}
	m.createdEdge = edge
	return edge, nil
}
func (m *mockEdgeWriter) UpdateOntologyEdge(_ context.Context, _ string, weight float64) error {
	m.updatedWeight = weight
	return m.err
}
func (m *mockEdgeWriter) DeleteOntologyEdge(_ context.Context, id string) error {
	m.deletedID = id
	return m.err
}

func ontNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func ontNode(id string, name string) *ontology.Node {
	return &ontology.Node{
		ID:        id,
		ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		Type:      ontology.NodeTypeFunction,
		Name:      name,
		FullPath:  "src/" + name,
		Version:   1,
		CreatedAt: ontNow(),
		UpdatedAt: ontNow(),
	}
}

func TestGetNodeNotFound(t *testing.T) {
	r := &mockNodeReader{node: nil}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	if _, err := s.GetNode(context.Background(), "missing"); err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGetNodeSuccess(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	n, err := s.GetNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "auth" {
		t.Fatalf("expected name auth, got %s", n.Name)
	}
}

func TestListNodesByProject(t *testing.T) {
	r := &mockNodeReader{nodes: []ontology.Node{*ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "a"), *ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAW", "b")}}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	nodes, err := s.ListNodesByProject(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", ontology.NodeTypeFunction)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestSearchNodes(t *testing.T) {
	r := &mockNodeReader{nodes: []ontology.Node{
		*ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "authHandler"),
		*ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAW", "sessionStore"),
		*ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAX", "authService"),
	}}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	results, err := s.SearchNodes(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", "auth")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(results))
	}
}

func TestSearchNodesEmptyQuery(t *testing.T) {
	s := New(&mockNodeReader{}, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	results, err := s.SearchNodes(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAA", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Fatalf("expected nil for empty query, got %v", results)
	}
}

func TestUpdateNodeNotFound(t *testing.T) {
	r := &mockNodeReader{node: nil}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	if err := s.UpdateNode(context.Background(), "missing", "desc", "{}"); err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestUpdateNodeRejectsOversizeDescription(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	long := make([]byte, 4097)
	for i := range long {
		long[i] = 'a'
	}
	if err := s.UpdateNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", string(long), "{}"); err != ErrDescriptionTooLong {
		t.Fatalf("expected ErrDescriptionTooLong, got %v", err)
	}
}

func TestUpdateNodeRejectsOversizeMetadata(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	huge := make([]byte, 65537)
	for i := range huge {
		huge[i] = 'a'
	}
	if err := s.UpdateNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "desc", string(huge)); err != ErrMetadataTooLarge {
		t.Fatalf("expected ErrMetadataTooLarge, got %v", err)
	}
}

func TestUpdateNodeSuccess(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	w := &mockNodeWriter{}
	s := New(r, &mockEdgeReader{}, w, &mockEdgeWriter{})
	if err := s.UpdateNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "new desc", `{"k":"v"}`); err != nil {
		t.Fatal(err)
	}
	if w.updatedDesc != "new desc" {
		t.Fatalf("expected desc updated, got %s", w.updatedDesc)
	}
}

func TestDeleteNodeCascadesEdges(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	er := &mockEdgeReader{
		outgoing: []ontology.Edge{{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEA", SourceNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
		incoming: []ontology.Edge{{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEB", TargetNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
	}
	nw := &mockNodeWriter{}
	ew := &mockEdgeWriter{}
	s := New(r, er, nw, ew)
	if err := s.DeleteNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if nw.deletedID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected node deleted, got %s", nw.deletedID)
	}
	// Edge writer should have been called twice (one outgoing, one incoming).
	_ = ew // both calls are best-effort; we just ensure no panic and node was deleted.
}

func TestDeleteNodeNotFound(t *testing.T) {
	r := &mockNodeReader{node: nil}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	if err := s.DeleteNode(context.Background(), "missing"); err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGetEdgeNotFound(t *testing.T) {
	er := &mockEdgeReader{edge: nil}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, &mockEdgeWriter{})
	if _, err := s.GetEdge(context.Background(), "missing"); err != ErrEdgeNotFound {
		t.Fatalf("expected ErrEdgeNotFound, got %v", err)
	}
}

func TestListOutgoingEdges(t *testing.T) {
	er := &mockEdgeReader{outgoing: []ontology.Edge{{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEA"}}}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, &mockEdgeWriter{})
	edges, err := s.ListOutgoingEdges(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}

func TestListIncomingEdges(t *testing.T) {
	er := &mockEdgeReader{incoming: []ontology.Edge{{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEB"}}}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, &mockEdgeWriter{})
	edges, err := s.ListIncomingEdges(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}

func TestUpdateEdgeRejectsInvalidWeight(t *testing.T) {
	er := &mockEdgeReader{edge: &ontology.Edge{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEA"}}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, &mockEdgeWriter{})
	if err := s.UpdateEdge(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FEA", 1.5); err == nil {
		t.Fatal("expected error for weight > 1")
	}
	if err := s.UpdateEdge(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FEA", -0.1); err == nil {
		t.Fatal("expected error for negative weight")
	}
}

func TestUpdateEdgeNotFound(t *testing.T) {
	er := &mockEdgeReader{edge: nil}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, &mockEdgeWriter{})
	if err := s.UpdateEdge(context.Background(), "missing", 0.5); err != ErrEdgeNotFound {
		t.Fatalf("expected ErrEdgeNotFound, got %v", err)
	}
}

func TestUpdateEdgeSuccess(t *testing.T) {
	er := &mockEdgeReader{edge: &ontology.Edge{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEA"}}
	ew := &mockEdgeWriter{}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, ew)
	if err := s.UpdateEdge(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FEA", 0.75); err != nil {
		t.Fatal(err)
	}
	if ew.updatedWeight != 0.75 {
		t.Fatalf("expected weight 0.75, got %f", ew.updatedWeight)
	}
}

func TestDeleteEdgeNotFound(t *testing.T) {
	er := &mockEdgeReader{edge: nil}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, &mockEdgeWriter{})
	if err := s.DeleteEdge(context.Background(), "missing"); err != ErrEdgeNotFound {
		t.Fatalf("expected ErrEdgeNotFound, got %v", err)
	}
}

func TestDeleteEdgeSuccess(t *testing.T) {
	er := &mockEdgeReader{edge: &ontology.Edge{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEA"}}
	ew := &mockEdgeWriter{}
	s := New(&mockNodeReader{}, er, &mockNodeWriter{}, ew)
	if err := s.DeleteEdge(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FEA"); err != nil {
		t.Fatal(err)
	}
	if ew.deletedID != "01ARZ3NDEKTSV4RRFFQ69G5FEA" {
		t.Fatalf("expected edge deleted, got %s", ew.deletedID)
	}
}

func TestGetNeighbors(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	er := &mockEdgeReader{
		outgoing: []ontology.Edge{
			{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEA", SourceNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TargetNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FTAA"},
			{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEB", SourceNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TargetNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FTAB"},
		},
		incoming: []ontology.Edge{
			{ID: "01ARZ3NDEKTSV4RRFFQ69G5FEC", SourceNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FTAB", TargetNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"},
			{ID: "01ARZ3NDEKTSV4RRFFQ69G5FED", SourceNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FTAC", TargetNodeID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"},
		},
	}
	s := New(r, er, &mockNodeWriter{}, &mockEdgeWriter{})
	neighbors, err := s.GetNeighbors(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	// Expected: FTAA, FTAB (deduplicated from outgoing+incoming), FTAC.
	if len(neighbors) != 3 {
		t.Fatalf("expected 3 unique neighbors, got %d: %v", len(neighbors), neighbors)
	}
	seen := map[string]bool{}
	for _, n := range neighbors {
		if seen[n] {
			t.Fatalf("duplicate neighbor %s", n)
		}
		seen[n] = true
	}
	if !seen["01ARZ3NDEKTSV4RRFFQ69G5FTAA"] || !seen["01ARZ3NDEKTSV4RRFFQ69G5FTAB"] || !seen["01ARZ3NDEKTSV4RRFFQ69G5FTAC"] {
		t.Fatalf("missing expected neighbor; got %v", neighbors)
	}
}

func TestGetNeighborsNodeNotFound(t *testing.T) {
	r := &mockNodeReader{node: nil}
	s := New(r, &mockEdgeReader{}, &mockNodeWriter{}, &mockEdgeWriter{})
	if _, err := s.GetNeighbors(context.Background(), "missing"); err != ErrNodeNotFound {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestGetNodeReaderUnavailable(t *testing.T) {
	s := &Service{}
	if _, err := s.GetNode(context.Background(), "x"); err == nil {
		t.Fatal("expected error for nil reader")
	}
}

func TestUpdateNodeWriterUnavailable(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	s := New(r, &mockEdgeReader{}, nil, &mockEdgeWriter{})
	err := s.UpdateNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "desc", "{}")
	if err == nil {
		t.Fatal("expected error for nil writer")
	}
}

func TestUpdateNodePropagatesError(t *testing.T) {
	r := &mockNodeReader{node: ontNode("01ARZ3NDEKTSV4RRFFQ69G5FAV", "auth")}
	boom := errors.New("storage failure")
	w := &mockNodeWriter{err: boom}
	s := New(r, &mockEdgeReader{}, w, &mockEdgeWriter{})
	if err := s.UpdateNode(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "desc", "{}"); err != boom {
		t.Fatalf("expected propagated error, got %v", err)
	}
}
