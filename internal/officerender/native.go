package officerender

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

// NativeOptions applies only to the loaded private copy used for PDF export.
// Updating an index or calculating in LibreOffice does not rewrite the source
// OOXML and is not evidence of Microsoft Office/WPS compatibility.
type NativeOptions struct {
	UpdateFields      bool `json:"updateFields"`
	Recalculate       bool `json:"recalculate"`
	ExportUpdatedCopy bool `json:"-"`
}

type FormulaError struct {
	SheetIndex int `json:"sheetIndex"` // Zero based; row and column are one based.
	Row        int `json:"row"`
	Column     int `json:"column"`
	Code       int `json:"code"`
}

type NativeEvidence struct {
	Scope             string         `json:"scope"`
	SourceUnchanged   bool           `json:"sourceUnchanged"`
	FieldsRefreshed   bool           `json:"fieldsRefreshed"`
	UpdatedIndexes    int            `json:"updatedIndexes"`
	Recalculated      bool           `json:"recalculated"`
	FormulaCells      int            `json:"formulaCells"` // Inspected cells, at most 50,000.
	FormulaErrorCount int            `json:"formulaErrorCount"`
	FormulaErrors     []FormulaError `json:"formulaErrors"`
	FormulaScanFull   bool           `json:"formulaScanFull"`
	ErrorsTruncated   bool           `json:"errorsTruncated"`
	Notice            string         `json:"notice"`
}

// RenderWithChecks runs fixed, application-owned UNO commands from a private
// Basic library. Document macros remain disabled. No user text is inserted into
// executable code; only owned file URLs, a digest, and fixed format constants.
func (r *Renderer) RenderWithChecks(ctx context.Context, kind string, data []byte, options NativeOptions) (Result, error) {
	if !options.UpdateFields && !options.Recalculate {
		return r.Render(ctx, kind, data)
	}
	if (options.UpdateFields && kind != "docx") || (options.Recalculate && kind != "xlsx") {
		return Result{}, errors.New("目录与域更新仅支持 Word，公式重算仅支持 Excel")
	}
	if len(data) == 0 || len(data) > 32<<20 {
		return Result{}, errors.New("排版输入为空或超过 32 MiB")
	}
	if r.Preflight == nil {
		return Result{}, errors.New("排版安全预检未初始化")
	}
	if err := r.Preflight(kind, data); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	c := r.Probe(ctx)
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !c.Available {
		return Result{}, fmt.Errorf("%w：%s", ErrUnavailable, c.Notice)
	}
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return Result{}, err
	}
	job, err := os.MkdirTemp(root, "native-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(job) // Only the private directory returned by MkdirTemp.
	profile := filepath.Join(job, "profile")
	profileURL := nativeFileURL(profile)
	input := filepath.Join(job, "source."+kind)
	if err = os.WriteFile(input, data, 0600); err != nil {
		return Result{}, err
	}
	// First initialize an entirely new profile. Keeping LibreOffice's startup
	// flags avoids the first-run wizard preventing our fixed library from loading.
	common := []string{"-env:UserInstallation=" + profileURL, "--headless", "--nologo", "--nodefault", "--norestore"}
	run := func(args []string, timeout time.Duration) error {
		out, runErr := r.runner()(ctx, commandworker.Spec{Exe: r.executable(), Args: args, Dir: job, Env: environment(job), Timeout: timeout, MaxOutputBytes: 16384, MaxMemoryBytes: 1 << 30}, nil, nil)
		if runErr != nil {
			return runErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if out.TimedOut {
			return errors.New("目录更新或公式重算超时，原文件已保留")
		}
		if out.ExitCode != 0 || out.Truncated {
			return errors.New("原生更新工作进程失败，不能视为检查通过")
		}
		return nil
	}
	if err = run(append(append([]string{}, common...), "--terminate_after_init"), 30*time.Second); err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(data)
	sourceDigest := hex.EncodeToString(sum[:])
	if err = installNativeLibrary(profile, job, kind, sourceDigest, options.ExportUpdatedCopy); err != nil {
		return Result{}, err
	}
	if err = run(append(append([]string{}, common...), "macro:///Standard.Module1.Main"), 90*time.Second); err != nil {
		return Result{}, err
	}
	if _, err = os.Stat(filepath.Join(job, "error.txt")); err == nil {
		return Result{}, errors.New("原生目录更新或公式重算失败，原文件已保留")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	receipt, err := readNativeFile(filepath.Join(job, "receipt.txt"), 16384)
	if err != nil {
		return Result{}, errors.New("未取得完整的原生更新回执，不能视为检查通过")
	}
	evidence, err := parseNativeReceipt(receipt, kind, sourceDigest)
	if err != nil {
		return Result{}, err
	}
	copyData, err := readNativeFile(input, 32<<20)
	if err != nil || !bytes.Equal(data, copyData) {
		return Result{}, errors.New("原生工作进程改写了输入快照，检查结果已拒绝")
	}
	pdf, err := readNativeFile(filepath.Join(job, "source.pdf"), 32<<20)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf[max(0, len(pdf)-1024):], []byte("%%EOF")) {
		return Result{}, errors.New("原生更新未产生完整 PDF，不能视为检查通过")
	}
	var updated []byte
	if options.ExportUpdatedCopy {
		updated, err = readNativeFile(filepath.Join(job, "updated."+kind), 32<<20)
		if err != nil || len(updated) == 0 {
			return Result{}, errors.New("原生更新未生成可核对的 Office 副本")
		}
		if err = r.Preflight(kind, updated); err != nil {
			return Result{}, err
		}
	}
	h := sha256.Sum256(pdf)
	return Result{PDF: pdf, UpdatedOffice: updated, SHA256: hex.EncodeToString(h[:]), Renderer: c.Name, RendererVersion: c.Version, SourceDigest: sourceDigest, Native: &evidence, Notice: evidence.Notice}, nil
}

func readNativeFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("原生排版文件读取失败或超过限额")
	}
	return data, nil
}

func nativeFileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func installNativeLibrary(profile, job, kind, digest string, exportUpdated ...bool) error {
	registry := filepath.Join(profile, "user", "registrymodifications.xcu")
	settings, err := readNativeFile(registry, 1<<20)
	if err != nil || bytes.Count(settings, []byte("</oor:items>")) != 1 {
		return errors.New("隔离排版配置初始化失败")
	}
	security := `<item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item>`
	settings = bytes.Replace(settings, []byte("</oor:items>"), []byte(security+"</oor:items>"), 1)
	if err = os.WriteFile(registry, settings, 0600); err != nil {
		return err
	}
	basic := filepath.Join(profile, "user", "basic")
	if err = os.MkdirAll(filepath.Join(basic, "Standard"), 0700); err != nil {
		return err
	}
	var escaped bytes.Buffer
	if err = xml.EscapeText(&escaped, []byte(nativeBasic(job, kind, digest, exportUpdated...))); err != nil {
		return err
	}
	files := map[string]string{
		"script.xlc":           `<?xml version="1.0" encoding="UTF-8"?><library:libraries xmlns:library="http://openoffice.org/2000/library" xmlns:xlink="http://www.w3.org/1999/xlink"><library:library library:name="Standard" library:link="false"/></library:libraries>`,
		"Standard/script.xlb":  `<?xml version="1.0" encoding="UTF-8"?><library:library xmlns:library="http://openoffice.org/2000/library" library:name="Standard" library:readonly="false" library:passwordprotected="false"><library:element library:name="Module1"/></library:library>`,
		"Standard/Module1.xba": `<?xml version="1.0" encoding="UTF-8"?><script:module xmlns:script="http://openoffice.org/2000/script" script:name="Module1" script:language="StarBasic">` + escaped.String() + `</script:module>`,
	}
	for name, content := range files {
		if err = os.WriteFile(filepath.Join(basic, filepath.FromSlash(name)), []byte(content), 0600); err != nil {
			return err
		}
	}
	return nil
}

func nativeBasic(job, kind, digest string, exportUpdated ...bool) string {
	filter := "writer_pdf_Export"
	action := `indexes=doc.getDocumentIndexes()
 indexCount=indexes.getCount()
 For i=0 To indexCount-1
   indexes.getByIndex(i).update()
 Next i
 doc.getTextFields().refresh()
 For i=0 To indexCount-1
   indexes.getByIndex(i).update()
 Next i
 fields=1`
	if kind == "xlsx" {
		filter = "calc_pdf_Export"
		action = `doc.calculateAll()
 recalculated=1
 scanFull=1
 For i=0 To doc.getSheets().getCount()-1
   sheet=doc.getSheets().getByIndex(i)
   cells=sheet.queryContentCells(16).getCells()
   enumeration=cells.createEnumeration()
   Do While enumeration.hasMoreElements()
     If formulaCount>=50000 Then
       scanFull=0
       Exit Do
     End If
     cell=enumeration.nextElement()
     formulaCount=formulaCount+1
     cellError=cell.getError()
     If cellError<>0 Then
       errorCount=errorCount+1
       If errorCount<=100 Then
         address=cell.getCellAddress()
         details=details & "error=" & CStr(i) & "," & CStr(address.Row+1) & "," & CStr(address.Column+1) & "," & CStr(cellError) & Chr(10)
       End If
     End If
   Loop
   If scanFull=0 Then Exit For
 Next i`
	}
	copyCommand := ""
	if len(exportUpdated) > 0 && exportUpdated[0] {
		copyFilter := "Office Open XML Text"
		if kind == "xlsx" {
			copyFilter = "Calc MS Excel 2007 XML"
		}
		copyCommand = `op(0).Value="` + copyFilter + `"
 doc.storeToURL("` + nativeFileURL(filepath.Join(job, "updated."+kind)) + `",op())`
	}
	return `Sub Main
 On Local Error GoTo Failed
 Dim doc As Object, indexes As Object, sheet As Object, cells As Object, enumeration As Object, cell As Object, address As Object
 Dim i As Long, indexCount As Long, fields As Long, recalculated As Long, formulaCount As Long, errorCount As Long, scanFull As Long, cellError As Long
 Dim details As String
 fields=0: indexCount=0: recalculated=0: formulaCount=0: errorCount=0: scanFull=0: details=""
 Dim p(3) As New com.sun.star.beans.PropertyValue
 p(0).Name="Hidden": p(0).Value=True
 p(1).Name="MacroExecutionMode": p(1).Value=0
 p(2).Name="UpdateDocMode": p(2).Value=0
 p(3).Name="ReadOnly": p(3).Value=False
 doc=StarDesktop.loadComponentFromURL("` + nativeFileURL(filepath.Join(job, "source."+kind)) + `","_blank",0,p())
 ` + action + `
 Dim op(1) As New com.sun.star.beans.PropertyValue
 op(0).Name="FilterName": op(0).Value="` + filter + `"
 op(1).Name="Overwrite": op(1).Value=True
 doc.storeToURL("` + nativeFileURL(filepath.Join(job, "source.pdf")) + `",op())
 ` + copyCommand + `
 doc.setModified(False)
 doc.close(True)
 Open ConvertFromURL("` + nativeFileURL(filepath.Join(job, "receipt.txt")) + `") For Output As #1
 Print #1, "office-native-v1"
 Print #1, "sourceSha256=` + digest + `"
 Print #1, "kind=` + kind + `"
 Print #1, "fieldsRefreshed=" & CStr(fields)
 Print #1, "updatedIndexes=" & CStr(indexCount)
 Print #1, "recalculated=" & CStr(recalculated)
 Print #1, "formulaCells=" & CStr(formulaCount)
 Print #1, "formulaErrorCount=" & CStr(errorCount)
 Print #1, "formulaScanFull=" & CStr(scanFull)
 Print #1, details;
 Print #1, "done=1"
 Close #1
 StarDesktop.terminate()
 Exit Sub
 Failed:
 On Local Error Resume Next
 Close #1
 Open ConvertFromURL("` + nativeFileURL(filepath.Join(job, "error.txt")) + `") For Output As #2
 Print #2, "native-update-failed"
 Close #2
 doc.setModified(False)
 doc.close(True)
 StarDesktop.terminate()
End Sub`
}

func parseNativeReceipt(data []byte, kind, digest string) (NativeEvidence, error) {
	bad := errors.New("原生更新回执不完整或与输入不一致")
	e := NativeEvidence{Scope: "derived-preview", SourceUnchanged: true, FormulaErrors: []FormulaError{}, Notice: "已在隔离的 LibreOffice 中更新 PDF 预览；输入原文件未改写，原文件域缓存、其他软件排版和公式语义兼容性仍需独立验证"}
	if len(data) == 0 || len(data) > 16384 {
		return NativeEvidence{}, bad
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if lines[0] != "office-native-v1" {
		return NativeEvidence{}, bad
	}
	values := make(map[string]string)
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return NativeEvidence{}, bad
		}
		if key == "error" {
			if len(e.FormulaErrors) >= 100 {
				return NativeEvidence{}, bad
			}
			parts := strings.Split(value, ",")
			if len(parts) != 4 {
				return NativeEvidence{}, bad
			}
			n := [4]int{}
			for i, p := range parts {
				x, err := strconv.Atoi(p)
				if err != nil || x < 0 || x > 1048576 {
					return NativeEvidence{}, bad
				}
				n[i] = x
			}
			if n[1] == 0 || n[2] == 0 || n[2] > 16384 || n[3] == 0 {
				return NativeEvidence{}, bad
			}
			e.FormulaErrors = append(e.FormulaErrors, FormulaError{SheetIndex: n[0], Row: n[1], Column: n[2], Code: n[3]})
			continue
		}
		if _, exists := values[key]; exists {
			return NativeEvidence{}, bad
		}
		values[key] = value
	}
	if len(values) != 9 || values["sourceSha256"] != digest || values["kind"] != kind || values["done"] != "1" {
		return NativeEvidence{}, bad
	}
	numbers := make(map[string]int)
	for _, key := range []string{"fieldsRefreshed", "updatedIndexes", "recalculated", "formulaCells", "formulaErrorCount", "formulaScanFull"} {
		n, err := strconv.Atoi(values[key])
		if err != nil || n < 0 || n > 50000 {
			return NativeEvidence{}, bad
		}
		numbers[key] = n
	}
	if numbers["fieldsRefreshed"] > 1 || numbers["recalculated"] > 1 || numbers["formulaScanFull"] > 1 || numbers["formulaErrorCount"] > numbers["formulaCells"] || len(e.FormulaErrors) != min(100, numbers["formulaErrorCount"]) {
		return NativeEvidence{}, bad
	}
	if kind == "docx" && (numbers["fieldsRefreshed"] != 1 || numbers["recalculated"] != 0 || numbers["formulaCells"] != 0 || numbers["formulaScanFull"] != 0) {
		return NativeEvidence{}, bad
	}
	if kind == "xlsx" && (numbers["recalculated"] != 1 || numbers["fieldsRefreshed"] != 0 || numbers["updatedIndexes"] != 0 || (numbers["formulaScanFull"] == 0 && numbers["formulaCells"] != 50000)) {
		return NativeEvidence{}, bad
	}
	e.FieldsRefreshed, e.UpdatedIndexes = numbers["fieldsRefreshed"] == 1, numbers["updatedIndexes"]
	e.Recalculated, e.FormulaCells = numbers["recalculated"] == 1, numbers["formulaCells"]
	e.FormulaErrorCount, e.FormulaScanFull = numbers["formulaErrorCount"], numbers["formulaScanFull"] == 1
	e.ErrorsTruncated = e.FormulaErrorCount > len(e.FormulaErrors)
	return e, nil
}
