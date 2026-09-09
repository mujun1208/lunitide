//go:build !windows

package officerender

func ProbeDesktopApplications() []DesktopApplication {
	return nil
}
