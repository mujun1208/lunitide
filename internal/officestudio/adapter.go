package officestudio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/officetools"
)

type AssetRecord struct {
	SourceURL  string `json:"sourceUrl"`
	Author     string `json:"author,omitempty"`
	License    string `json:"license"`
	Digest     string `json:"digest"`
	LogoDigest string `json:"logoDigest,omitempty"`
	Commercial bool   `json:"commercial"`
}

type BrandImport struct {
	BrandID    string
	Colors     map[string]string
	Fonts      BrandFonts
	LogoDigest string
}

type ExternalAdapterStatus struct {
	Name      string
	Available bool
	Reason    string
}

func ImportBrandL1(in BrandImport, asset AssetRecord) (BrandProfile, error) {
	if strings.TrimSpace(in.BrandID) == "" || strings.TrimSpace(asset.License) == "" || len(asset.Digest) != 64 {
		return BrandProfile{}, fmt.Errorf("%w: brand import requires brandId and AssetRecord license plus digest", ErrFormat)
	}
	colors := DefaultBrand().Colors
	for k, v := range in.Colors {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			colors[k] = v
		}
	}
	fonts := DefaultBrand().Fonts
	if in.Fonts.Latin != "" {
		fonts.Latin = in.Fonts.Latin
	}
	if in.Fonts.East != "" {
		fonts.East = in.Fonts.East
	}
	if in.Fonts.WordBodyPt > 0 {
		fonts.WordBodyPt = in.Fonts.WordBodyPt
	}
	if strings.TrimSpace(in.Fonts.Fallback) != "" {
		fonts.Fallback = in.Fonts.Fallback
	}
	brand := BrandProfile{
		BrandID:    in.BrandID,
		Version:    "1",
		Colors:     colors,
		Fonts:      fonts,
		ChartTheme: DefaultBrand().ChartTheme,
		Logo:       DefaultBrand().Logo,
	}
	if err := RegisterBrand(brand); err != nil {
		return BrandProfile{}, err
	}
	return brand, nil
}

func ProbeExternalAdapter(name, executable string) ExternalAdapterStatus {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "external"
	}
	if strings.TrimSpace(executable) == "" {
		return ExternalAdapterStatus{Name: name, Reason: "未配置外部生成器，结果不能绕过交付门槛"}
	}
	if _, err := os.Stat(executable); err != nil {
		return ExternalAdapterStatus{Name: name, Reason: "未检测到 " + name + " 进程，检查标为 missing"}
	}
	return ExternalAdapterStatus{Name: name, Available: true, Reason: "已检测到工作进程，仍须写入同一 QualityReport"}
}

func QualityFromExternalAdapter(st ExternalAdapterStatus) QualityReport {
	if st.Available {
		return EvaluateQuality([]Check{{ID: "external_adapter", Status: "passed", Message: st.Reason}}, 0, nil)
	}
	return EvaluateQuality([]Check{{ID: "external_adapter", Status: "missing", Message: st.Reason}}, 0, nil)
}

func ProbePresenton() ExternalAdapterStatus {
	return ProbeExternalAdapter("presenton", strings.TrimSpace(os.Getenv("LUNITIDE_PRESENTON")))
}

func ProbePptxGenJS() ExternalAdapterStatus {
	return ProbeExternalAdapter("pptxgenjs", strings.TrimSpace(os.Getenv("LUNITIDE_PPTXGENJS")))
}

func ProbeTypst(executable string) ExternalAdapterStatus {
	st := ProbeExternalAdapter("typst", executable)
	if !st.Available {
		st.Reason = "未检测到 Typst，独立 PDF 不可用；不得标为已验证"
	}
	return st
}

func TypstExecutable() string {
	return strings.TrimSpace(os.Getenv("LUNITIDE_TYPST"))
}

func IndependentPDFCheck() Check {
	st := ProbeTypst(TypstExecutable())
	if st.Available {
		return Check{ID: "independent_pdf", Status: "passed", Message: st.Reason}
	}
	return Check{ID: "independent_pdf", Status: "missing", Message: st.Reason}
}

func IndependentPDFNotice() string {
	return "独立 PDF 与 Word 使用同一内容版本，但不保证分页与 Word 像素一致。导出 PDF 不表示 PDF/A 或 PDF/UA 合规。"
}

func IndependentPDFACheck() Check {
	return Check{ID: "pdfa", Status: "unsupported", Message: "导出 PDF 不表示 PDF/A 或 PDF/UA 合规"}
}

func FormatIndependentReport(title, audience, purpose, body string, citations []string) string {
	var b strings.Builder
	b.WriteString("封面\n")
	if strings.TrimSpace(title) != "" {
		b.WriteString(strings.TrimSpace(title) + "\n")
	}
	if strings.TrimSpace(audience) != "" {
		b.WriteString("受众：" + strings.TrimSpace(audience) + "\n")
	}
	if strings.TrimSpace(purpose) != "" {
		b.WriteString("用途：" + strings.TrimSpace(purpose) + "\n")
	}
	b.WriteString("\n目录\n1. 正文\n")
	if len(citations) > 0 {
		b.WriteString("2. 引用\n")
	}
	b.WriteString("\n正文\n")
	b.WriteString(body)
	if len(citations) > 0 {
		b.WriteString("\n\n引用\n")
		for _, item := range citations {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			b.WriteString("- " + item + "\n")
		}
	}
	return b.String()
}

func factCitations(facts []Fact) []string {
	var out []string
	seen := map[string]bool{}
	for _, fact := range facts {
		item := firstNonEmpty(fact.SourceID, fact.Locator)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func ExportNotice(kind Kind, sameSource bool) string {
	if kind == PDF && !sameSource {
		return IndependentPDFNotice()
	}
	return "已导出可编辑副本；原始存档与接受状态保持不变"
}

func independentPDFInput(spec Spec) (title, body string, theme Theme) {
	theme = ResolveTheme(brandForSpec(spec))
	body = FormatIndependentReport(spec.Title, spec.Audience, spec.Purpose, spec.Body, factCitations(spec.Facts))
	if conf := strings.TrimSpace(spec.Confidentiality); conf != "" {
		if strings.HasPrefix(body, "封面\n") {
			body = "封面\n密级：" + conf + "\n" + strings.TrimPrefix(body, "封面\n")
		} else {
			body = "密级：" + conf + "\n" + body
		}
	}
	return spec.Title, body, theme
}

func RenderIndependentPDF(workDir, title, body string) ([]byte, Check, error) {
	return RenderIndependentPDFWithTheme(workDir, title, body, Theme{})
}

func RenderIndependentPDFWithTheme(workDir, title, body string, theme Theme) ([]byte, Check, error) {
	check := IndependentPDFCheck()
	if check.Status == "passed" {
		pdf, err := runTypst(workDir, title, body, TypstExecutable(), theme)
		if err == nil && len(pdf) > 0 {
			return pdf, check, nil
		}
		check = Check{ID: "independent_pdf", Status: "missing", Message: "Typst 已配置但未能生成独立 PDF；不得标为已验证"}
	}
	data, err := officetools.GenStablePDFThemed(title, body, officetools.PDFTheme{Heading: theme.Navy, Body: theme.Ink})
	return data, check, err
}

func runTypst(workDir, title, body, executable string, theme Theme) ([]byte, error) {
	if strings.TrimSpace(workDir) == "" || strings.TrimSpace(executable) == "" {
		return nil, fmt.Errorf("%w: typst workdir", ErrFormat)
	}
	src := filepath.Join(workDir, "independent.typ")
	out := filepath.Join(workDir, "independent.pdf")
	markup := typstMarkupWithBrand(title, body, theme)
	if err := os.WriteFile(src, []byte(markup), 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, "compile", src, out)
	cmd.Dir = workDir
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

func typstMarkup(title, body string) string {
	return typstMarkupWithBrand(title, body, Theme{})
}

func typstMarkupWithBrand(title, body string, theme Theme) string {
	var b strings.Builder
	b.WriteString("#set page(paper: \"a4\")\n")
	if fonts := typstFontList(theme); fonts != "" {
		b.WriteString("#set text(font: " + fonts + ")\n")
	}
	if strings.HasPrefix(strings.TrimSpace(body), "封面") {
		for _, line := range strings.Split(body, "\n") {
			label := strings.TrimSpace(line)
			switch label {
			case "封面", "目录", "正文", "引用":
				b.WriteString("= " + typstEscape(label) + "\n")
			default:
				if label != "" {
					b.WriteString(typstEscape(line) + "\n")
				} else {
					b.WriteString("\n")
				}
			}
		}
		return b.String()
	}
	if strings.TrimSpace(title) != "" {
		b.WriteString("= " + typstEscape(title) + "\n\n")
	}
	b.WriteString(typstEscape(body) + "\n")
	return b.String()
}

func typstFontList(theme Theme) string {
	seen := map[string]bool{}
	var names []string
	for _, name := range append([]string{theme.East, theme.Latin}, strings.Split(theme.Fallback, ",")...) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, "\""+typstEscape(name)+"\"")
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	return "(" + strings.Join(names, ", ") + ")"
}

func typstEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "[", "\\[")
	s = strings.ReplaceAll(s, "]", "\\]")
	s = strings.ReplaceAll(s, "#", "\\#")
	return s
}
