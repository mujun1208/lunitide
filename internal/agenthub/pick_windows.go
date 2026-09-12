//go:build windows

package agenthub

import (
	"context"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/winexec"
)

func pickWorkDirOS() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", pickDirScript).Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ErrPickCanceled
		}
		legacy := `$s=(New-Object -ComObject Shell.Application).BrowseForFolder(0,'选择 Agent 调度台工作目录',0,0);if($s){[Console]::OutputEncoding=[Text.Encoding]::UTF8;$s.Self.Path}`
		out, err = winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", legacy).Output()
		if err != nil {
			if ctx.Err() != nil {
				return "", ErrPickCanceled
			}
			return "", err
		}
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", ErrPickCanceled
	}
	return path, nil
}

const pickDirScript = `
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
$d.Description = '选择 Agent 调度台工作目录'
$d.ShowNewFolderButton = $true
$ok = $d.ShowDialog($form) -eq 'OK'
$path = $d.SelectedPath
$form.Close()
if ($ok) {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $path
}
`
