package webviewhost

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/lunitide/lunitide/internal/mediaapp"
)

const (
	MediaVirtualHost             = "media.lunitide.local"
	MediaAssetPathPrefix         = "/v1/assets/"
	MediaResourceFilterURI       = "https://media.lunitide.local/v1/assets/*"
	MediaResourceContextMedia    = int32(4) // COREWEBVIEW2_WEB_RESOURCE_CONTEXT_MEDIA
	mediaTicketMinLen            = 16
	mediaTicketMaxLen            = 64
)

// ParseMediaAssetTicket accepts only https://media.lunitide.local/v1/assets/<token>
// with no userinfo, port, query, fragment, or extra path.
func ParseMediaAssetTicket(raw string) (token string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	if strings.ToLower(u.Host) != MediaVirtualHost {
		return "", false
	}
	if !strings.HasPrefix(u.Path, MediaAssetPathPrefix) {
		return "", false
	}
	token = strings.TrimPrefix(u.Path, MediaAssetPathPrefix)
	if token == "" || strings.ContainsAny(token, "/\\") || len(token) < mediaTicketMinLen || len(token) > mediaTicketMaxLen {
		return "", false
	}
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return "", false
	}
	return token, true
}

// MediaResourceAllowed is COM-free so the origin/context policy can be tested
// without WebView2. Only the trusted app document may request media context.
func MediaResourceAllowed(sourceURL string, resourceContext int32, requestURL string) bool {
	if resourceContext != MediaResourceContextMedia || !NavigationAllowed(sourceURL) {
		return false
	}
	_, ok := ParseMediaAssetTicket(requestURL)
	return ok
}

func MediaResponseHeaders(result mediaapp.RangeResult) string {
	var b strings.Builder
	ctype := result.ContentType
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	fmt.Fprintf(&b, "Content-Type: %s\r\n", ctype)
	fmt.Fprintf(&b, "Content-Length: %d\r\n", result.ContentLength)
	if result.AcceptRanges != "" {
		fmt.Fprintf(&b, "Accept-Ranges: %s\r\n", result.AcceptRanges)
	}
	if result.ContentRange != "" {
		fmt.Fprintf(&b, "Content-Range: %s\r\n", result.ContentRange)
	}
	b.WriteString("X-Content-Type-Options: nosniff\r\nCache-Control: no-store")
	return b.String()
}

func MediaStatusReason(status int) string {
	if text := http.StatusText(status); text != "" {
		return text
	}
	return strconv.Itoa(status)
}
