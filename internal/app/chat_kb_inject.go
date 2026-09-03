package app

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/m8app"
)

const kbInjectMaxItems = 6

func (e *Engine) appendExpertKBEvidence(ctx context.Context, req chatMemoryRequest, pack *chatMemoryPack) {
	if e == nil || pack == nil || req.Companion || e.m8kb == nil {
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" || len(req.ExpertIDs) == 0 {
		return
	}
	added := 0
	for _, expertID := range req.ExpertIDs {
		expertID = strings.TrimSpace(expertID)
		if len(expertID) != 26 {
			continue
		}
		name, catalogID := e.lookupExpertIdentity(ctx, expertID)
		if isMROColleague(name, catalogID) {
			pack.MROTurn = true
		}
		tailNo, asOf := e.sessionMROContext(ctx, req.SessionID)
		res, err := e.m8kb.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: query, TopK: 4, TailNo: tailNo, AsOf: asOf})
		if err != nil {
			continue
		}
		pack.KBDiscarded += len(res.Explanation.NotAdopted)
		if len(res.Hits) == 0 {
			continue
		}
		for _, hit := range res.Hits {
			if added >= kbInjectMaxItems {
				break
			}
			quote := strings.TrimSpace(hit.Quote)
			if quote == "" {
				continue
			}
			if n := utf8.RuneCountInString(quote); n > 240 {
				quote = string([]rune(quote)[:240])
			}
			rev := strings.TrimSpace(hit.Revision)
			ata, _ := locatorStringField(hit.Locator, "ata")
			docType, _ := locatorStringField(hit.Locator, "docType")
			line := formatExpertKBEvidence(name, rev, ata, quote)
			pack.Evidence = append(pack.Evidence, contextapp.ContextSource{
				Type:       contextapp.SourceRetrievedEvidence,
				ID:         hit.DocID,
				Authority:  contextapp.AuthorityEvidence,
				Content:    line,
				Provenance: "kb:" + expertID + ":" + hit.DocID,
			})
			pack.KBCites = append(pack.KBCites, CitationBlock{
				ExpertID:   expertID,
				ExpertName: name,
				DocID:      hit.DocID,
				DocType:    docType,
				Revision:   rev,
				Locator:    hit.Locator,
				Quote:      quote,
				ATA:        ata,
			})
			added++
		}
	}
}

func formatExpertKBEvidence(name, revision, ata, quote string) string {
	if strings.TrimSpace(name) == "" {
		name = "专家"
	}
	var b strings.Builder
	b.WriteString("[专家知识 ")
	b.WriteString(name)
	b.WriteString("]\n")
	if revision != "" {
		b.WriteString("修订: ")
		b.WriteString(revision)
		b.WriteString("  ")
	}
	if ata != "" {
		b.WriteString("ATA: ")
		b.WriteString(ata)
		b.WriteString("  ")
	}
	b.WriteString("引用: ")
	b.WriteString(quote)
	return b.String()
}

func (e *Engine) lookupExpertIdentity(ctx context.Context, expertID string) (name, catalogID string) {
	if e == nil || e.m8expert == nil {
		return "专家", ""
	}
	detail, err := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: expertID})
	if err != nil {
		return "专家", ""
	}
	name, _ = detail.Expert["name"].(string)
	catalogID, _ = detail.Expert["catalogItemId"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "专家"
	}
	return name, strings.TrimSpace(catalogID)
}

func isMROColleague(name, catalogID string) bool {
	return catalogID == "mro-expert" || name == "航空机务维修专家" || name == "航空机务专家"
}

func locatorStringField(raw, key string) (string, bool) {
	var loc map[string]any
	if json.Unmarshal([]byte(raw), &loc) != nil {
		return "", false
	}
	s, ok := loc[key].(string)
	return s, ok
}
