package mroapp

import (
	"strings"
	"time"
)

type Tool struct {
	ID        string
	ToolNo    string
	SN        string
	Location  string
	Holder    string
	CalibDue  string
	Status    string
	UpdatedAt string
}

type ToolLoan struct {
	ID        string
	ToolID    string
	Holder    string
	OutAt     string
	InAt      string
}

func CanCheckoutTool(tool Tool, today time.Time) (ok bool, reason string) {
	due := strings.TrimSpace(tool.CalibDue)
	if due == "" {
		return false, "校准到期未录入"
	}
	day, err := time.Parse("2006-01-02", due)
	if err != nil {
		return false, "校准到期未录入"
	}
	today = today.UTC().Truncate(24 * time.Hour)
	if day.UTC().Truncate(24 * time.Hour).Before(today) {
		return false, "校准过期"
	}
	return true, ""
}

type ChemLot struct {
	ID          string
	LotNo       string
	ParentLotID string
	Qty         float64
	Expires     string
	SDSDoc      string
}

type ChemUse struct {
	ID     string
	LotID  string
	TailNo string
	WO     string
	Tech   string
}

type LotTrace struct {
	LotID    string
	Children []string
	Tails    []string
}

func TraceLot(lots []ChemLot, uses []ChemUse, lotID string) LotTrace {
	out := LotTrace{LotID: lotID}
	seenTail := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		for _, lot := range lots {
			if lot.ParentLotID == id {
				out.Children = append(out.Children, lot.ID)
				walk(lot.ID)
			}
		}
		for _, use := range uses {
			if use.LotID != id && !containsStr(out.Children, use.LotID) {
				continue
			}
			if use.LotID == id || containsStr(out.Children, use.LotID) {
				if tail := strings.TrimSpace(use.TailNo); tail != "" && !seenTail[tail] {
					seenTail[tail] = true
					out.Tails = append(out.Tails, tail)
				}
			}
		}
	}
	walk(lotID)
	for _, use := range uses {
		if use.LotID == lotID {
			if tail := strings.TrimSpace(use.TailNo); tail != "" && !seenTail[tail] {
				seenTail[tail] = true
				out.Tails = append(out.Tails, tail)
			}
		}
	}
	return out
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

type Kit struct {
	ID   string
	Name string
}

type KitItem struct {
	KitID    string
	PN       string
	Required float64
	OnHand   float64
}

type KitView struct {
	Kit
	Items   []KitItem
	Missing []string
}

type LotView struct {
	ChemLot
	Children []string
	Tails    []string
}

type ToolView struct {
	Tool
	CheckoutBlocked string
}

func KitShortage(items []KitItem) []string {
	var missing []string
	for _, item := range items {
		if item.OnHand < item.Required {
			missing = append(missing, item.PN)
		}
	}
	return missing
}
