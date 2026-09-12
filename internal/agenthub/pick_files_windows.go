//go:build windows

package agenthub

import (
	"context"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

func pickFilesOS() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", pickFilesScript).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrPickCanceled
		}
		return nil, err
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, ErrPickCanceled
	}
	var files []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	if len(files) == 0 {
		return nil, ErrPickCanceled
	}
	return files, nil
}

func pickFolderOS() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", pickInboxFolderScript).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrPickCanceled
		}
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrPickCanceled
	}
	return path, nil
}

const pickFilesScript = `
Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form
$form.TopMost = $true
$form.ShowInTaskbar = $false
$form.StartPosition = 'Manual'
$form.Location = New-Object System.Drawing.Point(-32000, -32000)
$form.Size = New-Object System.Drawing.Size(1, 1)
$form.Show()
$form.Activate()
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = '选择要交给 Agent 的文件'
$d.Filter = '所有文件|*.*'
$d.Multiselect = $true
$ok = $d.ShowDialog($form) -eq 'OK'
$names = $d.FileNames
$form.Close()
if ($ok) {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $names -join [Environment]::NewLine
}
`

const pickInboxFolderScript = `
Add-Type -AssemblyName System.Windows.Forms
$form = New-Object System.Windows.Forms.Form
$form.TopMost = $true
$form.ShowInTaskbar = $false
$form.StartPosition = 'Manual'
$form.Location = New-Object System.Drawing.Point(-32000, -32000)
$form.Size = New-Object System.Drawing.Size(1, 1)
$form.Show()
$form.Activate()
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '选择要交给 Agent 的资料夹'
$d.ShowNewFolderButton = $false
$ok = $d.ShowDialog($form) -eq 'OK'
$path = $d.SelectedPath
$form.Close()
if ($ok) {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $path
}
`
