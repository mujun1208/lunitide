package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

// OfficeStoragePolicy is read once by the composition root, before concurrent
// requests. It does not inherit a conversation's model or execution settings.
func OfficeStoragePolicy() (domain.StoragePolicy, error) {
	p := domain.DefaultStoragePolicy()
	if raw := strings.TrimSpace(os.Getenv("LUNITIDE_OFFICE_STORAGE_MAX_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 32<<20 || value > 1<<40 {
			return p, fmt.Errorf("办公存储配额必须在 32 MiB 到 1 TiB 之间")
		}
		p.MaxBytes = value
	}
	if raw := strings.TrimSpace(os.Getenv("LUNITIDE_OFFICE_STORAGE_GRACE")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value < time.Hour || value > 365*24*time.Hour {
			return p, fmt.Errorf("办公临时数据保留期必须在 1 小时到 365 天之间")
		}
		p.Grace = value
	}
	return p, nil
}
