package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/ontology"
	"github.com/lunitide/lunitide/internal/ontologyapp"
)

type OntologyService interface {
	GetNode(context.Context, string) (*ontology.Node, error)
	ListNodesByProject(context.Context, string, ontology.NodeType) ([]ontology.Node, error)
	SearchNodes(context.Context, string, string) ([]ontology.Node, error)
	ListOutgoingEdges(context.Context, string) ([]ontology.Edge, error)
	ListIncomingEdges(context.Context, string) ([]ontology.Edge, error)
}

type ontologyNodeDTO struct {
	ID           string            `json:"id"`
	ProjectID    string            `json:"projectId"`
	Type         ontology.NodeType `json:"type"`
	Name         string            `json:"name"`
	FullPath     string            `json:"fullPath"`
	Description  string            `json:"description"`
	MetadataJSON string            `json:"metadataJson"`
	Version      int64             `json:"version"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type ontologyEdgeDTO struct {
	ID             string            `json:"id"`
	SourceNodeID   string            `json:"sourceNodeId"`
	TargetNodeID   string            `json:"targetNodeId"`
	Type           ontology.EdgeType `json:"type"`
	Label          string            `json:"label"`
	PropertiesJSON string            `json:"propertiesJson"`
	Weight         float64           `json:"weight"`
	Version        int64             `json:"version"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

func newOntologyNodeDTO(n ontology.Node) ontologyNodeDTO {
	return ontologyNodeDTO{
		ID:           n.ID,
		ProjectID:    n.ProjectID,
		Type:         n.Type,
		Name:         n.Name,
		FullPath:     n.FullPath,
		Description:  n.Description,
		MetadataJSON: n.MetadataJSON,
		Version:      n.Version,
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
	}
}

func newOntologyEdgeDTO(e ontology.Edge) ontologyEdgeDTO {
	return ontologyEdgeDTO{
		ID:             e.ID,
		SourceNodeID:   e.SourceNodeID,
		TargetNodeID:   e.TargetNodeID,
		Type:           e.Type,
		Label:          e.Label,
		PropertiesJSON: e.PropertiesJSON,
		Weight:         e.Weight,
		Version:        e.Version,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
	}
}

func ontologyServiceAvailable(service OntologyService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Ptr || !v.IsNil()
}

func handleOntologyNodeGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "ontology.node.get 参数无效", false)
	}
	if !ontologyServiceAvailable(e.ontology) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "本体数据暂时不可用", true)
	}
	node, err := e.ontology.GetNode(ctx, p.ID)
	if err != nil {
		return ontologyFailure(r, err)
	}
	return bridge.Success(r.ID, newOntologyNodeDTO(*node))
}

func handleOntologyNodeList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string            `json:"projectId"`
		Type      ontology.NodeType `json:"type"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "ontology.node.list 参数无效", false)
	}
	if !ontologyServiceAvailable(e.ontology) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "本体数据暂时不可用", true)
	}
	items, err := e.ontology.ListNodesByProject(ctx, p.ProjectID, p.Type)
	if err != nil {
		return ontologyFailure(r, err)
	}
	dtos := make([]ontologyNodeDTO, len(items))
	for i := range items {
		dtos[i] = newOntologyNodeDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []ontologyNodeDTO `json:"items"`
	}{Items: dtos})
}

func handleOntologyNodeSearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Query     string `json:"query"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.Query) == "" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "ontology.node.search 参数无效", false)
	}
	if !ontologyServiceAvailable(e.ontology) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "本体数据暂时不可用", true)
	}
	items, err := e.ontology.SearchNodes(ctx, p.ProjectID, p.Query)
	if err != nil {
		return ontologyFailure(r, err)
	}
	dtos := make([]ontologyNodeDTO, len(items))
	for i := range items {
		dtos[i] = newOntologyNodeDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []ontologyNodeDTO `json:"items"`
	}{Items: dtos})
}

func handleOntologyEdgeList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		NodeID    string `json:"nodeId"`
		Direction string `json:"direction"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.NodeID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "ontology.edge.list 参数无效", false)
	}
	if p.Direction != "" && p.Direction != "outgoing" && p.Direction != "incoming" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "ontology.edge.list 参数无效", false)
	}
	if !ontologyServiceAvailable(e.ontology) {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "本体数据暂时不可用", true)
	}
	direction := p.Direction
	if direction == "" {
		direction = "outgoing"
	}
	var items []ontology.Edge
	var err error
	if direction == "incoming" {
		items, err = e.ontology.ListIncomingEdges(ctx, p.NodeID)
	} else {
		items, err = e.ontology.ListOutgoingEdges(ctx, p.NodeID)
	}
	if err != nil {
		return ontologyFailure(r, err)
	}
	dtos := make([]ontologyEdgeDTO, len(items))
	for i := range items {
		dtos[i] = newOntologyEdgeDTO(items[i])
	}
	return bridge.Success(r.ID, struct {
		Items []ontologyEdgeDTO `json:"items"`
	}{Items: dtos})
}

func ontologyFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, ontologyapp.ErrNodeNotFound):
		return bridge.Failure(r.ID, r.TraceID, "ONTOLOGY_NODE_NOT_FOUND", "本体节点不存在", false)
	case errors.Is(err, ontologyapp.ErrEdgeNotFound):
		return bridge.Failure(r.ID, r.TraceID, "ONTOLOGY_EDGE_NOT_FOUND", "本体边不存在", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "本体数据暂时不可用", true)
	}
}
