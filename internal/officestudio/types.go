// Package officestudio implements bounded, side-effect-free Office content
// inspection, generation and conservative edits. It never starts Office or
// treats a package check as proof of rendering in a target application.
package officestudio

type Kind string

const (
	PPTX Kind = "pptx"
	DOCX Kind = "docx"
	XLSX Kind = "xlsx"
	PDF  Kind = "pdf"
)

const (
	MaxInputBytes    = 32 << 20
	MaxPartBytes     = 16 << 20
	MaxExpandedBytes = 128 << 20
	MaxParts         = 4096
	MaxNodes         = 50000
)

type Issue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Part     string `json:"part,omitempty"`
	NodeID   string `json:"nodeId,omitempty"`
}

type Part struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

// IDs identify source locations, not text values, and remain stable across
// supported text edits. Digest binds the exact original XML element.
type Node struct {
	ID       string     `json:"id"`
	Part     string     `json:"part"`
	Kind     string     `json:"kind"`
	Text     string     `json:"text"`
	Digest   string     `json:"digest"`
	Ordinal  int        `json:"ordinal"`
	Editable bool       `json:"editable"`
	Locator  string     `json:"locator"`
	Image    *ImageInfo `json:"image,omitempty"`
	Chart    *ChartInfo `json:"chart,omitempty"`
}

type Inspection struct {
	Kind          Kind       `json:"kind"`
	SHA256        string     `json:"sha256"`
	Size          int        `json:"size"`
	RenderAllowed bool       `json:"renderAllowed"`
	Editability   string     `json:"editability"`
	Nodes         []Node     `json:"nodes"`
	Parts         []Part     `json:"parts"`
	Issues        []Issue    `json:"issues"`
	Preview       string     `json:"preview"`
	Structure     *Structure `json:"structure,omitempty"`
}

type Structure struct {
	Sections         int            `json:"sections,omitempty"`
	Tables           int            `json:"tables,omitempty"`
	HeadingCounts    map[string]int `json:"headingCounts,omitempty"`
	TOCFields        int            `json:"tocFields,omitempty"`
	PageFields       int            `json:"pageFields,omitempty"`
	NeedsFieldUpdate bool           `json:"needsFieldUpdate,omitempty"`
}

type Spec struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          Kind             `json:"kind"`
	Title         string           `json:"title"`
	BrandID       string           `json:"brandId,omitempty"`
	TemplateID    string           `json:"templateId,omitempty"`
	Slides        []Slide          `json:"slides,omitempty"`
	Blocks        []Block          `json:"blocks,omitempty"`
	Sheets        []Sheet          `json:"sheets,omitempty"`
	Body          string           `json:"body,omitempty"`
	Audience        string           `json:"audience,omitempty"`
	Purpose         string           `json:"purpose,omitempty"`
	Confidentiality string           `json:"confidentiality,omitempty"`
	Document      *DocumentOptions `json:"document,omitempty"`
	Facts         []Fact           `json:"facts,omitempty"`
}

type DocumentOptions struct {
	Header          string `json:"header,omitempty"`
	Footer          string `json:"footer,omitempty"`
	PageNumbers     bool   `json:"pageNumbers,omitempty"`
	PageNumberStart int    `json:"pageNumberStart,omitempty"`
	PageSize        string `json:"pageSize,omitempty"`    // A4 or Letter; default A4
	Orientation     string `json:"orientation,omitempty"` // portrait or landscape
}

// Layout supports cover, section, content, two-column, comparison, quote,
// metrics, timeline, agenda, closing and native table. Bullets are never silently truncated.
type Slide struct {
	Title        string           `json:"title"`
	Subtitle     string           `json:"subtitle,omitempty"`
	Layout       string           `json:"layout,omitempty"`
	Bullets      []string         `json:"bullets,omitempty"`
	Notes        string           `json:"notes,omitempty"`
	Rows         [][]string       `json:"rows,omitempty"`
	Images       []SlideImage     `json:"images,omitempty"`
	Charts       []SlideChart     `json:"charts,omitempty"`
	Purpose      string           `json:"purpose,omitempty"`
	Claim        string           `json:"claim,omitempty"`
	EvidenceRefs []string         `json:"evidenceRefs,omitempty"`
	Comparison   *ComparisonBlock `json:"comparison,omitempty"`
	Metrics      []MetricBlock    `json:"metrics,omitempty"`
	Evidence     []EvidenceItem   `json:"evidence,omitempty"`
}

// Native charts contain editable DrawingML objects and an internal XLSX data
// source. Values are explicit decimal strings, never JSON floating-point numbers.
type ChartSeries struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}
type SlideChart struct {
	Type       string        `json:"type"` // column, bar, line, pie
	Title      string        `json:"title"`
	Categories []string      `json:"categories"`
	Series     []ChartSeries `json:"series"`
	X          int64         `json:"x"`
	Y          int64         `json:"y"`
	Width      int64         `json:"width"`
	Height     int64         `json:"height"`
	Legend     bool          `json:"legend"`
}
type ChartInfo struct {
	ChartPart      string   `json:"chartPart"`
	ChartSHA256    string   `json:"chartSha256"`
	WorkbookPart   string   `json:"workbookPart,omitempty"`
	WorkbookSHA256 string   `json:"workbookSha256,omitempty"`
	Type           string   `json:"type,omitempty"`
	SeriesCount    int      `json:"seriesCount"`
	CategoryCount  int      `json:"categoryCount"`
	X              int64    `json:"x"`
	Y              int64    `json:"y"`
	Width          int64    `json:"width"`
	Height         int64    `json:"height"`
	SourceRanges   []string `json:"sourceRanges,omitempty"`
	CacheState     string   `json:"cacheState,omitempty"`
}

// ChartPatch replaces managed chart data/type/title. The source frame geometry
// remains unchanged; X/Y/Width/Height in Chart must be zero or exactly match it.
type ChartPatch struct {
	NodeID         string     `json:"nodeId"`
	ExpectedDigest string     `json:"expectedDigest"`
	Chart          SlideChart `json:"chart"`
}

// SlideImage accepts only bytes hydrated from a caller-verified managed source.
// Data is deliberately excluded from JSON model/tool contracts and version metadata.
// Geometry is in EMU (914400 per inch); contain preserves the whole image and
// cover uses a centered DrawingML source crop. Images are never stretched.
type SlideImage struct {
	SourceID string `json:"sourceId"`
	SHA256   string `json:"sha256"`
	X        int64  `json:"x"`
	Y        int64  `json:"y"`
	Width    int64  `json:"width"`
	Height   int64  `json:"height"`
	Fit      string `json:"fit"`
	Alt      string `json:"alt"`
	Data     []byte `json:"-"`
}

type ImageInfo struct {
	MediaPart      string `json:"mediaPart"`
	MediaSHA256    string `json:"mediaSha256"`
	RelationshipID string `json:"relationshipId"`
	SourceID       string `json:"sourceId,omitempty"`
	PixelWidth     int    `json:"pixelWidth,omitempty"`
	PixelHeight    int    `json:"pixelHeight,omitempty"`
	X              int64  `json:"x"`
	Y              int64  `json:"y"`
	Width          int64  `json:"width"`
	Height         int64  `json:"height"`
	CropLeft       int64  `json:"cropLeft,omitempty"`
	CropRight      int64  `json:"cropRight,omitempty"`
	CropTop        int64  `json:"cropTop,omitempty"`
	CropBottom     int64  `json:"cropBottom,omitempty"`
}

// ImagePatch replaces one simple embedded picture without modifying shared
// media. Geometry comes from the source node; alt=nil preserves the current text.
type ImagePatch struct {
	NodeID         string  `json:"nodeId"`
	ExpectedDigest string  `json:"expectedDigest"`
	SourceID       string  `json:"sourceId"`
	SHA256         string  `json:"sha256"`
	Fit            string  `json:"fit"`
	Alt            *string `json:"alt,omitempty"`
	Data           []byte  `json:"-"`
}

type Block struct {
	Type         string           `json:"type"` // heading, heading2, heading3, paragraph, bullet, numbered, quote, caption, table, pagebreak, toc, section
	Text         string           `json:"text,omitempty"`
	Rows         [][]string       `json:"rows,omitempty"`
	Section      *DocumentOptions `json:"section,omitempty"`
	Purpose      string           `json:"purpose,omitempty"`
	Claim        string           `json:"claim,omitempty"`
	EvidenceRefs []string         `json:"evidenceRefs,omitempty"`
}

type Sheet struct {
	Name         string       `json:"name"`
	Rows         [][]Cell     `json:"rows"`
	FreezeHeader bool         `json:"freezeHeader,omitempty"`
	Charts       []SheetChart `json:"charts,omitempty"`
}

// SheetChart references explicit local, single-column A1 ranges. Series values
// must initially be number cells, and category labels text/number cells. Formula
// caches are not treated as newly calculated data. Width/height are pixels.
type SheetChart struct {
	Type       string             `json:"type"`
	Title      string             `json:"title"`
	Categories string             `json:"categories"`
	Series     []SheetChartSeries `json:"series"`
	Anchor     string             `json:"anchor"`
	Width      int                `json:"width"`
	Height     int                `json:"height"`
	Legend     bool               `json:"legend"`
}
type SheetChartSeries struct {
	Name  string `json:"name"`
	Range string `json:"range"`
}

// Cell is deliberately typed. Text, including '=...', leading zeroes and long
// identifiers, stays text. Only formula cells are written as formulas.
// Decimal values use decimal text to avoid a JSON float round-trip.
type Cell struct {
	Type   string `json:"type"` // text, number, boolean, formula, date, blank
	Value  string `json:"value,omitempty"`
	Format string `json:"format,omitempty"`
}

type TextPatch struct {
	NodeID         string `json:"nodeId"`
	ExpectedDigest string `json:"expectedDigest"`
	Text           string `json:"text"`
}

type PatchRequest struct {
	Kind       Kind         `json:"kind"`
	BaseSHA256 string       `json:"baseSha256"`
	Operations []TextPatch  `json:"operations"`
	Ranges     []RangePatch `json:"ranges,omitempty"`
	Images     []ImagePatch `json:"images,omitempty"`
	Charts     []ChartPatch `json:"charts,omitempty"`
}

// ExpectedDigest is the original uncompressed worksheet part SHA256 from the
// inspection. Rows must exactly match the inclusive A1 range dimensions.
// Existing styles are preserved; changing cell.Format is not a range edit.
type RangePatch struct {
	Part           string   `json:"part"`
	Range          string   `json:"range"`
	ExpectedDigest string   `json:"expectedDigest"`
	Rows           [][]Cell `json:"rows"`
}

type CalculationReport struct {
	FormulaCount            int      `json:"formulaCount"`
	InvalidatedCaches       int      `json:"invalidatedCaches"`
	AffectedFormulaNodes    []string `json:"affectedFormulaNodes"`
	DependencyCoverage      string   `json:"dependencyCoverage"`
	Recalculation           string   `json:"recalculation"`
	Notice                  string   `json:"notice"`
	ChartCachesUpdated      int      `json:"chartCachesUpdated,omitempty"`
	ChartCachesInvalidated  int      `json:"chartCachesInvalidated,omitempty"`
	ChartDependencyCoverage string   `json:"chartDependencyCoverage,omitempty"`
	UnknownCharts           int      `json:"unknownCharts,omitempty"`
}

type PatchResult struct {
	Data         []byte             `json:"-"`
	Inspection   Inspection         `json:"inspection"`
	ChangedParts []string           `json:"changedParts"`
	Calculation  *CalculationReport `json:"calculation,omitempty"`
}

type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // passed, missing, blocked
	Message string `json:"message"`
}

type Validation struct {
	Status string  `json:"status"`
	Checks []Check `json:"checks"`
	Issues []Issue `json:"issues"`
}
