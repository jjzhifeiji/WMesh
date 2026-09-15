package lanprobe

import (
	"context"
	"net"
	"testing"
	"time"
)

// 局域网 UDP 探询应答。
func TestParseReplyAndProbe(t *testing.T) {
	if !IsProbe([]byte("WMESH-DISCOVER/1\n")) {
		t.Fatal("probe")
	}
	if IsProbe([]byte("nope")) {
		t.Fatal("junk")
	}
	p, ok := ParseReply(EncodeReply(52081))
	if !ok || p != 52081 {
		t.Fatalf("reply %d %v", p, ok)
	}
	if HTTPPort(":8080") != 8080 || HTTPPort("0.0.0.0:52081") != 52081 || HTTPPort("bad") != 0 {
		t.Fatalf("http port")
	}
}

func TestServeAnswers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	_ = pc.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- Serve(ctx, addr, 52081) }()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("udp4", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(Probe + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := ParseReply(buf[:n])
	if !ok || p != 52081 {
		t.Fatalf("got %q", buf[:n])
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve hung")
	}
}
