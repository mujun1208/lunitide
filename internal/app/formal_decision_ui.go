package app

import (
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

type FormalDecisionView struct {
	DecisionID    string
	Allowed       bool
	State         string
	Verified      bool
	MissingChecks []string
}

func PresentFormalDecision(dec domain.FormalDecision) FormalDecisionView {
	return FormalDecisionView{
		DecisionID:    dec.DecisionID,
		Allowed:       dec.Allowed,
		State:         dec.State,
		Verified:      dec.Allowed && dec.State == "verified",
		MissingChecks: append([]string{}, dec.MissingChecks...),
	}
}

func formalDraftMessage(accept bool, dec domain.FormalDecision) string {
	msg := "此版本检查未全部完成，请选择导出草稿"
	if accept {
		msg = "此版本检查未全部完成，请选择导出草稿或接受为草稿"
	}
	ids := append([]string{}, dec.MissingChecks...)
	ids = append(ids, dec.BlockingCodes...)
	if len(ids) > 0 {
		msg += "，所缺检查：" + strings.Join(ids, "、")
	}
	return msg
}
