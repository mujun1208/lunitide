package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/winexec"
)

var quitDesktopProcesses = winexec.QuitProcessImages
var openDesktopURL = openHTTPURL

func executeDesktopQuit(ctx context.Context, raw json.RawMessage, approved bool) (Result, error) {
	var a struct {
		Name  string `json:"name"`
		Force bool   `json:"force"`
	}
	if strict(raw, &a) != nil || strings.TrimSpace(a.Name) == "" {
		return Result{}, errors.New("invalid arguments")
	}
	if err := requireDesktopAction(approved); err != nil {
		return Result{}, err
	}
	// Process operations require an exact known application name.
	var target *knownLaunchApp
	for i := range knownLaunchApps {
		for _, alias := range append([]string{knownLaunchApps[i].Canonical}, knownLaunchApps[i].Aliases...) {
			if strings.EqualFold(strings.TrimSpace(a.Name), alias) {
				target = &knownLaunchApps[i]
				break
			}
		}
		if target != nil {
			break
		}
	}
	if target == nil {
		return Result{}, errors.New("无法执行：请指定受支持应用的完整名称，不按模糊名称结束进程")
	}
	if !a.Force {
		return Result{}, errors.New("未退出：彻底退出会结束该应用的进程，未保存的内容可能丢失；仅在用户明确要求彻底退出时使用 force=true")
	}
	count, err := quitDesktopProcesses(ctx, target.Processes)
	if err != nil {
		return Result{}, fmt.Errorf("未能确认%s已完全退出：%w", target.Canonical, err)
	}
	if count == 0 {
		return result(appendL0JSON(target.Canonical+"未运行，无需退出", "process", true, false, target.Canonical)), nil
	}
	return result(appendL0JSON(fmt.Sprintf("已彻底退出%s，已确认目标进程不再运行", target.Canonical), "process", true, false, target.Canonical)), nil
}

func executeDesktopBrowse(raw json.RawMessage, approved bool) (Result, error) {
	var a struct {
		URL   string `json:"url"`
		Query string `json:"query"`
	}
	if strict(raw, &a) != nil {
		return Result{}, errors.New("invalid arguments")
	}
	if err := requireDesktopAction(approved); err != nil {
		return Result{}, err
	}
	address, query := strings.TrimSpace(a.URL), strings.TrimSpace(a.Query)
	if address != "" && query != "" {
		return Result{}, errors.New("specify url or query, not both")
	}
	if len(query) > 2000 || len(address) > 8192 {
		return Result{}, errors.New("url/query too long")
	}
	if address == "" {
		address = "https://www.bing.com/"
		if query != "" {
			address += "search?" + url.Values{"q": {query}}.Encode()
		}
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return Result{}, errors.New("only HTTP(S) browser URLs are allowed")
	}
	if err := openDesktopURL(u.String()); err != nil {
		return Result{}, err
	}
	return result("已向系统默认桌面浏览器发送打开请求：" + u.String() + "；页面加载结果需通过 computer.act 核对"), nil
}
