// Command errcov measures wire error-code coverage of the M5 error
// contract test (T-5.5.3): it scans internal/bridge/m5/errors_test.go for
// "M5-XXX-NNN" literals and reports the fraction of the frozen 20-code
// registry the contract test pins. Any missing code exits 1 so CI can
// gate on full coverage.
//
// CVT-001/002 map onto the workspace convert sentinels (ErrConvertNoConfirm
// / ErrConvertPublishFailed) merged with T-5.5.1; all 20 codes carry both a
// sentinel mapping and a behavioral trigger in the contract test.
//
// Usage (from the module root): go run ./tools/errcov
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
)

// allCodes is the frozen 20-code M5 wire registry (must stay in sync with
// internal/bridge/m5/wire_errors.go). FROZEN (M5): 改动需走 ADR。
var allCodes = []string{
	"M5-RUN-001", "M5-RUN-002", "M5-RUN-003",
	"M5-WS-001", "M5-WS-002", "M5-WS-003", "M5-WS-004",
	"M5-GIT-001",
	"M5-ART-001",
	"M5-CMD-001", "M5-CMD-002",
	"M5-TASK-001",
	"M5-SKL-001",
	"M5-BRW-001", "M5-BRW-002",
	"M5-MCP-001", "M5-MCP-002", "M5-MCP-003",
	"M5-CVT-001", "M5-CVT-002",
}

func main() {
	path := flag.String("f", "internal/bridge/m5/errors_test.go", "M5 错误契约测试源码路径")
	flag.Parse()

	src, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "errcov: 读取契约测试源码失败: %v\n", err)
		os.Exit(2)
	}

	re := regexp.MustCompile(`M5-[A-Z]+-[0-9]{3}`)
	found := map[string]bool{}
	for _, m := range re.FindAllString(string(src), -1) {
		found[m] = true
	}

	required := make(map[string]bool, len(allCodes))
	for _, c := range allCodes {
		required[c] = true
	}

	var missing, unknown []string
	for _, c := range allCodes {
		if !found[c] {
			missing = append(missing, c)
		}
	}
	for c := range found {
		if !required[c] {
			unknown = append(unknown, c)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)

	covered := len(allCodes) - len(missing)
	pct := 100 * float64(covered) / float64(len(allCodes))
	fmt.Printf("M5 wire 错误码契约覆盖: %d/%d (%.0f%%)\n", covered, len(allCodes), pct)
	if len(unknown) > 0 {
		fmt.Printf("警告: 契约测试中出现名单外错误码: %v\n", unknown)
	}
	if len(missing) > 0 {
		fmt.Printf("缺失: %v\n", missing)
		os.Exit(1)
	}
	fmt.Println("覆盖完整: 全部 20 个错误码均被契约测试锁定")
}
