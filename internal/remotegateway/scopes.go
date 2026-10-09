package remotegateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"
)

var errNoEntropy = errors.New("remotegateway: entropy exhausted")

// scope 常量：PRD §5.2 的设备级授权范围。默认授予 chat/tools/files/hub，
// settings 需单独勾选（M0 走默认集，不提供改勾选的入口）。
const (
	scopeChat     = "chat"
	scopeTools    = "tools"
	scopeFiles    = "files"
	scopeHub      = "hub"
	scopeSettings = "settings"
)

// DefaultScopes 是新配对设备默认拿到的授权集。
var DefaultScopes = []string{scopeChat, scopeTools, scopeFiles, scopeHub}

// methodScope 把 bridge 方法名归类到授权范围。默认归 tools（MCP、技能、
// 插件、专家、agent 等全部能力面），敏感面显式归类：
//   - chat：对话/会话/消息/语音
//   - files：文件系统与工作区产物
//   - hub：产品中枢只读
//   - settings：系统设置与远程管理本身（默认拒绝）
func methodScope(method string) string {
	switch {
	case strings.HasPrefix(method, "chat."),
		strings.HasPrefix(method, "conversation."),
		strings.HasPrefix(method, "message."),
		strings.HasPrefix(method, "talk."),
		strings.HasPrefix(method, "tts."),
		method == "system.health",
		method == "system.diagnostics",
		// 模型列表只读：手机端选模型发起对话的前提；凭据与增删改
		// （provider.create/update/delete、credential.*）仍属 settings。
		method == "provider.list",
		method == "provider.get":
		return scopeChat
	case strings.HasPrefix(method, "fs."),
		strings.HasPrefix(method, "workspace"),
		strings.HasPrefix(method, "office."),
		strings.HasPrefix(method, "attachment."):
		return scopeFiles
	case strings.HasPrefix(method, "productHub."):
		return scopeHub
	case strings.HasPrefix(method, "settings."),
		strings.HasPrefix(method, "system.settings"),
		strings.HasPrefix(method, "remote."),
		strings.HasPrefix(method, "power."),
		strings.HasPrefix(method, "provider."),
		strings.HasPrefix(method, "secret"):
		return scopeSettings
	default:
		return scopeTools
	}
}

// deviceAllows 判断设备授权集是否放行该方法。
func deviceAllows(scopes []string, method string) bool {
	need := methodScope(method)
	for _, s := range scopes {
		if s == need {
			return true
		}
	}
	return false
}

// randomToken 生成 32 字节随机数的 hex（64 字符），用作设备令牌明文。
func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// randomPairCode 生成 8 位数字配对码（拒绝采样保证均匀）。
func randomPairCode() (string, error) {
	for i := 0; i < 32; i++ {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", err
		}
		value := uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
		if value < 1_000_000_000 {
			return padCode(int(value) % 100_000_000), nil
		}
	}
	return "", errNoEntropy
}

func padCode(value int) string {
	code := "00000000" + intToString(value)
	return code[len(code)-8:]
}

func intToString(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// hostAddresses 枚举本机可被手机直连的候选地址：公网 IPv6 置顶（手机蜂窝
// 流量免 Wi-Fi 直连家里的公网 IPv6，国内运营商蜂窝默认下发 IPv6——二维码
// 首选地址必须蜂窝可达，否则不连 Wi-Fi 时浏览器打不开配对页、无法下载
// APP），其次局域网 IPv4（同一 Wi-Fi 必达、零依赖），尾网（Tailscale/
// ZeroTier 的 100.64.0.0/10）垫底——它要求手机也登录同一尾网，国内环境
// 登录/中继常被阻断，只作为最后兜底。同 Wi-Fi 时手机与电脑同网段，IPv6
// 同样直连可达，置顶不损失局域网速度；无公网 IPv6 的网络回落 IPv4 首选。
// 返回顺序即二维码候选顺序。
func hostAddresses() []string {
	addresses := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return addresses
	}
	v6 := []string{}
	tailnet := ""
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				// 100.64.0.0/10（CGNAT）是 Tailscale/ZeroTier 等尾网的地址段：
				// 依赖手机端同尾网在线，排序垫底，仅在其余通道全不可达时兜底。
				if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
					if tailnet == "" {
						tailnet = ip4.String()
					}
					continue
				}
				addresses = append(addresses, ip4.String())
			} else if ip.IsGlobalUnicast() {
				v6 = append(v6, ip.String())
			}
		}
	}
	return orderHostCandidates(addresses, v6, tailnet)
}

// orderHostCandidates 按直连可达性给候选排序：公网 IPv6 → 局域网 IPv4 →
// 尾网垫底。IPv6 置顶钉死「蜂窝流量可扫码/可下载」的验收（IPv4 在蜂窝下
// 不可达，IPv6 双网可达）。抽成纯函数是因为 hostAddresses 枚举本机真实
// 网卡，测试机地址形状不定；排序契约（蜂窝必达的 IPv6 优先、同网必达的
// IPv4 次之、尾网最后兜底）在这里钉死。
func orderHostCandidates(lan []string, v6 []string, tailnet string) []string {
	ordered := append([]string{}, v6...)
	ordered = append(ordered, lan...)
	if tailnet != "" {
		ordered = append(ordered, tailnet)
	}
	return ordered
}
