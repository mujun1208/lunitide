// T-5.5.3 wire error contract: the single translation table from service
// sentinel errors onto the frozen 20-code M5 wire registry (design doc
// M5/04 错误矩阵). WireCode walks the table with errors.Is, so wrapped
// chains (fmt.Errorf %w, errors.Join aggregates) map unchanged; unmapped
// errors return "" and stay opaque to the renderer.
package m5

import (
	"errors"

	"github.com/lunitide/lunitide/internal/browser"
	"github.com/lunitide/lunitide/internal/command"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/lunitide/lunitide/internal/skill"
	"github.com/lunitide/lunitide/internal/vcs"
	"github.com/lunitide/lunitide/internal/workspace"
)

// ErrSendNotDurable anchors M5-RUN-001: run.send could not prove the run
// row and the seq=1 user-message event were committed, so the renderer
// must not mark the send as delivered. The transaction layer surfaces
// opaque driver errors; the wire adapter stamps this sentinel when Send
// returns a failure after entering the write-ahead transaction.
var ErrSendNotDurable = errors.New("m5: run.send message not durable (M5-RUN-001)")

// wireSentinels is the frozen sentinel -> wire-code table. Order is
// irrelevant (no error maps to two codes). FROZEN (M5): 改动需走 ADR。
var wireSentinels = []struct {
	sentinel error
	code     string
}{
	// M5-RUN-001 run.send 落盘失败
	{ErrSendNotDurable, "M5-RUN-001"},
	// M5-RUN-002 cancel 非法（状态、reason 或 runId 未知）
	{ErrCancelStateInvalid, "M5-RUN-002"},
	{ErrCancelReasonInvalid, "M5-RUN-002"},
	{ErrRunNotFound, "M5-RUN-002"},
	// M5-RUN-003 租约/所有权冲突
	{ErrSessionMismatch, "M5-RUN-003"},
	{ErrIdempotencyConflict, "M5-RUN-003"},
	// M5-WS-001 配额超限
	{workspace.ErrQuotaExceeded, "M5-WS-001"},
	// M5-WS-002 路径穿越
	{workspace.ErrPathEscape, "M5-WS-002"},
	// M5-WS-003 工作区/基线状态冲突
	{m5workspace.ErrTransition, "M5-WS-003"},
	{m5workspace.ErrChangeSetConflict, "M5-WS-003"},
	// M5-WS-004 工作区不存在/已删除
	{ErrFsWorkspaceGone, "M5-WS-004"},
	{workspace.ErrFsWorkspaceGone, "M5-WS-004"},
	{m5workspace.ErrNotFound, "M5-WS-004"},
	// M5-GIT-001 git allowlist
	{vcs.ErrNotAllowed, "M5-GIT-001"},
	// M5-ART-001 artifact 校验族
	{m5workspace.ErrArtifactMime, "M5-ART-001"},
	{m5workspace.ErrArtifactTooLarge, "M5-ART-001"},
	{m5workspace.ErrArtifactTampered, "M5-ART-001"},
	// M5-CMD-001 spec 验签族
	{command.ErrSpecSignature, "M5-CMD-001"},
	{command.ErrSpecExpired, "M5-CMD-001"},
	{command.ErrSpecRevoked, "M5-CMD-001"},
	// M5-CMD-002 argv/环境/cwd
	{command.ErrParamInvalid, "M5-CMD-002"},
	{command.ErrEnvNotAllowed, "M5-CMD-002"},
	{command.ErrCwdOutsideWorkspace, "M5-CMD-002"},
	{command.ErrTemplateUnknown, "M5-CMD-002"},
	// M5-TASK-001 孤儿/未知任务
	{ErrTaskNotFound, "M5-TASK-001"},
	{command.ErrOrphaned, "M5-TASK-001"},
	// M5-SKL-001 技能清单拒绝
	{skill.ErrSkillRejected, "M5-SKL-001"},
	// M5-BRW-001 浏览器策略（协议/敏感意图）
	{browser.ErrProtocolBlocked, "M5-BRW-001"},
	{browser.ErrDownloadBlocked, "M5-BRW-001"},
	// M5-BRW-002 DNS 重绑定防线（本机/内网/保留地址、重定向链）
	{browser.ErrPrivateAddress, "M5-BRW-002"},
	{browser.ErrLoopbackBlocked, "M5-BRW-002"},
	{browser.ErrTooManyRedirects, "M5-BRW-002"},
	// M5-MCP-001 传输策略
	{mcp.ErrNotHttps, "M5-MCP-001"},
	{mcp.ErrHostNotAllowed, "M5-MCP-001"},
	{mcp.ErrRedirectBlocked, "M5-MCP-001"},
	{mcp.ErrMethodNotAllowed, "M5-MCP-001"},
	// M5-MCP-002 响应策略
	{mcp.ErrResponseTooLarge, "M5-MCP-002"},
	{mcp.ErrEncodingBlocked, "M5-MCP-002"},
	{mcp.ErrHttpStatus, "M5-MCP-002"},
	// M5-MCP-003 超时重试
	{mcp.ErrInvokeFailed, "M5-MCP-003"},
	// M5-CVT-001 工作区转换缺少用户确认
	{workspace.ErrConvertNoConfirm, "M5-CVT-001"},
	// M5-CVT-002 转换发布失败已回滚
	{workspace.ErrConvertPublishFailed, "M5-CVT-002"},
}

// wireCodes is the frozen 20-code registry.
//
// FROZEN (M5): 名单与文案改动需走 ADR。
var wireCodes = []string{
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

// wireMessages is the user-facing Chinese catalog, one entry per wire code.
var wireMessages = map[string]string{
	"M5-RUN-001": "消息发送未能可靠落盘，请重试；已落盘内容不受影响。",
	"M5-RUN-002": "取消请求与当前运行状态冲突：运行不存在、已终止或 reason 非法。",
	"M5-RUN-003": "运行租约或所有权冲突：当前会话不持有该运行，请刷新后重试。",
	"M5-WS-001":  "工作区硬配额已满并转为只读，请清理文件后重试。",
	"M5-WS-002":  "路径非法或试图越出工作区根目录，已被拒绝。",
	"M5-WS-003":  "工作区状态不允许该操作（状态机拒绝或基线漂移），请刷新后重试。",
	"M5-WS-004":  "工作区不存在或已被清理删除。",
	"M5-GIT-001": "git 子命令或参数不在允许清单内，已拒绝执行。",
	"M5-ART-001": "产物校验失败：MIME 非法或伪装可执行、超出大小上限或内容与登记摘要不符。",
	"M5-CMD-001": "命令规格清单验签失败：签名无效、已过期或已吊销。",
	"M5-CMD-002": "命令启动参数、环境变量或工作目录不符合规格约束。",
	"M5-TASK-001": "任务不存在或已成孤儿被回收。",
	"M5-SKL-001": "内置技能清单被拒绝：签名、摘要或有效期校验未通过。",
	"M5-BRW-001": "浏览目标违反策略：协议不在允许范围或敏感意图（下载等）被阻止。",
	"M5-BRW-002": "目标地址属于本机/内网/保留地址或重定向链异常，疑似 DNS 重绑定，已拒绝。",
	"M5-MCP-001": "远程 MCP 端点违反传输策略：仅允许 HTTPS + 主机允许清单 + 只读 GET。",
	"M5-MCP-002": "远程 MCP 响应违反策略：非 2xx、手工内容编码或超过 4MiB 上限。",
	"M5-MCP-003": "远程 MCP 调用在超时与一次重试后仍然失败。",
	"M5-CVT-001": "工作区转换参数缺失或缺少用户明确确认，请先完成预检确认。",
	"M5-CVT-002": "工作区转换原子发布失败（目标冲突或并发转换），已回滚。",
}

// WireCode translates err onto the M5 wire registry. It returns "" for
// unmapped errors (including nil).
func WireCode(err error) string {
	if err == nil {
		return ""
	}
	for _, m := range wireSentinels {
		if errors.Is(err, m.sentinel) {
			return m.code
		}
	}
	return ""
}

// WireMessage returns the Chinese user-facing message for a wire code
// ("" when the code is unknown).
func WireMessage(code string) string { return wireMessages[code] }

// AllWireCodes returns the frozen 20-code registry (a fresh slice).
func AllWireCodes() []string {
	out := make([]string, len(wireCodes))
	copy(out, wireCodes)
	return out
}
