//go:build windows

package desktopfiles

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

func pickOSForms(folder, multiple bool) ([]Item, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	script := openFilesScript
	if folder {
		script = folderScript
	} else if !multiple {
		script = openFileScript
	}
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		if os.IsTimeout(err) {
			return nil, nil, ErrUnavailable
		}
		return nil, nil, ErrUnavailable
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil, nil, ErrCanceled
	}
	if folder {
		return listFolder(text)
	}
	var items []Item
	for _, line := range strings.Split(text, "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		item, err := itemFromPath(path)
		if err != nil {
			continue
		}
		items = append(items, item)
		if len(items) >= maxItems {
			break
		}
	}
	if len(items) == 0 {
		return nil, nil, ErrCanceled
	}
	return items, nil, nil
}

const openFileScript = `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = '选择要附加的文件'
$d.Filter = '支持的文件|*.txt;*.md;*.json;*.csv;*.html;*.xml;*.js;*.ts;*.py;*.go;*.java;*.c;*.cpp;*.rs;*.yaml;*.yml;*.sh;*.sql;*.png;*.jpg;*.jpeg;*.webp|所有文件|*.*'
$d.CheckFileExists = $true
$d.Multiselect = $false
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.FileName
}
`

const openFilesScript = `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = '选择要附加的文件'
$d.Filter = '支持的文件|*.txt;*.md;*.json;*.csv;*.html;*.xml;*.js;*.ts;*.py;*.go;*.java;*.c;*.cpp;*.rs;*.yaml;*.yml;*.sh;*.sql;*.png;*.jpg;*.jpeg;*.webp|所有文件|*.*'
$d.CheckFileExists = $true
$d.Multiselect = $true
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.FileNames -join [Environment]::NewLine
}
`

const folderScript = `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '选择要导入的文件夹'
$d.ShowNewFolderButton = $true
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.SelectedPath
}
`
