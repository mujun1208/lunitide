package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// resolveOpenMedia looks up a direct file in the public-domain catalogs.
// Tests replace it. The live lookup only returns files those catalogs mark
// as public domain or Creative Commons.
var resolveOpenMedia = resolveOpenMediaLive

var (
	archiveSearchEndpoint   = "https://archive.org/advancedsearch.php"
	archiveMetadataEndpoint = "https://archive.org/metadata/"
	commonsAPIEndpoint      = "https://commons.wikimedia.org/w/api.php"
	nasaSearchEndpoint      = "https://images-api.nasa.gov/search"
	openMediaHTTP           = &http.Client{Timeout: 15 * time.Second}
)

func catalogLookupQuery(query string) string {
	q := strings.TrimSpace(query)
	lower := strings.ToLower(q)
	switch {
	case strings.Contains(q, "大都会") || strings.Contains(lower, "metropolis"):
		return "Metropolis"
	case strings.Contains(q, "诺斯费拉图") || strings.Contains(lower, "nosferatu"):
		return "Nosferatu"
	default:
		return catalogQueryText(q)
	}
}

func resolveOpenMediaLive(ctx context.Context, query string) (string, string, string, bool) {
	query = catalogLookupQuery(query)
	if query == "" || genericCenterMovie(query) || searchQueryForbidden(query) {
		return "", "", "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if rawURL, title, kind, ok := resolveCommonsCatalog(ctx, query); ok {
		return rawURL, title, kind, true
	}
	if !openCatalogWantAudio(query) {
		if rawURL, title, kind, ok := resolveNASACatalog(ctx, query); ok {
			return rawURL, title, kind, true
		}
	}
	return resolveArchiveCatalog(ctx, query)
}

func openCatalogWantAudio(query string) bool {
	lower := strings.ToLower(query)
	if strings.Contains(query, "电影") || strings.Contains(query, "视频") || strings.Contains(lower, "movie") || strings.Contains(lower, "film") {
		return false
	}
	return strings.Contains(query, "歌") || strings.Contains(lower, "song") || strings.Contains(lower, "music")
}

func validateOpenCatalogURL(raw string) (string, string, error) {
	checked, kind, err := validateCenterMediaURL(raw, false)
	if err != nil {
		return "", "", err
	}
	parsed, err := url.Parse(checked)
	if err != nil || !openCatalogHost(parsed) {
		return "", "", errors.New("媒体中心只播放公版片库里的直链")
	}
	return checked, kind, nil
}

func openCatalogHost(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	pathLower := strings.ToLower(u.Path)
	switch {
	case host == "archive.org" || strings.HasSuffix(host, ".archive.org"):
		return strings.Contains(pathLower, "/download/")
	case host == "upload.wikimedia.org":
		return true
	case host == "images-assets.nasa.gov":
		return true
	default:
		return false
	}
}

func resolveArchiveCatalog(ctx context.Context, query string) (string, string, string, bool) {
	mediaType := "movies"
	if openCatalogWantAudio(query) {
		mediaType = "audio"
	}
	q := url.Values{}
	q.Set("q", "mediatype:("+mediaType+") AND (licenseurl:*publicdomain* OR licenseurl:*creativecommons*) AND title:("+catalogQueryText(query)+")")
	q.Add("fl[]", "identifier")
	q.Add("fl[]", "title")
	q.Set("rows", "3")
	q.Set("page", "1")
	q.Set("output", "json")
	body, err := openCatalogGet(ctx, archiveSearchEndpoint+"?"+q.Encode())
	if err != nil {
		return "", "", "", false
	}
	var hits struct {
		Response struct {
			Docs []struct {
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
			} `json:"docs"`
		} `json:"response"`
	}
	if json.Unmarshal(body, &hits) != nil {
		return "", "", "", false
	}
	type archiveHit struct {
		id, title string
		rank      int
	}
	ranked := make([]archiveHit, 0, len(hits.Response.Docs))
	for _, doc := range hits.Response.Docs {
		id := strings.TrimSpace(doc.Identifier)
		if id == "" || strings.Contains(id, "/") || catalogTitleRank(doc.Title, query) == 0 {
			continue
		}
		ranked = append(ranked, archiveHit{id: id, title: doc.Title, rank: catalogTitleRank(doc.Title, query)})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].rank > ranked[j].rank })
	for _, doc := range ranked {
		id := doc.id
		metaBody, err := openCatalogGet(ctx, archiveMetadataEndpoint+url.PathEscape(id))
		if err != nil {
			continue
		}
		var meta struct {
			Metadata struct {
				Title      string `json:"title"`
				LicenseURL string `json:"licenseurl"`
			} `json:"metadata"`
			Files []struct {
				Name   string `json:"name"`
				Format string `json:"format"`
				Size   string `json:"size"`
			} `json:"files"`
		}
		if json.Unmarshal(metaBody, &meta) != nil || !openLicense(meta.Metadata.LicenseURL) {
			continue
		}
		name := pickArchiveFile(meta.Files, openCatalogWantAudio(query))
		if name == "" {
			continue
		}
		title := strings.TrimSpace(meta.Metadata.Title)
		if title == "" {
			title = strings.TrimSpace(doc.title)
		}
		kind := "video"
		if openCatalogWantAudio(query) {
			kind = "audio"
		}
		return archiveDownloadURL(id, name), title, kind, true
	}
	return "", "", "", false
}

func pickArchiveFile(files []struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Size   string `json:"size"`
}, audio bool) string {
	bestName := ""
	bestSize := int64(-1)
	for _, file := range files {
		ext := strings.ToLower(path.Ext(file.Name))
		if audio {
			switch ext {
			case ".mp3", ".m4a", ".flac", ".wav", ".ogg", ".oga":
			default:
				continue
			}
		} else {
			switch ext {
			case ".mp4", ".webm", ".m4v":
			default:
				continue
			}
		}
		lowerName := strings.ToLower(file.Name)
		if strings.Contains(lowerName, "thumb") || strings.Contains(lowerName, ".torrent") {
			continue
		}
		size := catalogFileSize(file.Size)
		if size > 0 && (size < 200_000 || size > 800_000_000) {
			continue
		}
		if bestName == "" || (size > 0 && (bestSize < 0 || size < bestSize)) {
			bestName = file.Name
			bestSize = size
		}
	}
	return bestName
}

func archiveDownloadURL(id, name string) string {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return "https://archive.org/download/" + url.PathEscape(id) + "/" + strings.Join(parts, "/")
}

func resolveCommonsCatalog(ctx context.Context, query string) (string, string, string, bool) {
	search := catalogQueryText(query)
	if openCatalogWantAudio(query) {
		search = "filetype:audio " + search
	} else {
		search = "filetype:video " + search
	}
	q := url.Values{}
	q.Set("action", "query")
	q.Set("generator", "search")
	q.Set("gsrsearch", search)
	q.Set("gsrnamespace", "6")
	q.Set("gsrlimit", "5")
	q.Set("prop", "imageinfo")
	q.Set("iiprop", "url|mime|extmetadata")
	q.Set("iiextmetadatafilter", "LicenseShortName|LicenseUrl")
	q.Set("format", "json")
	body, err := openCatalogGet(ctx, commonsAPIEndpoint+"?"+q.Encode())
	if err != nil {
		return "", "", "", false
	}
	var payload struct {
		Query struct {
			Pages map[string]struct {
				Title     string `json:"title"`
				ImageInfo []struct {
					URL         string `json:"url"`
					Mime        string `json:"mime"`
					ExtMetadata map[string]struct {
						Value string `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", "", "", false
	}
	for _, page := range payload.Query.Pages {
		for _, info := range page.ImageInfo {
			license := info.ExtMetadata["LicenseShortName"].Value + " " + info.ExtMetadata["LicenseUrl"].Value
			if !openLicense(license) {
				continue
			}
			kind := commonsMediaKind(info.Mime, info.URL)
			if kind == "" || catalogTitleRank(page.Title, query) == 0 {
				continue
			}
			if openCatalogWantAudio(query) && kind != "audio" {
				continue
			}
			if !openCatalogWantAudio(query) && kind != "video" {
				continue
			}
			title := strings.TrimPrefix(page.Title, "File:")
			return stripCatalogQuery(info.URL), title, kind, true
		}
	}
	return "", "", "", false
}

func stripCatalogQuery(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func commonsMediaKind(mime, rawURL string) string {
	switch strings.ToLower(mime) {
	case "video/mp4", "video/webm":
		return "video"
	case "audio/mpeg", "audio/mp4", "audio/flac", "audio/wav", "audio/ogg":
		return "audio"
	}
	switch strings.ToLower(path.Ext(strings.Split(rawURL, "?")[0])) {
	case ".mp4", ".webm", ".m4v":
		return "video"
	case ".mp3", ".m4a", ".flac", ".wav", ".ogg", ".oga":
		return "audio"
	default:
		return ""
	}
}

func resolveNASACatalog(ctx context.Context, query string) (string, string, string, bool) {
	q := url.Values{}
	q.Set("q", catalogQueryText(query))
	q.Set("media_type", "video")
	body, err := openCatalogGet(ctx, nasaSearchEndpoint+"?"+q.Encode())
	if err != nil {
		return "", "", "", false
	}
	var hits struct {
		Collection struct {
			Items []struct {
				Href string `json:"href"`
				Data []struct {
					Title string `json:"title"`
				} `json:"data"`
			} `json:"items"`
		} `json:"collection"`
	}
	if json.Unmarshal(body, &hits) != nil {
		return "", "", "", false
	}
	for _, item := range hits.Collection.Items {
		href := strings.TrimSpace(item.Href)
		if href == "" || (!strings.HasPrefix(href, "https://images-assets.nasa.gov/") && !strings.HasPrefix(href, nasaSearchEndpoint)) {
			continue
		}
		collectionBody, err := openCatalogGet(ctx, href)
		if err != nil {
			continue
		}
		var files []string
		if json.Unmarshal(collectionBody, &files) != nil {
			continue
		}
		rawURL := pickNASAFile(files)
		if rawURL == "" {
			continue
		}
		title := ""
		if len(item.Data) > 0 {
			title = strings.TrimSpace(item.Data[0].Title)
		}
		if catalogTitleRank(title, query) == 0 {
			continue
		}
		return rawURL, title, "video", true
	}
	return "", "", "", false
}

func pickNASAFile(urls []string) string {
	best := ""
	bestRank := 9
	for _, raw := range urls {
		rank := nasaFileRank(raw)
		if rank < bestRank {
			best = raw
			bestRank = rank
		}
	}
	if bestRank == 9 {
		return ""
	}
	return best
}

func nasaFileRank(raw string) int {
	lower := strings.ToLower(strings.Split(raw, "?")[0])
	if !strings.HasPrefix(lower, "https://images-assets.nasa.gov/") {
		return 9
	}
	switch {
	case strings.Contains(lower, "~medium.mp4"):
		return 0
	case strings.Contains(lower, "~mobile.mp4"), strings.Contains(lower, "~small.mp4"):
		return 1
	case strings.HasSuffix(lower, ".mp4"), strings.HasSuffix(lower, ".webm"):
		return 2
	default:
		return 9
	}
}

func catalogTitleRank(title, query string) int {
	got := normalizeCatalogTitle(title)
	want := normalizeCatalogTitle(query)
	if got == "" || want == "" {
		return 0
	}
	if got == want {
		return 100
	}
	if strings.HasPrefix(got, want+" ") && len([]rune(strings.TrimPrefix(got, want))) <= 40 {
		return 80
	}
	return 0
}

func normalizeCatalogTitle(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "File:"))
	value = strings.TrimSuffix(value, path.Ext(value))
	return strings.ToLower(catalogQueryText(value))
}

func openLicense(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return false
	}
	return strings.Contains(text, "public domain") || strings.Contains(text, "publicdomain") || strings.Contains(text, "cc0") || strings.Contains(text, "creative commons") || strings.Contains(text, "creativecommons.org") || strings.Contains(text, "cc by")
}

func catalogQueryText(query string) string {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case '(', ')', '"', '\'', '\\', ':', '&', '|', '!', '{', '}', '[', ']':
			return ' '
		default:
			return r
		}
	}, query)
	clean = strings.Join(strings.Fields(clean), " ")
	if len(clean) > 120 {
		clean = clean[:120]
	}
	return clean
}

func catalogFileSize(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func openCatalogGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LunitideMediaCenter/1.0 (public-domain catalog lookup)")
	req.Header.Set("Accept", "application/json")
	resp, err := openMediaHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("catalog request failed")
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}
