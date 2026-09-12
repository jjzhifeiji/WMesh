package wanchannel

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/platform/nodekey"
)

func TestHold(t *testing.T) {
	pingEvery = 20 * time.Millisecond
	t.Cleanup(func() { pingEvery = 15 * time.Second })

	fid := uuid.New()
	pub, priv, err := nodekey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	gotPing := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		var msg envelope
		if err := wsjson.Read(ctx, c, &msg); err != nil || msg.Typ != "hello" || msg.FactoryID != fid.String() {
			return
		}
		nonce := make([]byte, 32)
		if _, err := rand.Read(nonce); err != nil {
			return
		}
		if err := wsjson.Write(ctx, c, envelope{Typ: "challenge", Nonce: nonce}); err != nil {
			return
		}
		if err := wsjson.Read(ctx, c, &msg); err != nil || msg.Typ != "hello_ack" {
			return
		}
		if !nodekey.Verify(pub, helloPayload(fid, nonce), msg.Signature) {
			_ = wsjson.Write(ctx, c, envelope{Typ: "error", Error: "unauthorized"})
			return
		}
		if err := wsjson.Write(ctx, c, envelope{Typ: "ready"}); err != nil {
			return
		}
		for {
			if err := wsjson.Read(ctx, c, &msg); err != nil {
				return
			}
			if msg.Typ != "ping" {
				continue
			}
			select {
			case gotPing <- struct{}{}:
			default:
			}
			if err := wsjson.Write(ctx, c, envelope{Typ: "pong"}); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Hold(ctx, srv.URL, fid, priv, nil, nil, nil, nil, nil, nil) }()
	select {
	case <-gotPing:
	case err := <-errCh:
		t.Fatalf("hold: %v", err)
	case <-ctx.Done():
		t.Fatal("no ping")
	}
	cancel()
	<-errCh
}

func TestHoldRetired(t *testing.T) {
	fid := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		var msg envelope
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			return
		}
		_ = wsjson.Write(ctx, c, envelope{Typ: "error", Error: "factory is retired"})
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Hold(ctx, srv.URL, fid, nil, nil, nil, nil, nil, nil, nil); !errors.Is(err, domain.ErrFactoryRetired) {
		t.Fatalf("hold: %v", err)
	}
}
