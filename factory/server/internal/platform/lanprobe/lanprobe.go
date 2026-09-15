// Package lanprobe 在局域网用 UDP 回答「厂服务在哪」，不含业务身份、钥或正文。
package lanprobe

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	Probe   = "WMESH-DISCOVER/1" // Client 广播探询
	Reply   = "WMESH-FACTORY/1"  // 厂服回 HTTP 端口
	UDPPort = 52082              // 默认探询端口
)

// HTTPPort 从监听地址取出端口；解析失败则 0。
func HTTPPort(addr string) int {
	_, p, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 || n > 65535 {
		return 0
	}
	return n
}

// EncodeReply 把对外 HTTP 端口收成探活应答。
func EncodeReply(httpPort int) []byte {
	return []byte(fmt.Sprintf("%s %d\n", Reply, httpPort))
}

// ParseReply 认出厂服应答并取出 HTTP 端口。
func ParseReply(b []byte) (int, bool) {
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, Reply) {
		return 0, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, Reply))
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 || n > 65535 {
		return 0, false
	}
	return n, true
}

// IsProbe 是否为本协议探询。
func IsProbe(b []byte) bool {
	return strings.TrimSpace(string(b)) == Probe
}

// Serve 在 UDP 口应答探询，直到上下文取消。httpPort 是给 Client 连的对外端口。
func Serve(ctx context.Context, udpAddr string, httpPort int) error {
	if httpPort <= 0 {
		return fmt.Errorf("http port required")
	}
	if strings.TrimSpace(udpAddr) == "" || udpAddr == "-" {
		return nil
	}
	pc, err := net.ListenPacket("udp4", udpAddr)
	if err != nil {
		return err
	}
	defer pc.Close()
	go func() {
		<-ctx.Done()
		_ = pc.SetDeadline(time.Now())
	}()
	buf := make([]byte, 256)
	reply := EncodeReply(httpPort)
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		if !IsProbe(buf[:n]) {
			continue
		}
		_, _ = pc.WriteTo(reply, addr)
	}
}
