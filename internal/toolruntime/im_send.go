package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

func (r *Runtime) executeIMSend(ctx context.Context, session string, args json.RawMessage, approved, unconfined bool) (Result, error) {
	var a struct {
		Channel string `json:"channel"`
		To      string `json:"to"`
		Text    string `json:"text"`
	}
	if strict(args, &a) != nil {
		return Result{}, errors.New("无法执行：参数无效")
	}
	text := strings.TrimSpace(a.Text)
	if text == "" || utf8.RuneCountInString(text) > 4000 {
		return Result{}, errors.New("无法执行：没有可发送的内容")
	}
	if r == nil || r.imSend == nil {
		return Result{}, errors.New("无法执行：消息通道未配置")
	}
	desktopApp, output, err := r.imSend(ctx, a.Channel, strings.TrimSpace(a.To), text)
	if err != nil {
		return Result{}, err
	}
	if desktopApp == "" {
		return result(output), nil
	}
	if !unconfined || !r.FullDiskEnabled() {
		return Result{}, errors.New("无法执行：本机客户端发送需要完整磁盘访问")
	}
	path, others, err := pickLaunchTarget(desktopApp)
	if err != nil {
		return Result{}, fmt.Errorf("无法执行：打不开%s（%v）", desktopApp, err)
	}
	if path == "" {
		return Result{}, fmt.Errorf("无法执行：多个程序匹配%s：%s", desktopApp, strings.Join(others, ", "))
	}
	if err = openWithDefaultApp(path); err != nil {
		return Result{}, fmt.Errorf("无法执行：打不开%s（%v）", desktopApp, err)
	}
	invoke := func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (Result, error) {
		return r.runCcTool(ctx, FullAccess, session, tool, args, approved, unconfined)
	}
	typeArgs, _ := json.Marshal(map[string]any{
		"text":   text,
		"window": desktopApp,
		"submit": true,
		"after":  strings.TrimSpace(a.To),
	})
	typed, typeErr := executeDesktopType(ctx, invoke, session, typeArgs, approved, unconfined)
	if typeErr != nil {
		return result(fmt.Sprintf("opened %s; type into the chat: %v", desktopApp, typeErr)), nil
	}
	return result(fmt.Sprintf("opened %s; %s", desktopApp, typed.Output)), nil
}
