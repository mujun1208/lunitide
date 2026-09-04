package mroapp

import (
	"strings"
	"time"
)

type WorkPackage struct {
	ID        string
	Title     string
	Sources   []string
	Hours     float64
	CreatedAt string
}

type IntervalRule struct {
	ID            string
	TaskKey       string
	IntervalValue float64
	Unit          string
	Version       string
	EffectiveFrom string
	SourceCite    string
}

func AssembleWorkPackage(cards, ads, mels, open []string) WorkPackage {
	var sources []string
	add := func(kind string, items []string) {
		if len(items) > 0 {
			sources = append(sources, kind)
		}
	}
	add("标准卡", cards)
	add("AD/SB", ads)
	add("MEL", mels)
	add("未关闭项", open)
	return WorkPackage{Title: "工作包草稿", Sources: sources}
}

func ProposeIntervalChange(mpdCite, fleetCite string) (ok bool, reason string) {
	if strings.TrimSpace(mpdCite) == "" {
		return false, "缺少 MPD/AMP 条款引用"
	}
	if strings.TrimSpace(fleetCite) == "" {
		return false, "缺少本队数据引用"
	}
	return true, ""
}

type ScheduleAssignment struct {
	TailNo    string
	CheckName string
	Start     string
	End       string
	Hours     float64
	Skill     string
}

type CapacitySlot struct {
	Skill string
	Hours float64
}

type ConstraintViolation struct {
	Code   string
	Detail string
}

type ScheduleInput struct {
	Assignments []ScheduleAssignment
	Slots       []CapacitySlot
	Dues        []DueStatus
	AOGTails    []string
	KitMissing  []string
	LongLeadPN  []string
	HasCite     bool
	Today       time.Time
}

// CheckScheduleConstraints lists C1–C7 violations. It does not solve or auto-shift.
func CheckScheduleConstraints(in ScheduleInput) []ConstraintViolation {
	var out []ConstraintViolation
	for _, due := range in.Dues {
		if due.State == DueStateOverdue {
			out = append(out, ConstraintViolation{Code: "C1", Detail: "窗口晚于已超限到期项"})
			break
		}
	}
	used := map[string]float64{}
	for _, a := range in.Assignments {
		used[a.Skill] += a.Hours
	}
	cap := map[string]float64{}
	for _, s := range in.Slots {
		cap[s.Skill] += s.Hours
	}
	for skill, hours := range used {
		if cap[skill] > 0 && hours > cap[skill] {
			out = append(out, ConstraintViolation{Code: "C2", Detail: "技能组工时超出 " + skill})
		}
	}
	aog := map[string]bool{}
	for _, t := range in.AOGTails {
		aog[t] = true
	}
	for _, a := range in.Assignments {
		if aog[a.TailNo] {
			out = append(out, ConstraintViolation{Code: "C3", Detail: "机尾已标记 AOG/停场 " + a.TailNo})
			break
		}
	}
	type win struct{ start, end, tail string }
	var wins []win
	for _, a := range in.Assignments {
		if a.Start == "" || a.End == "" {
			continue
		}
		wins = append(wins, win{start: a.Start, end: a.End, tail: a.TailNo})
	}
	for i := 0; i < len(wins); i++ {
		for j := i + 1; j < len(wins); j++ {
			if wins[i].tail == wins[j].tail && wins[i].start < wins[j].end && wins[j].start < wins[i].end {
				out = append(out, ConstraintViolation{Code: "C4", Detail: "同一机尾窗口重叠"})
				i = len(wins)
				break
			}
		}
	}
	if len(in.KitMissing) > 0 {
		out = append(out, ConstraintViolation{Code: "C5", Detail: "套件缺件"})
	}
	if len(in.LongLeadPN) > 0 {
		out = append(out, ConstraintViolation{Code: "C6", Detail: "长周期件无库存且无替代"})
	}
	if !in.HasCite {
		out = append(out, ConstraintViolation{Code: "C7", Detail: "间隔规则缺 source_cite"})
	}
	return out
}
