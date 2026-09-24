package toolruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenCatalogPlaysLicensedArchiveAndSkipsUnlicensed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "advancedsearch.php"):
			_, _ = w.Write([]byte(`{"response":{"docs":[{"identifier":"street-fighter","title":"Street Fighter Nosferatu vs Harmonaz"},{"identifier":"copyrighted-film","title":"Nosferatu"},{"identifier":"nosferatu","title":"Nosferatu"}]}}`))
		case strings.Contains(r.URL.Path, "street-fighter"):
			_, _ = w.Write([]byte(`{"metadata":{"title":"Street Fighter Nosferatu vs Harmonaz","licenseurl":"https://creativecommons.org/publicdomain/zero/1.0/"},"files":[{"name":"fight.mp4","size":"5000000"}]}`))
		case strings.Contains(r.URL.Path, "copyrighted-film"):
			_, _ = w.Write([]byte(`{"metadata":{"title":"Copyrighted","licenseurl":""},"files":[{"name":"film.mp4","size":"5000000"}]}`))
		case strings.Contains(r.URL.Path, "nosferatu"):
			_, _ = w.Write([]byte(`{"metadata":{"title":"Nosferatu","licenseurl":"https://creativecommons.org/publicdomain/mark/1.0/"},"files":[{"name":"thumb.jpg","size":"1000"},{"name":"nosferatu.mp4","format":"h.264","size":"5000000"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	prevSearch, prevMeta := archiveSearchEndpoint, archiveMetadataEndpoint
	archiveSearchEndpoint = server.URL + "/advancedsearch.php"
	archiveMetadataEndpoint = server.URL + "/metadata/"
	t.Cleanup(func() {
		archiveSearchEndpoint = prevSearch
		archiveMetadataEndpoint = prevMeta
	})
	rawURL, title, kind, ok := resolveArchiveCatalog(context.Background(), "Nosferatu")
	if !ok || title != "Nosferatu" || kind != "video" || rawURL != "https://archive.org/download/nosferatu/nosferatu.mp4" {
		t.Fatalf("%v %s %s %s", ok, rawURL, title, kind)
	}
}

func TestOpenCatalogPlaysCommonsAndNASA(t *testing.T) {
	commons := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query":{"pages":{"1":{"title":"File:Example.webm","imageinfo":[{"url":"https://upload.wikimedia.org/wikipedia/commons/a/a0/Example.webm","mime":"video/webm","extmetadata":{"LicenseShortName":{"value":"CC BY-SA 4.0"},"LicenseUrl":{"value":"https://creativecommons.org/licenses/by-sa/4.0"}}}]},"2":{"title":"File:Locked.webm","imageinfo":[{"url":"https://upload.wikimedia.org/wikipedia/commons/b/b0/Locked.webm","mime":"video/webm","extmetadata":{"LicenseShortName":{"value":"Copyrighted"}}}]}}}}`))
	}))
	nasa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "collection.json") {
			_, _ = w.Write([]byte(`["https://images-assets.nasa.gov/video/Apollo/Apollo~orig.mp4","https://images-assets.nasa.gov/video/Apollo/Apollo~medium.mp4"]`))
			return
		}
		_, _ = w.Write([]byte(`{"collection":{"items":[{"href":"` + nasaSearchEndpoint + `/video/Apollo/collection.json","data":[{"title":"Apollo"}]}]}}`))
	}))
	t.Cleanup(commons.Close)
	t.Cleanup(nasa.Close)
	prevCommons, prevNASA := commonsAPIEndpoint, nasaSearchEndpoint
	commonsAPIEndpoint = commons.URL
	nasaSearchEndpoint = nasa.URL
	t.Cleanup(func() {
		commonsAPIEndpoint = prevCommons
		nasaSearchEndpoint = prevNASA
	})
	rawURL, title, kind, ok := resolveCommonsCatalog(context.Background(), "Example")
	if !ok || rawURL != "https://upload.wikimedia.org/wikipedia/commons/a/a0/Example.webm" || title != "Example.webm" || kind != "video" {
		t.Fatalf("commons %v %s %s %s", ok, rawURL, title, kind)
	}
	rawURL, title, kind, ok = resolveNASACatalog(context.Background(), "Apollo")
	if !ok || rawURL != "https://images-assets.nasa.gov/video/Apollo/Apollo~medium.mp4" || title != "Apollo" || kind != "video" {
		t.Fatalf("nasa %v %s %s %s", ok, rawURL, title, kind)
	}
}
