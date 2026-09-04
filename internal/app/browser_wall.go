package app

import (
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func browserHumanGate(url, snapshot string) string {
	blob := strings.ToLower(url + "\n" + snapshot)
	switch {
	case strings.Contains(blob, "captcha") || strings.Contains(blob, "验证码"):
		return "captcha"
	case strings.Contains(blob, "/pay") || strings.Contains(blob, "alipay") || strings.Contains(blob, "checkout") || strings.Contains(blob, "支付"):
		return "pay"
	case strings.Contains(blob, "login") || strings.Contains(blob, "password") || strings.Contains(blob, "passwd") || strings.Contains(blob, "登录"):
		return "login"
	default:
		return ""
	}
}

func browserWallError(reason string) error {
	return fmt.Errorf("needs_user: browser %s wall. 请调用 user.ask，reason=%s，不要再 computer.act 或继续盲点", reason, reason)
}

func browserWallReason(summary string) string {
	blob := strings.ToLower(summary)
	if !strings.Contains(blob, "needs_user") {
		return ""
	}
	if !strings.Contains(blob, "user.ask") && !strings.Contains(blob, "browser") {
		return ""
	}
	for _, reason := range []string{"captcha", "pay", "login"} {
		if strings.Contains(blob, "reason="+reason) || strings.Contains(blob, "browser "+reason+" wall") {
			return reason
		}
	}
	return ""
}

func looksLikeBrowserWallToolResult(summary string) bool {
	return browserWallReason(summary) != ""
}

func finishBrowserAct(url, output string, out toolruntime.Result) (toolruntime.Result, error) {
	if wall := rejectBrowserHumanWall(url, output); wall != nil {
		return toolruntime.Result{}, wall
	}
	out.Output = toolruntime.AppendL0JSON(out.Output, "url", true, false, strings.TrimSpace(url))
	return out, nil
}

func browserActLooksStale(err error, output string) bool {
	blob := strings.ToLower("")
	if err != nil {
		blob += err.Error() + " "
	}
	blob += output
	blob = strings.ToLower(blob)
	if strings.Contains(blob, "stale") {
		return true
	}
	return strings.Contains(blob, "not found") && strings.Contains(blob, "ref")
}

func rejectBrowserHumanWall(url, snapshot string) error {
	if reason := browserHumanGate(url, snapshot); reason != "" {
		return browserWallError(reason)
	}
	return nil
}
