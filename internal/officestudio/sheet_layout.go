package officestudio

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"
)

// Managed sheets get bounded readable defaults. Imported worksheet dimensions,
// printing settings and styles are never rewritten by this helper.
func layoutManagedSheet(f *excelize.File, name string, sheet Sheet) error {
	var widths []float64
	for _, row := range sheet.Rows {
		for ci, cell := range row {
			for len(widths) <= ci {
				widths = append(widths, 12)
			}
			width := float64(displayWidth(cell.Value) + 3)
			if cell.Type == "formula" {
				width = 16
			} // The formula is not its displayed result.
			if cell.Type == "date" {
				width = 14
			}
			widths[ci] = math.Max(widths[ci], math.Min(width, 48))
		}
	}
	total := 0.0
	for ci, width := range widths {
		col, err := excelize.ColumnNumberToName(ci + 1)
		if err != nil {
			return err
		}
		if err = f.SetColWidth(name, col, col, width); err != nil {
			return err
		}
		total += width
	}
	for ri, row := range sheet.Rows {
		lines := 1
		for ci, cell := range row {
			if cell.Type == "formula" || cell.Type == "date" {
				continue
			}
			count := 0
			for _, line := range strings.Split(cell.Value, "\n") {
				count += max(1, int(math.Ceil(float64(displayWidth(line))/math.Max(widths[ci]-2, 1))))
			}
			lines = max(lines, count)
		}
		if err := f.SetRowHeight(name, ri+1, math.Min(409.5, float64(lines)*16+8)); err != nil {
			return err
		}
	}
	orientation := "portrait"
	if total > 90 {
		orientation = "landscape"
	}
	paper, fitWidth, fitHeight := 9, 1, 0
	// Very wide tables paginate instead of shrinking all columns to unreadable text.
	if total > 160 {
		fitWidth = 0
	}
	if err := f.SetPageLayout(name, &excelize.PageLayoutOptions{Size: &paper, Orientation: &orientation, FitToWidth: &fitWidth, FitToHeight: &fitHeight}); err != nil {
		return err
	}
	fit := true
	if err := f.SetSheetProps(name, &excelize.SheetPropsOptions{FitToPage: &fit}); err != nil {
		return err
	}
	side, vertical := 0.35, 0.5
	return f.SetPageMargins(name, &excelize.PageLayoutMarginsOptions{Left: &side, Right: &side, Top: &vertical, Bottom: &vertical})
}

func applySheetRoleLayout(f *excelize.File, name string, sheet Sheet) error {
	lastRow := len(sheet.Rows)
	lastCol := 1
	for _, row := range sheet.Rows {
		if len(row) > lastCol {
			lastCol = len(row)
		}
	}
	end, err := excelize.CoordinatesToCellName(lastCol, max(lastRow, 1))
	if err != nil {
		return err
	}
	if err = setManagedPrintArea(f, name, lastCol, max(lastRow, 1)); err != nil {
		return err
	}
	switch name {
	case "原始数据":
		return f.AutoFilter(name, "A1:"+end, nil)
	case "看板":
		show := false
		return f.SetSheetView(name, 0, &excelize.ViewOptions{ShowGridLines: &show})
	default:
		return nil
	}
}

func setManagedPrintArea(f *excelize.File, name string, lastCol, lastRow int) error {
	if lastRow < 1 {
		lastRow = 1
	}
	if lastCol < 1 {
		lastCol = 1
	}
	col, err := excelize.ColumnNumberToName(lastCol)
	if err != nil {
		return err
	}
	quoted := "'" + strings.ReplaceAll(name, "'", "''") + "'"
	return f.SetDefinedName(&excelize.DefinedName{
		Name:     "_xlnm.Print_Area",
		RefersTo: fmt.Sprintf("%s!$A$1:$%s$%d", quoted, col, lastRow),
		Scope:    name,
	})
}

func displayWidth(value string) int {
	widest, current := 0, 0
	for _, r := range value {
		switch {
		case r == '\n':
			widest = max(widest, current)
			current = 0
		case r == '\t':
			current += 4
		case unicode.Is(unicode.Mn, r):
		case r >= 0x2e80:
			current += 2
		default:
			current++
		}
	}
	return max(widest, current)
}
