package mroapp

import (
	"sort"
	"strings"
	"time"
)

type WorkPackage struct {
	SourceRefs    []string
	EvidenceState string
	ID            string
	Title         string
	Sources       []string
	Hours         float64
	CreatedAt     string
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
	if len(in.Assignments) == 0 {
		out = append(out, ConstraintViolation{Code: "C0", Detail: "未录入排程窗口"})
	}
	for _, due := range in.Dues {
		if due.State == DueStateOverdue || due.State == DueStateMissing {
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
	skills := make([]string, 0, len(used))
	for skill := range used {
		skills = append(skills, skill)
	}
	sort.Strings(skills)
	for _, skill := range skills {
		hours := used[skill]
		if hours > cap[skill] {
			out = append(out, ConstraintViolation{Code: "C2", Detail: "技能组工时超出 " + skill})
			break
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
	// Sorting keeps overlap checks bounded for the supported 10,000 records.
	wins := append([]ScheduleAssignment(nil), in.Assignments...)
	sort.Slice(wins, func(i, j int) bool {
		if wins[i].TailNo != wins[j].TailNo {
			return wins[i].TailNo < wins[j].TailNo
		}
		return wins[i].Start < wins[j].Start
	})
	latestEnd := map[string]string{}
	for _, a := range wins {
		start, errStart := time.Parse("2006-01-02", a.Start)
		end, errEnd := time.Parse("2006-01-02", a.End)
		if errStart != nil || errEnd != nil || !end.After(start) || a.Hours <= 0 || strings.TrimSpace(a.Skill) == "" || (!in.Today.IsZero() && start.Before(in.Today.UTC().Truncate(24*time.Hour))) {
			out = append(out, ConstraintViolation{Code: "C4", Detail: "窗口日期、工时或技能不完整，或窗口已经过期"})
			break
		}
		if a.Start < latestEnd[a.TailNo] {
			out = append(out, ConstraintViolation{Code: "C4", Detail: "同一机尾窗口重叠"})
			break
		}
		latestEnd[a.TailNo] = a.End
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
