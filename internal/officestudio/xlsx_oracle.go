package officestudio

import (
	"bytes"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var sixDigitID = regexp.MustCompile(`^[0-9]{6}$`)
var a1Cell = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]{0,6})$`)

func IndependentIntegerCents(insp Inspection) (int64, error) {
	byRow := map[string][]Node{}
	for _, n := range insp.Nodes {
		col, row, ok := splitA1(strings.TrimPrefix(n.Locator, "cell:"))
		if !ok {
			continue
		}
		key := n.Part + ":" + strconv.Itoa(row)
		byRow[key] = append(byRow[key], n)
		_ = col
	}
	var total int64
	seen := 0
	for _, nodes := range byRow {
		hasID := false
		var amount *int64
		for _, n := range nodes {
			if n.Kind == "cell:text" && sixDigitID.MatchString(n.Text) {
				hasID = true
			}
			if n.Kind == "cell:number" {
				v, err := strconv.ParseInt(n.Text, 10, 64)
				if err != nil {
					continue
				}
				if amount == nil || abs64(v) >= abs64(*amount) {
					cp := v
					amount = &cp
				}
			}
		}
		if hasID && amount != nil {
			total += *amount
			seen++
		}
	}
	if seen == 0 {
		return 0, fmt.Errorf("independent cents: no six-digit order rows")
	}
	return total, nil
}

func IndependentGrowth(insp Inspection) (string, error) {
	may, june, _, _, err := x02Inputs(insp)
	if err != nil {
		return "", err
	}
	if may.Sign() == 0 {
		return "", fmt.Errorf("independent growth: zero May sales")
	}
	diff := new(big.Rat).Sub(june, may)
	return new(big.Rat).Quo(diff, may).FloatString(2), nil
}

func IndependentAttainment(insp Inspection) (string, error) {
	_, june, _, juneTarget, err := x02Inputs(insp)
	if err != nil {
		return "", err
	}
	if juneTarget.Sign() == 0 {
		return "n/a", nil
	}
	return new(big.Rat).Quo(june, juneTarget).FloatString(4), nil
}

func IndependentZeroDenominator(insp Inspection) (string, error) {
	_, _, prev, _, err := x02Inputs(insp)
	if err != nil {
		return "", err
	}
	if prev.Sign() == 0 {
		return "n/a", nil
	}
	return "", fmt.Errorf("independent zero-denominator: previous was not zero")
}

func IndependentRangeSum(insp Inspection, area string) (int64, error) {
	r, err := parseRange(area)
	if err != nil {
		return 0, err
	}
	var total int64
	found := 0
	for _, n := range insp.Nodes {
		if n.Kind != "cell:number" {
			continue
		}
		col, row, ok := splitA1(strings.TrimPrefix(n.Locator, "cell:"))
		if !ok {
			continue
		}
		x := colIndex(col)
		if x < r.x1 || x > r.x2 || row < r.y1 || row > r.y2 {
			continue
		}
		v, err := strconv.ParseInt(n.Text, 10, 64)
		if err != nil {
			return 0, err
		}
		total += v
		found++
	}
	if found == 0 {
		return 0, fmt.Errorf("independent range sum: no number cells in %s", area)
	}
	return total, nil
}

func FormulaCacheDisagreesWithOracle(data []byte, oracle int64) bool {
	p, err := readPackage(data)
	if err != nil {
		return true
	}
	shared, err := sharedStrings(p.parts["xl/sharedStrings.xml"])
	if err != nil {
		return true
	}
	for name, body := range p.parts {
		if !strings.HasPrefix(name, "xl/worksheets/sheet") {
			continue
		}
		cells, err := spans(body, sheetNS, "c")
		if err != nil {
			return true
		}
		for _, c := range cells {
			raw := body[c.start:c.end]
			text, typ, _ := cellText(raw, shared)
			if typ != "formula" || !bytes.Contains(raw, []byte("<v>")) {
				_ = text
				continue
			}
			m := cachedValueXML.FindSubmatch(raw)
			if m == nil {
				continue
			}
			n, ok := nativeNumber(strings.TrimSpace(string(m[1])))
			if !ok {
				return true
			}
			got, _ := n.Float64()
			if int64(got) != oracle {
				return true
			}
		}
	}
	return false
}

func x02Inputs(insp Inspection) (may, june, prev, juneTarget *big.Rat, err error) {
	cells := map[string]Node{}
	for _, n := range insp.Nodes {
		if n.Part != "xl/worksheets/sheet1.xml" {
			continue
		}
		cells[strings.TrimPrefix(n.Locator, "cell:")] = n
	}
	must := func(addr string) (*big.Rat, error) {
		n, ok := cells[addr]
		if !ok || n.Kind != "cell:number" {
			return nil, fmt.Errorf("independent x02: missing number %s", addr)
		}
		r, ok := nativeNumber(n.Text)
		if !ok {
			return nil, fmt.Errorf("independent x02: %s not decimal", addr)
		}
		return r, nil
	}
	may, err = must("B2")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	june, err = must("B3")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	juneTarget, err = must("C3")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	prev, err = must("B4")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return may, june, prev, juneTarget, nil
}

func splitA1(addr string) (col string, row int, ok bool) {
	m := a1Cell.FindStringSubmatch(strings.ToUpper(addr))
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}

func colIndex(col string) int {
	n := 0
	for _, c := range col {
		n = n*26 + int(c-'A') + 1
	}
	return n
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

var cachedValueXML = regexp.MustCompile(`<v>([^<]*)</v>`)
