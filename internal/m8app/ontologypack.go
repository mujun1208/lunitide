package m8app

import (
	"encoding/json"
	"fmt"
	"strings"

	"embed"
)

//go:embed ontologypacks/*.json
var ontologyPackFiles embed.FS

// OntologyPack is a declarative object/link/action schema for one domain.
type OntologyPack struct {
	ID      string `json:"id"`
	Objects []struct {
		Kind string `json:"kind"`
	} `json:"objects"`
	Links []struct {
		Name string `json:"name"`
	} `json:"links"`
	Actions []struct {
		Name string `json:"name"`
		Mode string `json:"mode"`
	} `json:"actions"`
}

// LoadOntologyPack answers a shipped pack by id.
func LoadOntologyPack(id string) (OntologyPack, error) {
	id = strings.TrimSpace(id)
	raw, err := ontologyPackFiles.ReadFile("ontologypacks/" + id + ".json")
	if err != nil {
		return OntologyPack{}, fmt.Errorf("unknown ontology pack %q", id)
	}
	var pack OntologyPack
	if err := json.Unmarshal(raw, &pack); err != nil {
		return OntologyPack{}, err
	}
	if pack.ID != id {
		return OntologyPack{}, fmt.Errorf("pack id mismatch")
	}
	return pack, nil
}

func (p OntologyPack) HasKind(kind string) bool {
	for _, o := range p.Objects {
		if o.Kind == kind {
			return true
		}
	}
	return false
}

func (p OntologyPack) HasLink(name string) bool {
	for _, l := range p.Links {
		if l.Name == name {
			return true
		}
	}
	return false
}

func (p OntologyPack) HasAction(name string) bool {
	for _, a := range p.Actions {
		if a.Name == name {
			return true
		}
	}
	return false
}

// ValidateNodePayload requires pack == this pack id and kind in objects.
func (p OntologyPack) ValidateNodePayload(raw string) error {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return err
	}
	pack, _ := m["pack"].(string)
	kind, _ := m["kind"].(string)
	if pack != p.ID {
		return fmt.Errorf("pack %q != %q", pack, p.ID)
	}
	if !p.HasKind(kind) {
		return fmt.Errorf("unknown kind %q", kind)
	}
	return nil
}
