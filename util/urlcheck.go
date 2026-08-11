package util

import (
	"fmt"
	"net"
	"net/url"
)

// ValidatePublicURL 校验自定义 LLM 端点 URL:
//   - 仅允许 http/https
//   - 默认拒绝私有网段(防 SSRF);allowPrivate=true 时放行(个人部署接本地 Ollama/vLLM 使用)
//
// 注意:此处做 DNS 解析校验,极端 DNS rebinding 场景需在连接层加固,超出网盘范围。
func ValidatePublicURL(rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	if allowPrivate {
		return nil
	}

	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		// 域名解析失败:若本身是 IP 字面量则直接使用,否则报错
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			return fmt.Errorf("resolve host failed: %w", err)
		}
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("private network address not allowed: %s", ip)
		}
	}
	return nil
}

// isPrivateIP 判断 IP 是否属于私有/保留网段
// 覆盖:loopback(127/8、::1)、RFC1918(10/8、172.16/12、192.168/16)、
// 链路本地(169.254/16、fe80::/10)、IPv6 ULA(fc00::/7)、未指定地址
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 169.254.0.0/16 兜底(IsLinkLocalUnicast 对 IPv4 也应覆盖,此处双保险)
		return ip4[0] == 169 && ip4[1] == 254
	}
	return false
}
