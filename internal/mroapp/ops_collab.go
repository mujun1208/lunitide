package mroapp

import "strings"

type OpsTodo struct {
	ID        string
	Kind      string
	Ref       string
	Status    string
	Detail    string
	CreatedAt string
}

func PublishScheduleTodos(pkg WorkPackage) []OpsTodo {
	ref := strings.TrimSpace(pkg.ID)
	if ref == "" {
		ref = strings.TrimSpace(pkg.Title)
	}
	return []OpsTodo{
		{Kind: "kit_staging", Ref: ref, Status: "open", Detail: "发布后套件备妥"},
		{Kind: "parts_request", Ref: ref, Status: "open", Detail: "发布后航材备料"},
	}
}

type BulletinChain struct {
	LotID   string
	Tails   []string
	Note    string
	Freeze  bool
	RecomputeDue bool
}

func QualityBulletinChain(lotID string, uses []ChemUse, lots ...[]ChemLot) BulletinChain {
	var lotRows []ChemLot
	if len(lots) > 0 {
		lotRows = lots[0]
	}
	trace := TraceLot(lotRows, uses, lotID)
	return BulletinChain{
		LotID:        lotID,
		Tails:        trace.Tails,
		Note:         "质量通报串查草稿：适航影响待法规/手册 cite，航材冻结与询价只出草稿，到期重算待人审",
		Freeze:       true,
		RecomputeDue: true,
	}
}
