package officestudio

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/xuri/excelize/v2"
)

// Generate creates managed documents only. Imported OOXML must be edited
// through Patch; reconstructing an import from this spec would lose opaque
// parts and is intentionally not an operation offered by this package.
func Generate(spec Spec) ([]byte, error) {
	if spec.SchemaVersion != 1 {
		return nil, fmt.Errorf("%w: unsupported spec schema version", ErrFormat)
	}
	if strings.TrimSpace(spec.Title) == "" || len(spec.Title) > 1024 || !validText(spec.Title) {
		return nil, fmt.Errorf("%w: title is required", ErrFormat)
	}
	if spec.Kind != PPTX && len(spec.Slides) > 0 || spec.Kind != DOCX && (len(spec.Blocks) > 0 || spec.Document != nil) || spec.Kind != XLSX && len(spec.Sheets) > 0 || spec.Kind != PDF && spec.Body != "" {
		return nil, fmt.Errorf("%w: content fields do not match document kind; no content was discarded", ErrFormat)
	}
	var data []byte
	var err error
	switch spec.Kind {
	case PPTX:
		if len(spec.Slides) == 0 || len(spec.Slides) > officetools.MaxPptxSlides {
			return nil, ErrLimit
		}
		slides := make([]officetools.SlideSpec, len(spec.Slides))
		tables := map[int][][]string{}
		for i, s := range spec.Slides {
			if !validText(s.Title) || !validText(s.Subtitle) || !validText(s.Notes) {
				return nil, ErrFormat
			}
			for _, v := range s.Bullets {
				if !validText(v) {
					return nil, ErrFormat
				}
			}
			if len(s.Rows) > 0 {
				for _, row := range s.Rows {
					for _, v := range row {
						if !validText(v) {
							return nil, ErrFormat
						}
					}
				}
				tables[i] = s.Rows
			}
			slides[i] = officetools.SlideSpec{Title: s.Title, Subtitle: s.Subtitle, Layout: s.Layout, Bullets: s.Bullets, Notes: s.Notes}
			if s.Layout == "" && (len(s.Images) > 0 || len(s.Charts) > 0) {
				// Object coordinates describe the content area. A first-slide cover
				// puts its title in that area and would overlap otherwise valid data.
				slides[i].Layout = "content"
			}
		}
		data, err = officetools.GenStudioPptxWithTables(spec.Title, slides, tables)
		if err == nil {
			data, err = addSlideImages(data, spec.Slides)
		}
		if err == nil {
			data, err = addSlideCharts(data, spec.Slides)
		}
		if err == nil {
			_, data, err = RepairGeometryOverflow(data)
		}
	case DOCX:
		blocks := make([]officetools.StudioDocxBlock, len(spec.Blocks))
		for i, b := range spec.Blocks {
			if b.Type != "table" && len(b.Rows) > 0 || b.Type != "section" && b.Section != nil || (b.Type == "table" || b.Type == "toc" || b.Type == "pagebreak") && b.Text != "" {
				return nil, fmt.Errorf("%w: block %d contains fields not supported by %s", ErrFormat, i+1, b.Type)
			}
			if !validText(b.Text) {
				return nil, ErrFormat
			}
			for _, row := range b.Rows {
				for _, v := range row {
					if !validText(v) {
						return nil, ErrFormat
					}
				}
			}
			section, err := docOptions(b.Section)
			if err != nil {
				return nil, err
			}
			blocks[i] = officetools.StudioDocxBlock{Type: b.Type, Text: b.Text, Rows: b.Rows, Section: section}
		}
		options, optionErr := docOptions(spec.Document)
		if optionErr != nil {
			return nil, optionErr
		}
		data, err = officetools.GenStudioDocxWithOptions(spec.Title, blocks, options)
	case XLSX:
		data, err = generateXLSX(spec.Sheets)
		if err == nil {
			data, err = addSheetCharts(data, spec.Sheets)
		}
	case PDF:
		if !validText(spec.Body) {
			return nil, ErrFormat
		}
		data, err = officetools.GenStablePDF(spec.Title, spec.Body)
	default:
		return nil, ErrFormat
	}
	if err != nil {
		return nil, err
	}
	i, err := Inspect(spec.Kind, data)
	if err != nil {
		return nil, err
	}
	if i.Editability == "blocked" {
		return nil, fmt.Errorf("%w: generated document failed preflight: %v", ErrFormat, i.Issues)
	}
	return data, nil
}

func docOptions(o *DocumentOptions) (*officetools.StudioDocumentOptions, error) {
	if o == nil {
		return nil, nil
	}
	if !validText(o.Header) || !validText(o.Footer) {
		return nil, ErrFormat
	}
	return &officetools.StudioDocumentOptions{Header: o.Header, Footer: o.Footer, PageNumbers: o.PageNumbers, PageNumberStart: o.PageNumberStart, Orientation: o.Orientation, PageSize: o.PageSize}, nil
}

var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)
var dangerousFormula = regexp.MustCompile(`(?i)(?:\b(?:WEBSERVICE|HYPERLINK|RTD|CALL|EXEC|REGISTER|REGISTER\.ID|EVALUATE|DDE|IMAGE|FILTERXML)\s*\(|\[[^\]]+\][^!+*/(),;]*!|\||https?://|file:|\\\\|\b[A-Z]:\\)`)

func generateXLSX(sheets []Sheet) ([]byte, error) {
	if len(sheets) == 0 || len(sheets) > officetools.MaxSheets {
		return nil, ErrLimit
	}
	f := excelize.NewFile()
	defer f.Close()
	seen := map[string]bool{}
	total := 0
	styleCache := map[string]int{}
	for si, s := range sheets {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = fmt.Sprintf("Sheet%d", si+1)
		}
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("%w: duplicate sheet name", ErrFormat)
		}
		seen[strings.ToLower(name)] = true
		if len(s.Rows) == 0 || len(s.Rows) > officetools.MaxRowsPerSheet {
			return nil, ErrLimit
		}
		if si == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return nil, err
			}
		} else {
			if _, err := f.NewSheet(name); err != nil {
				return nil, err
			}
		}
		for ri, row := range s.Rows {
			if len(row) > officetools.MaxColsPerSheet {
				return nil, ErrLimit
			}
			total += len(row)
			if total > officetools.MaxCellsTotal {
				return nil, ErrLimit
			}
			for ci, cell := range row {
				address, err := excelize.CoordinatesToCellName(ci+1, ri+1)
				if err != nil {
					return nil, err
				}
				format, err := writeTypedCell(f, name, address, cell)
				if err != nil {
					return nil, fmt.Errorf("%s!%s: %w", name, address, err)
				}
				{
					styleKey := fmt.Sprintf("%t:%s", s.FreezeHeader && ri == 0, format)
					style, ok := styleCache[styleKey]
					if !ok {
						options := &excelize.Style{Font: &excelize.Font{Size: 11}, Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}}
						if format != "" {
							options.CustomNumFmt = &format
						}
						if s.FreezeHeader && ri == 0 {
							options.Font.Bold = true
							options.Font.Color = "15314A"
							options.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"E8F0F6"}}
						}
						style, err = f.NewStyle(options)
						if err != nil {
							return nil, err
						}
						styleCache[styleKey] = style
					}
					if err := f.SetCellStyle(name, address, address, style); err != nil {
						return nil, err
					}
				}
			}
		}
		if err := layoutManagedSheet(f, name, s); err != nil {
			return nil, err
		}
		if s.FreezeHeader {
			if err := f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
				return nil, err
			}
		}
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		return nil, err
	}
	// Restore explicit decimal representations after the library has assembled
	// styles and layout. No numerical source value passes through a float writer.
	p, err := readPackage(b.Bytes())
	if err != nil {
		return nil, err
	}
	replaced := map[string][]byte{}
	for si, s := range sheets {
		writes := map[string]Cell{}
		for ri, row := range s.Rows {
			for ci, cell := range row {
				if cell.Type == "number" {
					address, _ := excelize.CoordinatesToCellName(ci+1, ri+1)
					cell.Format = ""
					writes[address] = cell
				}
			}
		}
		if len(writes) == 0 {
			continue
		}
		part := fmt.Sprintf("xl/worksheets/sheet%d.xml", si+1)
		body, err := editSheet(p.parts[part], writes, false)
		if err != nil {
			return nil, err
		}
		replaced[part] = body
	}
	if len(replaced) == 0 {
		return b.Bytes(), nil
	}
	return rewritePackage(p, replaced)
}

func writeTypedCell(f *excelize.File, sheet, address string, cell Cell) (string, error) {
	if !validText(cell.Value) || len(cell.Value) > 32767 || !validText(cell.Format) || len(cell.Format) > 128 {
		return "", ErrLimit
	}
	format := cell.Format
	switch cell.Type {
	case "text":
		// SetCellStr never routes strings beginning with '=' into SetCellFormula.
		if err := f.SetCellStr(sheet, address, cell.Value); err != nil {
			return "", err
		}
		if format == "" {
			format = "@"
		}
	case "number":
		if !decimalPattern.MatchString(cell.Value) {
			return "", fmt.Errorf("%w: number must be decimal text", ErrFormat)
		}
		digits := strings.TrimLeft(strings.ReplaceAll(strings.TrimPrefix(cell.Value, "-"), ".", ""), "0")
		if len(digits) > 15 {
			return "", fmt.Errorf("%w: Excel numbers support 15 significant digits; store identifiers as text", ErrFormat)
		}
		value, err := strconv.ParseFloat(cell.Value, 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return "", ErrFormat
		}
		if err := f.SetCellStr(sheet, address, cell.Value); err != nil {
			return "", err
		}
	case "boolean":
		if cell.Value != "true" && cell.Value != "false" {
			return "", fmt.Errorf("%w: boolean must be true or false", ErrFormat)
		}
		if err := f.SetCellBool(sheet, address, cell.Value == "true"); err != nil {
			return "", err
		}
	case "formula":
		formula, err := localFormula(cell.Value)
		if err != nil {
			return "", err
		}
		if err := f.SetCellFormula(sheet, address, formula); err != nil {
			return "", err
		}
	case "date":
		value, err := time.Parse("2006-01-02", cell.Value)
		if err != nil || value.Year() < 1900 || value.Year() > 9999 {
			return "", fmt.Errorf("%w: date must be YYYY-MM-DD, 1900–9999", ErrFormat)
		}
		if err := f.SetCellValue(sheet, address, value); err != nil {
			return "", err
		}
		if format == "" {
			format = "yyyy-mm-dd"
		}
	case "blank":
		if cell.Value != "" {
			return "", fmt.Errorf("%w: blank cell cannot have a value", ErrFormat)
		}
	default:
		return "", fmt.Errorf("%w: explicit cell type is required", ErrFormat)
	}
	return format, nil
}

// Formula is an explicit cell type, so both '=SUM(A1:A2)' and the OOXML-style
// 'SUM(A1:A2)' are unambiguous. Text cells never use this normalization.
func localFormula(value string) (string, error) {
	formula := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "="))
	if formula == "" || strings.HasPrefix(formula, "=") || dangerousFormula.MatchString(formula) {
		return "", fmt.Errorf("%w: only local workbook formulas are supported", ErrReadOnly)
	}
	return formula, nil
}
