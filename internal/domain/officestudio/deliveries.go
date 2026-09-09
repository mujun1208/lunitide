package officestudio

import (
	"context"
	"time"
)

type Metric struct {
	ID               string    `json:"id"`
	TaskID           string    `json:"taskId"`
	Name             string    `json:"name"`
	SourceVersionID  string    `json:"sourceVersionId"`
	SourceSHA256     string    `json:"sourceSha256"`
	SourceNodeID     string    `json:"sourceNodeId"`
	SourceNodeDigest string    `json:"sourceNodeDigest"`
	RawValue         string    `json:"rawValue"`
	ValueType        string    `json:"valueType"`
	Unit             string    `json:"unit"`
	Currency         string    `json:"currency"`
	Period           string    `json:"period"`
	Aggregation      string    `json:"aggregation"`
	RoundingDigits   *int      `json:"roundingDigits,omitempty"`
	RoundingPolicy   string    `json:"roundingPolicy"`
	DisplayValue     string    `json:"displayValue"`
	CreatedAt        time.Time `json:"createdAt"`
}

type BundleFile struct {
	VersionID  string `json:"versionId"`
	ArtifactID string `json:"artifactId"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	Quality    string `json:"quality"`
	Accepted   bool   `json:"accepted"`
}

type Bundle struct {
	SchemaVersion int          `json:"schemaVersion"`
	ID            string       `json:"id"`
	TaskID        string       `json:"taskId"`
	Title         string       `json:"title"`
	Files         []BundleFile `json:"files"`
	CreatedAt     time.Time    `json:"createdAt"`
}

type DeliveryStore interface {
	CreateOfficeMetric(context.Context, Metric, string) (Metric, error)
	GetOfficeMetric(context.Context, string) (Metric, error)
	ListOfficeMetrics(context.Context, string) ([]Metric, error)
	CreateOfficeBundle(context.Context, Bundle, string) (Bundle, error)
	GetOfficeBundle(context.Context, string) (Bundle, error)
	ListOfficeBundles(context.Context, string) ([]Bundle, error)
}
