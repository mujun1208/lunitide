//go:build windows

package toolruntime

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const locationScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Device
$w = New-Object System.Device.Location.GeoCoordinateWatcher
$w.Start()
$deadline = (Get-Date).AddSeconds(8)
while ((Get-Date) -lt $deadline) {
  if ($w.Permission -eq 'Denied') { 'LOCATION_PERMISSION'; exit 2 }
  if ($w.Status -eq 'Ready' -and -not $w.Position.Location.IsUnknown) { break }
  Start-Sleep -Milliseconds 250
}
$loc = $w.Position.Location
if ($null -eq $loc -or $loc.IsUnknown) { 'LOCATION_UNKNOWN'; exit 2 }
$inv = [Globalization.CultureInfo]::InvariantCulture
$tz = [TimeZoneInfo]::Local.Id
'{0}|{1}|{2}|{3}' -f $loc.Latitude.ToString($inv), $loc.Longitude.ToString($inv), $loc.HorizontalAccuracy.ToString($inv), $tz
`

func ReadLocation(ctx context.Context) (LocationFix, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", locationScript)
	out, err := cmd.CombinedOutput()
	line := lastNonEmpty(string(out))
	if fix, parseErr := parseLocationLine(line); parseErr == nil {
		return fix, nil
	} else if line == "LOCATION_PERMISSION" || line == "LOCATION_UNKNOWN" || strings.Contains(line, "LOCATION_") {
		return LocationFix{}, parseErr
	}
	if err != nil && line == "" {
		return LocationFix{}, err
	}
	return parseLocationLine(line)
}

func lastNonEmpty(text string) string {
	var last string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			last = line
		}
	}
	return last
}
