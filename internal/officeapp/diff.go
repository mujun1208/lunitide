package officeapp

import (
	"context"
	"fmt"
	"sort"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

const MaxDiffResponseBytes = 100 << 10

type DiffOptions struct {
	NodeOffset int `json:"nodeOffset"`
	PartOffset int `json:"partOffset"`
}

type DiffSummary struct {
	AddedNodes     int   `json:"addedNodes"`
	DeletedNodes   int   `json:"deletedNodes"`
	ModifiedNodes  int   `json:"modifiedNodes"`
	UnchangedNodes int   `json:"unchangedNodes"`
	AddedParts     int   `json:"addedParts"`
	DeletedParts   int   `json:"deletedParts"`
	ModifiedParts  int   `json:"modifiedParts"`
	UnchangedParts int   `json:"unchangedParts"`
	UnchangedBytes int64 `json:"unchangedBytes"`
}

type DiffNodeValue struct {
	Kind          string `json:"kind"`
	Text          string `json:"text"`
	Digest        string `json:"digest"`
	TextTruncated bool   `json:"textTruncated"`
}

type DiffNodeChange struct {
	Change           string         `json:"change"` // added, deleted, modified
	NodeID           string         `json:"nodeId"`
	Part             string         `json:"part"`
	PartNameDigest   string         `json:"partNameDigest"`
	PartTruncated    bool           `json:"partTruncated"`
	Locator          string         `json:"locator"`
	LocatorTruncated bool           `json:"locatorTruncated"`
	ChangedFields    []string       `json:"changedFields"` // text, kind, xml
	Before           *DiffNodeValue `json:"before,omitempty"`
	After            *DiffNodeValue `json:"after,omitempty"`
}

type DiffPartChange struct {
	Change        string `json:"change"`
	Name          string `json:"name"`
	NameDigest    string `json:"nameDigest"`
	NameTruncated bool   `json:"nameTruncated"`
	BeforeSHA256  string `json:"beforeSha256,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	BeforeSize    int    `json:"beforeSize"`
	Size          int    `json:"size"`
}

type DiffResult struct {
	BaseVersionID      string           `json:"baseVersionId"`
	VersionID          string           `json:"versionId"`
	Kind               string           `json:"kind"`
	BaseSHA256         string           `json:"baseSha256"`
	SHA256             string           `json:"sha256"`
	ComparisonBasis    string           `json:"comparisonBasis"`
	BytesIdentical     bool             `json:"bytesIdentical"`
	PartHashesCompared bool             `json:"partHashesCompared"`
	PayloadIdentical   bool             `json:"payloadIdentical"`
	Summary            DiffSummary      `json:"summary"`
	Changes            []DiffNodeChange `json:"changes"`
	Parts              []DiffPartChange `json:"parts"`
	NodeOffset         int              `json:"nodeOffset"`
	PartOffset         int              `json:"partOffset"`
	NextNodeOffset     int              `json:"nextNodeOffset"`
	NextPartOffset     int              `json:"nextPartOffset"`
	TotalNodeChanges   int              `json:"totalNodeChanges"`
	TotalPartChanges   int              `json:"totalPartChanges"`
	Truncated          bool             `json:"truncated"`
	Notice             string           `json:"notice"`
}

// Diff reads both immutable snapshots in the requested task/scope. It does not
// publish a version, invoke a renderer or trust the bounded stored index.
func (s *Service) Diff(ctx context.Context, taskID, baseVersionID, versionID string, opts DiffOptions) (DiffResult, error) {
	if opts.NodeOffset < 0 || opts.PartOffset < 0 {
		return DiffResult{}, domain.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return DiffResult{}, err
	}
	if _, err := s.Store.GetOfficeTask(ctx, taskID); err != nil {
		return DiffResult{}, err
	}
	base, beforeBytes, err := s.ReadVersion(ctx, taskID, baseVersionID)
	if err != nil {
		return DiffResult{}, err
	}
	version, afterBytes, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return DiffResult{}, err
	}
	if base.Kind != version.Kind {
		return DiffResult{}, fmt.Errorf("%w: different document formats cannot be structurally compared", domain.ErrInvalid)
	}
	before, err := content.Inspect(content.Kind(base.Kind), beforeBytes)
	if err != nil {
		return DiffResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return DiffResult{}, err
	}
	after, err := content.Inspect(content.Kind(version.Kind), afterBytes)
	if err != nil {
		return DiffResult{}, err
	}
	r, err := compareInspections(ctx, before, after, opts)
	if err != nil {
		return DiffResult{}, err
	}
	r.BaseVersionID, r.VersionID = base.ID, version.ID
	if len(encode(r)) > MaxDiffResponseBytes {
		return DiffResult{}, content.ErrLimit
	}
	return r, nil
}

type sourceNodeChange struct {
	before, after *content.Node
}

func compareInspections(ctx context.Context, before, after content.Inspection, opts DiffOptions) (DiffResult, error) {
	r := DiffResult{Kind: string(after.Kind), BaseSHA256: before.SHA256, SHA256: after.SHA256, ComparisonBasis: "structure", BytesIdentical: before.SHA256 == after.SHA256, Changes: []DiffNodeChange{}, Parts: []DiffPartChange{}, NodeOffset: opts.NodeOffset, PartOffset: opts.PartOffset, NextNodeOffset: opts.NodeOffset, NextPartOffset: opts.PartOffset,
		Notice: "按原稿部件与节点位置比较；插入内容可能使后续文本位置变化。未变部件已按完整原始摘要核对，变更部件可能还含未索引的样式或对象。此结果不代表像素排版一致，也不判断修改是否符合业务意图。"}
	if before.Kind != after.Kind || opts.NodeOffset < 0 || opts.PartOffset < 0 {
		return r, domain.ErrInvalid
	}
	if after.Kind == content.PDF {
		if opts.NodeOffset != 0 || opts.PartOffset != 0 {
			return r, domain.ErrInvalid
		}
		r.ComparisonBasis = "file-digest"
		r.PayloadIdentical = r.BytesIdentical
		r.Notice = "PDF 仅比较完整文件摘要；未执行页面、文字或对象差异分析。文件摘要不同不能推断具体内容或排版变化。"
		return r, ctx.Err()
	}
	beforeNodes := make(map[nodeAddress]*content.Node, len(before.Nodes))
	for i := range before.Nodes {
		n := &before.Nodes[i]
		beforeNodes[nodeKey(n)] = n
	}
	changes := make([]sourceNodeChange, 0)
	for i := range after.Nodes {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		n := &after.Nodes[i]
		key := nodeKey(n)
		old := beforeNodes[key]
		if old == nil {
			r.Summary.AddedNodes++
			changes = append(changes, sourceNodeChange{after: n})
		} else if old.Digest != n.Digest || old.Kind != n.Kind || old.Text != n.Text {
			r.Summary.ModifiedNodes++
			changes = append(changes, sourceNodeChange{old, n})
		} else {
			r.Summary.UnchangedNodes++
		}
		delete(beforeNodes, key)
	}
	for _, old := range beforeNodes {
		r.Summary.DeletedNodes++
		changes = append(changes, sourceNodeChange{before: old})
	}
	sort.Slice(changes, func(i, j int) bool {
		a, b := changeNode(changes[i]), changeNode(changes[j])
		if a.Part != b.Part {
			return a.Part < b.Part
		}
		return a.Locator < b.Locator
	})
	beforeParts := make(map[string]content.Part, len(before.Parts))
	for _, p := range before.Parts {
		beforeParts[p.Name] = p
	}
	partChanges := make([]DiffPartChange, 0)
	for _, p := range after.Parts {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		old, exists := beforeParts[p.Name]
		if !exists {
			r.Summary.AddedParts++
			partChanges = append(partChanges, partDelta(nil, &p))
		} else if old.SHA256 != p.SHA256 || old.Size != p.Size {
			r.Summary.ModifiedParts++
			partChanges = append(partChanges, partDelta(&old, &p))
		} else {
			r.Summary.UnchangedParts++
			r.Summary.UnchangedBytes += int64(p.Size)
		}
		delete(beforeParts, p.Name)
	}
	for _, p := range beforeParts {
		r.Summary.DeletedParts++
		partChanges = append(partChanges, partDelta(&p, nil))
	}
	sort.Slice(partChanges, func(i, j int) bool {
		if partChanges[i].Name == partChanges[j].Name {
			return partChanges[i].NameDigest < partChanges[j].NameDigest
		}
		return partChanges[i].Name < partChanges[j].Name
	})
	r.PartHashesCompared = true
	r.PayloadIdentical = len(partChanges) == 0
	r.TotalNodeChanges = len(changes)
	r.TotalPartChanges = len(partChanges)
	if opts.NodeOffset > len(changes) || opts.PartOffset > len(partChanges) {
		return DiffResult{}, domain.ErrInvalid
	}
	used := 0
	for _, c := range changes[opts.NodeOffset:] {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		row := nodeDelta(c)
		cost := len(encode(row)) + 1
		if used+cost > 60<<10 || len(r.Changes) >= 200 {
			break
		}
		r.Changes = append(r.Changes, row)
		r.NextNodeOffset++
		used += cost
	}
	used = 0
	for _, row := range partChanges[opts.PartOffset:] {
		cost := len(encode(row)) + 1
		if used+cost > 24<<10 || len(r.Parts) >= 100 {
			break
		}
		r.Parts = append(r.Parts, row)
		r.NextPartOffset++
		used += cost
	}
	r.Truncated = r.NextNodeOffset < len(changes) || r.NextPartOffset < len(partChanges)
	if len(encode(r)) > MaxDiffResponseBytes {
		return DiffResult{}, content.ErrLimit
	}
	return r, ctx.Err()
}

type nodeAddress struct{ part, locator string }

func nodeKey(n *content.Node) nodeAddress { return nodeAddress{n.Part, n.Locator} }
func changeNode(c sourceNodeChange) *content.Node {
	if c.after != nil {
		return c.after
	}
	return c.before
}
func nodeDelta(c sourceNodeChange) DiffNodeChange {
	n := changeNode(c)
	part, truncated := shortDiffText(n.Part, 1024)
	locator, locatorTruncated := shortDiffText(n.Locator, 1024)
	r := DiffNodeChange{NodeID: n.ID, Part: part, PartNameDigest: digest([]byte(n.Part)), PartTruncated: truncated, Locator: locator, LocatorTruncated: locatorTruncated, ChangedFields: []string{}}
	if c.before != nil {
		r.Before = diffValue(c.before)
	}
	if c.after != nil {
		r.After = diffValue(c.after)
	}
	switch {
	case c.before == nil:
		r.Change = "added"
	case c.after == nil:
		r.Change = "deleted"
	default:
		r.Change = "modified"
		if c.before.Text != c.after.Text {
			r.ChangedFields = append(r.ChangedFields, "text")
		}
		if c.before.Kind != c.after.Kind {
			r.ChangedFields = append(r.ChangedFields, "kind")
		}
		if c.before.Digest != c.after.Digest {
			r.ChangedFields = append(r.ChangedFields, "xml")
		}
	}
	return r
}
func diffValue(n *content.Node) *DiffNodeValue {
	text, truncated := shortDiffText(n.Text, 2048)
	return &DiffNodeValue{Kind: n.Kind, Text: text, Digest: n.Digest, TextTruncated: truncated}
}
func partDelta(before, after *content.Part) DiffPartChange {
	r := DiffPartChange{Change: "modified"}
	p := after
	if p == nil {
		p = before
		r.Change = "deleted"
	} else if before == nil {
		r.Change = "added"
	}
	r.Name, r.NameTruncated = shortDiffText(p.Name, 1024)
	r.NameDigest = digest([]byte(p.Name))
	if before != nil {
		r.BeforeSHA256 = before.SHA256
		r.BeforeSize = before.Size
	}
	if after != nil {
		r.SHA256 = after.SHA256
		r.Size = after.Size
	}
	return r
}
func shortDiffText(text string, limit int) (string, bool) {
	count := 0
	for i := range text {
		if count == limit {
			return text[:i], true
		}
		count++
	}
	return text, false
}
