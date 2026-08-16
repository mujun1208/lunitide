package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListToolsParsesCatalogue(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"searchDocs","description":"Search project docs","inputSchema":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}},
			{"name":"getMeta","description":"","inputSchema":null}
		]`))
	}))
	defer srv.Close()
	c := tlsTestClient(t, srv)
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools = %d", len(tools))
	}
	if tools[0].Name != "searchDocs" || tools[0].Description != "Search project docs" {
		t.Fatalf("tool[0] = %+v", tools[0])
	}
	if string(tools[0].InputSchema) == "" || tools[0].InputSchema[0] != '{' {
		t.Fatalf("input schema missing: %s", tools[0].InputSchema)
	}
}

func TestListToolsRejectsNonArrayBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()
	c := tlsTestClient(t, srv)
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("object body accepted as catalogue")
	}
}

func TestListToolsSurfacesHttpStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := tlsTestClient(t, srv)
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("404 catalogue accepted")
	}
}
