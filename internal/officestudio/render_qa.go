package officestudio

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
)

// Deterministic raster QA. These checks run on whatever per-page renders the
// pipeline can actually produce (LibreOffice preview export or the platform
// PDF page renderer). They never claim anything about pages that were not
// rendered.

const (
	// renderQABorderBandPermyriad is the outer border band thickness in
	// 1/10000 of the page's smaller dimension: 125 means 1.25%. Content
	// bleeding into this band is a clipping signal.
	renderQABorderBandPermyriad = 125
	// renderQAInkMin is the minimum share of pixels that must differ from the
	// page's dominant color, otherwise the page is judged blank.
	renderQAInkMin = 0.002
	// renderQABorderInkMax is the share of band pixels allowed to differ from
	// the dominant color before the page reports border ink.
	renderQABorderInkMax = 0.02
	// renderQAPhotoInkMax suppresses border-ink on image-heavy pages, where
	// band ink is imagery rather than layout clipping.
	renderQAPhotoInkMax = 0.35
	// renderQAMinDimension rejects tiny fragments that cannot be slides.
	renderQAMinDimension = 64
)

type renderPageStats struct {
	page                                int
	width, height                       int
	total, ink                          int64
	borderTotal, borderInk              int64
	decodeFailed, tooSmall, blank       bool
	inkRatio, borderInkRatio            float64
}

// RenderedPageQA checks rendered page PNGs with deterministic pixel rules:
// page-count consistency with the source deck, non-blank content, and a clean
// outer border band. expectedPages <= 0 skips the count comparison (PDF kind).
// It returns one "render-pixel-qa" check and, when pages were analyzed, one
// "render-border-ink" check.
func RenderedPageQA(pages [][]byte, expectedPages int) []Check {
	if len(pages) == 0 {
		return []Check{{
			ID:     "render-pixel-qa",
			Status: "unsupported",
			Message: "没有可分析的逐页渲染图；实际排版像素检查未执行，不能当作已验证",
		}}
	}
	stats := make([]renderPageStats, len(pages))
	decodeFailures := []int{}
	tooSmallPages := []int{}
	blankPages := []int{}
	borderPages := []string{}
	photoPages := 0
	for i, raw := range pages {
		stats[i] = analyzeRenderedPage(i+1, raw)
		if stats[i].decodeFailed {
			decodeFailures = append(decodeFailures, i+1)
		} else if stats[i].tooSmall {
			tooSmallPages = append(tooSmallPages, i+1)
		} else if stats[i].blank {
			blankPages = append(blankPages, i+1)
		} else if stats[i].inkRatio > renderQAPhotoInkMax {
			// Image-heavy art pages legitimately reach the border; band ink
			// there is imagery, not layout clipping.
			photoPages++
		} else if stats[i].borderInkRatio > renderQABorderInkMax {
			borderPages = append(borderPages, fmt.Sprintf("第%d页 %.1f%%", i+1, stats[i].borderInkRatio*100))
		}
	}
	countMismatch := expectedPages > 0 && expectedPages != len(pages)
	status, msg := "passed", fmt.Sprintf("已逐页像素检查 %d 页：非空白、无解码失败", len(pages))
	if countMismatch {
		status = "failed"
		msg = fmt.Sprintf("渲染页数 %d 与源页数 %d 不一致", len(pages), expectedPages)
	} else if len(decodeFailures) > 0 {
		status = "failed"
		msg = fmt.Sprintf("第 %s 页渲染图解码失败", joinInts(decodeFailures))
	} else if len(tooSmallPages) > 0 {
		status = "failed"
		msg = fmt.Sprintf("第 %s 页渲染图尺寸过小，不足以构成版面", joinInts(tooSmallPages))
	} else if len(blankPages) > 0 {
		status = "failed"
		msg = fmt.Sprintf("第 %s 页接近空白：版面没有可见内容", joinInts(blankPages))
	} else if expectedPages > 0 {
		msg = fmt.Sprintf("已逐页像素检查 %d 页：页数一致、非空白、无解码失败", len(pages))
	}
	pixel := Check{ID: "render-pixel-qa", Status: status, Message: msg}
	border := Check{ID: "render-border-ink", Status: "passed", Message: fmt.Sprintf("%d 页边框带干净", len(pages))}
	if photoPages > 0 && status == "passed" {
		border.Message = fmt.Sprintf("%d 页边框带干净（%d 页图像为主，跳过边框判定）", len(pages), photoPages)
	}
	if status != "passed" {
		border.Status = "unsupported"
		border.Message = "像素基础检查未通过，边框结论不可用"
	} else if len(borderPages) > 0 {
		border.Status = "failed"
		border.Message = "内容贴近页边，可能被裁切：" + strings.Join(borderPages, "、") + "；请核对实际排版"
	}
	return []Check{pixel, border}
}

// analyzeRenderedPage decodes one PNG and measures ink relative to the page's
// dominant color, so full-bleed dark covers are not misread as defects.
func analyzeRenderedPage(page int, raw []byte) renderPageStats {
	stats := renderPageStats{page: page}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		stats.decodeFailed = true
		return stats
	}
	bounds := img.Bounds()
	stats.width, stats.height = bounds.Dx(), bounds.Dy()
	if stats.width < renderQAMinDimension || stats.height < renderQAMinDimension {
		stats.decodeFailed = false
		stats.tooSmall = true
		return stats
	}
	type pixelReader func(x, y int) (uint8, uint8, uint8, uint8)
	quant := func(c uint32) uint8 { return uint8(c >> 8) }
	buckets := map[uint32]int64{}
	var at pixelReader
	switch src := img.(type) {
	case *image.RGBA:
		at = func(x, y int) (uint8, uint8, uint8, uint8) {
			i := src.PixOffset(x, y)
			return src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3]
		}
	case *image.NRGBA:
		at = func(x, y int) (uint8, uint8, uint8, uint8) {
			i := src.PixOffset(x, y)
			return src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3]
		}
	case *image.Gray:
		at = func(x, y int) (uint8, uint8, uint8, uint8) {
			v := src.GrayAt(x, y).Y
			return v, v, v, 0xFF
		}
	default:
		at = func(x, y int) (uint8, uint8, uint8, uint8) {
			r, g, b, a := img.At(x, y).RGBA()
			return quant(r), quant(g), quant(b), uint8(a >> 8)
		}
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := at(x, y)
			if a < 16 {
				continue
			}
			buckets[uint32(r>>4)<<8|uint32(g>>4)<<4|uint32(b>>4)]++
		}
	}
	var dominant uint32
	var dominantCount int64
	for key, count := range buckets {
		if count > dominantCount {
			dominant, dominantCount = key, count
		}
	}
	if dominantCount == 0 {
		stats.blank = true
		return stats
	}
	dr := int(dominant>>8&0xF)<<4 + 8
	dg := int(dominant>>4&0xF)<<4 + 8
	db := int(dominant&0xF)<<4 + 8
	thickness := stats.height * renderQABorderBandPermyriad / 10000
	if stats.width < stats.height {
		thickness = stats.width * renderQABorderBandPermyriad / 10000
	}
	if thickness < 2 {
		thickness = 2
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := at(x, y)
			stats.total++
			if a < 16 {
				continue
			}
			inBand := x-bounds.Min.X < thickness || bounds.Max.X-1-x < thickness || y-bounds.Min.Y < thickness || bounds.Max.Y-1-y < thickness
			if inBand {
				stats.borderTotal++
			}
			diff := abs(int(r)-dr) + abs(int(g)-dg) + abs(int(b)-db)
			if diff <= 48 {
				continue
			}
			stats.ink++
			if inBand {
				stats.borderInk++
			}
		}
	}
	if stats.total > 0 {
		stats.inkRatio = float64(stats.ink) / float64(stats.total)
		if stats.inkRatio < renderQAInkMin {
			stats.blank = true
		}
	}
	if stats.borderTotal > 0 {
		stats.borderInkRatio = float64(stats.borderInk) / float64(stats.borderTotal)
	}
	return stats
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(parts, "、")
}
