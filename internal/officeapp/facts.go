package officeapp

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

type MetricCapture struct {
	SourceVersionID  string `json:"sourceVersionId"`
	SourceNodeID     string `json:"sourceNodeId"`
	SourceNodeDigest string `json:"sourceNodeDigest"`
	FactID           string `json:"factId,omitempty"`
	Name             string `json:"name"`
	Unit             string `json:"unit"`
	Currency         string `json:"currency"`
	Period           string `json:"period"`
	RoundingDigits   *int   `json:"roundingDigits,omitempty"`
}

type MetricApply struct {
	MetricID         string `json:"metricId"`
	TargetVersionID  string `json:"targetVersionId"`
	TargetNodeID     string `json:"targetNodeId"`
	TargetNodeDigest string `json:"targetNodeDigest"`
	Template         string `json:"template"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (s *Service) deliveries() (domain.DeliveryStore, error) {
	d, ok := s.Store.(domain.DeliveryStore)
	if !ok {
		return nil, errors.New("办公指标与成套交付存储尚未就绪")
	}
	return d, nil
}

var metricNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]{1,3})?$`)

// CaptureMetric reads the exact node value. It never accepts a model-supplied
// raw value or treats a formula's old cached result as fresh calculation.
func (s *Service) CaptureMetric(ctx context.Context, taskID string, r MetricCapture, key string) (domain.Metric, error) {
	d, err := s.deliveries()
	if err != nil {
		return domain.Metric{}, err
	}
	v, b, err := s.ReadVersion(ctx, taskID, r.SourceVersionID)
	if err != nil {
		return domain.Metric{}, err
	}
	if len(r.Name) > 256 || len(r.Unit) > 64 || len(r.Period) > 128 || len(r.Currency) > 3 || r.SourceNodeID == "" || len(r.SourceNodeDigest) != 64 {
		return domain.Metric{}, domain.ErrInvalid
	}
	if r.Currency != "" {
		if len(r.Currency) != 3 {
			return domain.Metric{}, domain.ErrInvalid
		}
		for _, c := range r.Currency {
			if c < 'A' || c > 'Z' {
				return domain.Metric{}, domain.ErrInvalid
			}
		}
	}
	i, err := content.Inspect(content.Kind(v.Kind), b)
	if err != nil {
		return domain.Metric{}, err
	}
	var node *content.Node
	for n := range i.Nodes {
		if i.Nodes[n].ID == r.SourceNodeID {
			node = &i.Nodes[n]
			break
		}
	}
	if node == nil || node.Digest != r.SourceNodeDigest {
		return domain.Metric{}, domain.ErrConflict
	}
	valueType := strings.TrimPrefix(node.Kind, "cell:")
	if valueType == "formula" || valueType == "error" || valueType == "invalid" {
		return domain.Metric{}, errors.New("该节点是公式或错误值，未证明已重算，不能作为已核实指标")
	}
	if v.Quality == "blocked" || v.Quality == "stale" {
		return domain.Metric{}, errors.New("来源检查阻断或已过期，请先核对来源")
	}
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = node.Locator
	}
	if name == "" {
		name = "指标"
	}
	m := domain.Metric{TaskID: taskID, Name: name, SourceVersionID: v.ID, SourceSHA256: v.SHA256, SourceNodeID: node.ID, SourceNodeDigest: node.Digest, RawValue: node.Text, ValueType: valueType, Unit: r.Unit, Currency: r.Currency, Period: r.Period, Aggregation: "identity", DisplayValue: node.Text, RoundingDigits: r.RoundingDigits, RoundingPolicy: "none"}
	if r.RoundingDigits != nil {
		if valueType != "number" || *r.RoundingDigits < 0 || *r.RoundingDigits > 12 || len(node.Text) > 128 || !metricNumber.MatchString(node.Text) {
			return m, domain.ErrInvalid
		}
		if exp := strings.IndexAny(node.Text, "eE"); exp >= 0 {
			n, e := strconv.Atoi(node.Text[exp+1:])
			if e != nil || n > 308 || n < -308 {
				return m, domain.ErrInvalid
			}
		}
		value, ok := new(big.Rat).SetString(node.Text)
		if !ok {
			return m, domain.ErrInvalid
		}
		m.DisplayValue = value.FloatString(*r.RoundingDigits)
		m.RoundingPolicy = "half_away_from_zero"
	}
	return d.CreateOfficeMetric(ctx, m, key)
}

func (s *Service) CaptureMetricByFact(ctx context.Context, taskID, versionID string, fact content.Fact, key string) (domain.Metric, error) {
	if strings.TrimSpace(fact.FactID) == "" || strings.TrimSpace(fact.Value) == "" {
		return domain.Metric{}, domain.ErrInvalid
	}
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return domain.Metric{}, err
	}
	i, err := content.Inspect(content.Kind(v.Kind), b)
	if err != nil {
		return domain.Metric{}, err
	}
	node, ok := content.LocateFactNode(i, fact)
	if !ok {
		return domain.Metric{}, domain.ErrNotFound
	}
	name := fact.FactID
	if fact.Locator != "" {
		name = fact.Locator
	}
	return s.CaptureMetric(ctx, taskID, MetricCapture{
		SourceVersionID: versionID, SourceNodeID: node.ID, SourceNodeDigest: node.Digest,
		FactID: fact.FactID, Name: name, Unit: fact.Unit, Period: fact.Period,
	}, key)
}

func (s *Service) AssertTaskFactSet(ctx context.Context, taskID string, facts []content.Fact, versionIDs []string) error {
	inspections := make([]content.Inspection, 0, len(versionIDs))
	for _, id := range versionIDs {
		v, b, err := s.ReadVersion(ctx, taskID, id)
		if err != nil {
			return err
		}
		i, err := content.Inspect(content.Kind(v.Kind), b)
		if err != nil {
			return err
		}
		inspections = append(inspections, i)
	}
	return content.AssertFactSetCoverage(facts, inspections)
}

func (s *Service) ListMetrics(ctx context.Context, taskID string) ([]domain.Metric, error) {
	d, err := s.deliveries()
	if err != nil {
		return nil, err
	}
	return d.ListOfficeMetrics(ctx, taskID)
}

// ApplyMetric substitutes one verified metric in an explicit target node.
// The new file and its source edge are committed together; accepted pointers
// remain untouched and concurrent upstream changes reject publication.
func (s *Service) ApplyMetric(ctx context.Context, taskID string, r MetricApply, key string) (domain.Version, error) {
	d, err := s.deliveries()
	if err != nil {
		return domain.Version{}, err
	}
	m, err := d.GetOfficeMetric(ctx, r.MetricID)
	if err != nil {
		return domain.Version{}, err
	}
	if m.TaskID != taskID {
		return domain.Version{}, domain.ErrScope
	}
	if strings.Count(r.Template, "{{value}}") != 1 || len(r.Template) > 16000 {
		return domain.Version{}, domain.ErrInvalid
	}
	source, sourceBytes, err := s.ReadVersion(ctx, taskID, m.SourceVersionID)
	if err != nil {
		return domain.Version{}, err
	}
	if source.SHA256 != m.SourceSHA256 || source.Quality == "stale" || source.Quality == "blocked" {
		return domain.Version{}, domain.ErrConflict
	}
	si, err := content.Inspect(content.Kind(source.Kind), sourceBytes)
	if err != nil {
		return domain.Version{}, err
	}
	sourceVerified := false
	for _, n := range si.Nodes {
		if n.ID == m.SourceNodeID && n.Digest == m.SourceNodeDigest && n.Text == m.RawValue {
			sourceVerified = true
			break
		}
	}
	if !sourceVerified {
		return domain.Version{}, domain.ErrConflict
	}
	v, b, err := s.ReadVersion(ctx, taskID, r.TargetVersionID)
	if err != nil {
		return domain.Version{}, err
	}
	if v.ArtifactID == source.ArtifactID {
		return v, domain.ErrInvalid
	}
	value := strings.NewReplacer("{{value}}", m.DisplayValue, "{{unit}}", m.Unit, "{{currency}}", m.Currency, "{{period}}", m.Period).Replace(r.Template)
	if strings.Contains(value, "{{") {
		return v, domain.ErrInvalid
	}
	patched, err := content.Patch(b, content.PatchRequest{Kind: content.Kind(v.Kind), BaseSHA256: v.SHA256, Operations: []content.TextPatch{{NodeID: r.TargetNodeID, ExpectedDigest: r.TargetNodeDigest, Text: value}}})
	if err != nil {
		return v, err
	}
	lease, err := s.putManaged(ctx, taskID, patched.Data)
	if err != nil {
		return v, err
	}
	defer s.releaseBlob(ctx, lease.ID)
	ref := lease.Digest
	next := domain.Version{TaskID: taskID, ArtifactID: v.ArtifactID, Kind: v.Kind, Name: v.Name, ContentRef: ref, SHA256: ref, Size: int64(len(patched.Data)), MediaType: v.MediaType, ContentMode: "imported", BaseVersionID: v.ID, Index: encode(patched.Inspection)}
	next, err = s.Store.PublishOfficeVersion(ctx, domain.PublishRequest{Version: next, ExpectedHeadRevision: r.ExpectedRevision, IdempotencyKey: key, CreatedBy: "office-studio", BlobLeaseID: lease.ID, Evidence: []domain.EvidenceEdge{{SourceVersionID: source.ID, SourceNode: m.SourceNodeID, TargetNode: r.TargetNodeID, Metric: encode(m)}}})
	if err != nil {
		return next, err
	}
	if _, err = s.Check(ctx, taskID, next.ID, false); err != nil {
		return next, err
	}
	return s.Store.GetOfficeVersion(ctx, next.ID)
}
