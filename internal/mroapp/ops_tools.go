package mroapp

import (
	"sort"
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
	ID     string
	ToolID string
	Holder string
	OutAt  string
	InAt   string
}

func CanCheckoutTool(tool Tool, today time.Time) (ok bool, reason string) {
	if tool.Holder != "" || tool.Status == "out" {
		return false, "工具已借出"
	}
	if tool.Status != "" && tool.Status != "ready" {
		return false, "工具当前不可借用"
	}
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

func lotIndexes(lots []ChemLot, uses []ChemUse) (map[string][]string, map[string][]ChemUse) {
	children := map[string][]string{}
	for _, lot := range lots {
		children[lot.ParentLotID] = append(children[lot.ParentLotID], lot.ID)
	}
	byLot := map[string][]ChemUse{}
	for _, use := range uses {
		byLot[use.LotID] = append(byLot[use.LotID], use)
	}
	return children, byLot
}
func TraceLot(lots []ChemLot, uses []ChemUse, lotID string) LotTrace {
	children, byLot := lotIndexes(lots, uses)
	return traceLotIndexed(children, byLot, lotID)
}
func traceLotIndexed(children map[string][]string, byLot map[string][]ChemUse, lotID string) LotTrace {
	out := LotTrace{LotID: lotID}

	seen := map[string]bool{lotID: true}
	queue := []string{lotID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, child := range children[id] {
			if seen[child] {
				continue
			}
			seen[child] = true
			out.Children = append(out.Children, child)
			queue = append(queue, child)
		}
	}
	tails := map[string]bool{}
	for id := range seen {
		for _, use := range byLot[id] {
			tail := strings.TrimSpace(use.TailNo)
			if tail != "" && !tails[tail] {
				tails[tail] = true
				out.Tails = append(out.Tails, tail)
			}
		}
	}
	sort.Strings(out.Tails)

	return out
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
