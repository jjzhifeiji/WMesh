package clientmqtt

import (
	"encoding/json"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"

	"wmesh/factory/internal/platform/id"
)

func TestBrokerACLLocksOwnTopics(t *testing.T) {
	fid := id.New()
	a, b := id.New(), id.New()
	bus, err := Listen("127.0.0.1:0", Hooks{
		Auth: func(factoryID, clientID uuid.UUID, token string) (uuid.UUID, error) {
			if factoryID != fid {
				return uuid.Nil, errIntent("factory")
			}
			if (clientID == a && token == "tokA") || (clientID == b && token == "tokB") {
				return clientID, nil
			}
			return uuid.Nil, errIntent("token")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close() })

	cliA := connect(t, bus.Addr(), fid, a, "tokA")
	cliB := connect(t, bus.Addr(), fid, b, "tokB")
	gotA := make(chan string, 4)
	gotB := make(chan string, 4)
	sub(t, cliA, DownTopic(fid, a), gotA)
	sub(t, cliB, DownTopic(fid, b), gotB)

	// 他机 Topic 订不上：B 订 A 的 down，A 的下行 B 收不到。
	tok := cliB.Subscribe(DownTopic(fid, a), 1, func(_ mqtt.Client, m mqtt.Message) {
		select {
		case gotB <- string(m.Payload()):
		default:
		}
	})
	_ = tok.WaitTimeout(2 * time.Second)

	payload := []byte(`{"typ":"policy","revision":2}`)
	if err := bus.Publish(fid, a, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-gotA:
		if m != string(payload) {
			t.Fatalf("a got %s", m)
		}
		if HasBody([]byte(m)) {
			t.Fatal("body on down")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a missed own down")
	}
	select {
	case m := <-gotB:
		t.Fatalf("b saw a down: %s", m)
	case <-time.After(400 * time.Millisecond):
	}

	wrong := connectRaw(t, bus.Addr(), fid, a, "nope")
	if wrong.IsConnected() {
		t.Fatal("bad token connected")
	}
}

func connect(t *testing.T, addr string, fid, cid uuid.UUID, token string) mqtt.Client {
	t.Helper()
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://" + addr)
	opts.SetClientID(cid.String())
	opts.SetUsername(fid.String())
	opts.SetPassword(token)
	opts.SetAutoReconnect(false)
	opts.SetConnectTimeout(3 * time.Second)
	cli := mqtt.NewClient(opts)
	tok := cli.Connect()
	if !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("connect %s: %v", cid, tok.Error())
	}
	t.Cleanup(func() { cli.Disconnect(250) })
	return cli
}

func connectRaw(t *testing.T, addr string, fid, cid uuid.UUID, token string) mqtt.Client {
	t.Helper()
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://" + addr)
	opts.SetClientID(cid.String())
	opts.SetUsername(fid.String())
	opts.SetPassword(token)
	opts.SetAutoReconnect(false)
	opts.SetConnectTimeout(2 * time.Second)
	cli := mqtt.NewClient(opts)
	tok := cli.Connect()
	_ = tok.WaitTimeout(3 * time.Second)
	return cli
}

func sub(t *testing.T, cli mqtt.Client, topic string, ch chan string) {
	t.Helper()
	tok := cli.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) {
		select {
		case ch <- string(m.Payload()):
		default:
		}
	})
	if !tok.WaitTimeout(3*time.Second) || tok.Error() != nil {
		t.Fatal(tok.Error())
	}
}

func TestHasBodyDetectsMembers(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"typ": TypClosure, "members": []any{}})
	if !HasBody(raw) {
		t.Fatal("members")
	}
}
