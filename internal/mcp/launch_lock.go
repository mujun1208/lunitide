package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var ErrLaunchLock = errors.New("mcp: launcher needs a resolvable package with an exact version")
var npmName = regexp.MustCompile(`^(?:@[a-z0-9._-]+/)?[a-z0-9][a-z0-9._-]*$`)
var npmVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var pythonName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var pythonVersion = regexp.MustCompile(`^[0-9][0-9A-Za-z._+-]{0,63}$`)

type PackageMetadataFetcher func(context.Context, string) ([]byte, error)

// ResolveLaunchArgs locks existing bare/tagged npx and uvx presets before any
// executable starts. A later startup reuses these exact args without resolving
// latest again. Unsupported launcher flags are reported explicitly.
func ResolveLaunchArgs(ctx context.Context, command string, args []string, fetch PackageMetadataFetcher) ([]string, error) {
	out := append([]string(nil), args...)
	if command == "node" {
		return out, nil
	}
	if command == "uvx" && len(out) >= 3 && out[0] == "--from" {
		locked, err := lockPythonPackage(ctx, out[1], fetch)
		if err != nil {
			return nil, err
		}
		out[1] = locked
		for i := 2; i < len(out); i++ {
			if strings.HasPrefix(out[i], "-") || !pythonName.MatchString(out[i]) {
				return nil, ErrLaunchLock
			}
		}
		return out, nil
	}
	index := 0
	for index < len(out) && strings.HasPrefix(out[index], "-") {
		if command == "npx" && (out[index] == "-y" || out[index] == "--yes") {
			index++
			continue
		}
		return nil, ErrLaunchLock
	}
	if index >= len(out) {
		return nil, ErrLaunchLock
	}
	spec := out[index]
	switch command {
	case "npx":
		name, version := spec, ""
		if at := strings.LastIndex(spec, "@"); at > 0 {
			name, version = spec[:at], spec[at+1:]
		}
		if !npmName.MatchString(name) {
			return nil, ErrLaunchLock
		}
		if npmVersion.MatchString(version) {
			return out, nil
		}
		if version == "" {
			version = "latest"
		}
		if !regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`).MatchString(version) || fetch == nil {
			return nil, ErrLaunchLock
		}
		data, err := fetch(ctx, "https://registry.npmjs.org/"+url.PathEscape(name)+"/"+url.PathEscape(version))
		if err != nil {
			return nil, err
		}
		var info struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if len(data) > 1<<20 || json.Unmarshal(data, &info) != nil || info.Name != name || !npmVersion.MatchString(info.Version) {
			return nil, ErrLaunchLock
		}
		out[index] = name + "@" + info.Version
	case "uvx":
		locked, err := lockPythonPackage(ctx, spec, fetch)
		if err != nil {
			return nil, err
		}
		out[index] = locked
	default:
		return nil, ErrLaunchLock
	}
	return out, nil
}

func lockPythonPackage(ctx context.Context, spec string, fetch PackageMetadataFetcher) (string, error) {
	name, version, locked := strings.Cut(spec, "==")
	if !pythonName.MatchString(name) {
		return "", ErrLaunchLock
	}
	if locked {
		if !pythonVersion.MatchString(version) {
			return "", ErrLaunchLock
		}
		return spec, nil
	}
	if fetch == nil {
		return "", ErrLaunchLock
	}
	data, err := fetch(ctx, "https://pypi.org/pypi/"+url.PathEscape(name)+"/json")
	if err != nil {
		return "", err
	}
	var info struct {
		Info struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"info"`
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &info) != nil || !strings.EqualFold(strings.ReplaceAll(info.Info.Name, "_", "-"), strings.ReplaceAll(name, "_", "-")) || !pythonVersion.MatchString(info.Info.Version) {
		return "", ErrLaunchLock
	}
	return name + "==" + info.Info.Version, nil
}
