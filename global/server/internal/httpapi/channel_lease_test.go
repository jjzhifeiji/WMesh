package httpapi_test

import (
	"encoding/json"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"wmesh/global/internal/service"
)

// 厂上行旧的续租请求后，WAN 不再经 MQTT 下发租约，租约只走 HTTPS。
func TestChannelLeaseRenewNotOverMQTT(t *testing.T) {
	// 准备已认领厂和管理员会话。
	svc, tok, fid, priv := enrolledFactory(t)
	// 起通道和临时云端，测完都会关掉。
	srv, mqttAddr := startChannel(t, svc)
	// 用厂钥连上通道，同一条连接既收下行也发上行。
	cli := connectMQTT(t, mqttAddr, fid, priv)
	// 给下行留缓冲，后面看有没有租约。
	down := make(chan service.Cmd, 32)
	// 订本厂下行，坏包丢掉，缓冲满了就弃。
	sub := cli.Subscribe("wan/"+fid.String()+"/down", 1, func(_ mqtt.Client, m mqtt.Message) {
		// 接住解开的指令，坏包就丢掉。
		var cmd service.Cmd
		// 解不开就丢掉，不当成收到。
		if json.Unmarshal(m.Payload(), &cmd) != nil {
			return
		}
		// 缓冲还空就送进去，满了就丢掉。
		select {
		// 送进缓冲，后面逐条检查。
		case down <- cmd:
		// 缓冲满了就丢掉，不堵回调。
		default:
		}
	})
	// 时限内没订上就停测。
	if !sub.WaitTimeout(5*time.Second) || sub.Error() != nil {
		// 订不上下行就停测，后面的断言没有意义。
		t.Fatalf("subscribe: %v", sub.Error())
	}
	// 发一条旧版厂端的续租请求。
	renew := cli.Publish("wan/"+fid.String()+"/up", 1, false, []byte(`{"typ":"lease_renew"}`))
	// 时限内没发出就停测。
	if !renew.WaitTimeout(5*time.Second) || renew.Error() != nil {
		// 续租请求没发出去就停测。
		t.Fatalf("lease_renew: %v", renew.Error())
	}
	// 再报一次版本当哨兵，同一连接的上行按序处理。
	raw, err := json.Marshal(service.Cmd{Typ: service.CmdPresence, WebVersion: 7, WebVersionName: "7.0.0", ServiceVersion: 7, ServiceVersionName: "7.0.0"})
	// 编不出就停测。
	if err != nil {
		// 哨兵编不出就停测。
		t.Fatal(err)
	}
	// 把哨兵发上去。
	pres := cli.Publish("wan/"+fid.String()+"/up", 1, false, raw)
	// 时限内没发出就停测。
	if !pres.WaitTimeout(5*time.Second) || pres.Error() != nil {
		// 哨兵没发出去就停测。
		t.Fatalf("presence: %v", pres.Error())
	}
	// 等名录出现哨兵版本，说明续租请求已经处理过。
	deadline := time.Now().Add(3 * time.Second)
	// 记下哨兵是否已经生效。
	seen := false
	// 时限内反复看名录，没生效就再等。
	for time.Now().Before(deadline) && !seen {
		// 读名录，看哨兵版本到了没有。
		_, body := do(t, srv, "GET", "/v1/directory", tok, "")
		// 接住名录，核对版本。
		var dir service.Directory
		// 解开名录并核对哨兵版本。
		seen = json.Unmarshal([]byte(body), &dir) == nil && len(dir.Factories) == 1 && dir.Factories[0].WebVersion == 7
		// 稍等再看，给上行留处理时间。
		time.Sleep(50 * time.Millisecond)
	}
	// 哨兵一直没生效就停测，后面的断言不可信。
	if !seen {
		// 名录里没出现哨兵版本就停测。
		t.Fatal("presence sentinel not applied")
	}
	// 再等一小段，若真发了租约此时早已送到。
	wait := time.After(500 * time.Millisecond)
	// 把这段时间收到的下行逐条检查。
	for {
		// 收到下行或等到点，谁先到走谁。
		select {
		// 收到一条下行就看是不是租约。
		case c := <-down:
			// 经 MQTT 收到租约就停测。
			if c.Typ == service.CmdLease || len(c.Lease) > 0 {
				// 租约走了 MQTT 就停测。
				t.Fatalf("lease over mqtt: %+v", c)
			}
		// 到点没收到租约就算通过。
		case <-wait:
			return
		}
	}
}
