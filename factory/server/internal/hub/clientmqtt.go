package hub

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/platform/domain"
	"wmesh/factory/internal/service"
)

// clientBroker 本厂 Client MQTT 的最小面，测试可替换。
type clientBroker interface {
	// 向指定设备投一条下行，失败由调用方处理。
	Publish(factoryID, clientID uuid.UUID, payload []byte) error
	// 断开这个人在本厂的全部连接，交回断了几条。
	DropPerson(factoryID, personID uuid.UUID) int
	// 返回实际监听地址，给登录回给平板。
	Addr() string
	// 关掉监听，替换或退出时调用。
	Close() error
}

// StartClientBroker 起本厂 Client MQTT；一机只能订自己的 down。
func (h *Hub) StartClientBroker(addr string) error {
	// 拉起本厂设备通道，口令不对的设备订不上。
	bus, err := clientmqtt.Listen(addr, clientmqtt.Hooks{
		// 用设备口令核对，对不上就拒绝订阅。
		Auth: func(factoryID, clientID uuid.UUID, token string) (uuid.UUID, error) {
			// 按工厂打开本厂服务，打不开就拒绝这台设备。
			svc, err := h.Service(context.Background(), factoryID)
			// 厂还没建好就拒绝连接，避免空库认证；库打不开算暂不可用，不当令牌失效。
			if err != nil {
				// 分清令牌失效和暂不可用再交回。
				return uuid.Nil, mqttAuthErr(err)
			}
			// 核对口令并取出账号，对不上就拒绝这台设备。
			acc, err := svc.Node.AuthClientMQTT(context.Background(), clientID, token)
			// 口令不对就拒绝，不能订别人的下行；租约没了、库抖动只算暂不可用。
			if err != nil {
				// 分清令牌失效和暂不可用再交回。
				return uuid.Nil, mqttAuthErr(err)
			}
			return acc.ID, nil
		},
		// 连上就记这台平板在线，名录可以显示。
		Online: func(factoryID, personID uuid.UUID) {
			// 按工厂打开服务，打不开就当没连上。
			svc, err := h.Service(context.Background(), factoryID)
			// 厂库打不开就不记在线，也不拆监听。
			if err != nil {
				return
			}
			// 记下这台平板在线，名录可以显示。
			svc.Node.NoteAppMQTT(context.Background(), personID, true)
		},
		// 断开就记离线，避免名录一直显示在线。
		Offline: func(factoryID, personID uuid.UUID) {
			// 按工厂打开服务，打不开就当没断开过。
			svc, err := h.Service(context.Background(), factoryID)
			// 厂库打不开就不改在线状态，也不拆监听。
			if err != nil {
				return
			}
			// 记下这台平板离线，避免一直显示在线。
			svc.Node.NoteAppMQTT(context.Background(), personID, false)
		},
		// 上行先给平板自己，再给设备控制回执。
		Up: func(factoryID, clientID, personID uuid.UUID, payload []byte) {
			// 按工厂打开服务，打不开就丢掉这条上行。
			svc, err := h.Service(context.Background(), factoryID)
			// 厂库打不开就丢弃，不断掉别的设备。
			if err != nil {
				return
			}
			// 先处理平板自己的上行，设备用会话里鉴权过的本机。
			svc.Node.HandleAppUp(context.Background(), personID, clientID, payload)
			// 再处理设备上的控制回执，审计记到会话里的登录人。
			svc.Closure.HandleClientUp(context.Background(), personID, clientID, payload)
		},
	})
	// 设备通道起不来就交回，进程不要假装已经在听。
	if err != nil {
		return err
	}
	// 占住设备通道，避免替换时还有人在发。
	h.clientMu.Lock()
	// 记住旧的，新的换上后再关旧的。
	old := h.clientBus
	// 换上新的监听，后面的下行走这里。
	h.clientBus = bus
	// 换完就放开，下发不必一直等。
	h.clientMu.Unlock()
	// 有旧监听就关掉，避免同一端口留两份。
	if old != nil {
		// 关掉旧监听，关失败也不影响新的。
		_ = old.Close()
	}
	return nil
}

// ClientMQTTAddr 实际监听地址，给登录回给平板。
func (h *Hub) ClientMQTTAddr() string {
	// 读地址前先占住，避免读到换到一半的通道。
	h.clientMu.Lock()
	// 读完就放开设备通道锁。
	defer h.clientMu.Unlock()
	// 还没拉起就回空，登录方知道现在没有通道。
	if h.clientBus == nil {
		return ""
	}
	// 把实际监听地址交回，平板按它去连。
	return h.clientBus.Addr()
}

// PublishDown 向指定本机投控制面小信封。
func (h *Hub) PublishDown(factoryID, clientID uuid.UUID, payload []byte) {
	// 通道还在才投，没拉起就丢掉这封。
	if bus := h.clientDown(); bus != nil {
		// 投下行，失败由设备侧超时再取，这里不阻塞。
		_ = bus.Publish(factoryID, clientID, payload)
	}
}

// DropPerson 断开这个人在本厂的示教器连接；停用、重置密码、收回角色后调，示教器重连被拒即登出。
func (h *Hub) DropPerson(factoryID, personID uuid.UUID) {
	// 通道还在才断，没拉起就没有连接可断。
	if bus := h.clientDown(); bus != nil {
		// 按人断开，断了几条不影响调用方。
		_ = bus.DropPerson(factoryID, personID)
	}
}

// mqttAuthErr 把 CONNECT 鉴权失败分成三类：令牌没钉这台设备包成设备不符；令牌失效、账号停用、设备作废、厂停用注销照原样拒，示教器登出；
// 其余（租约没领到或到期、库连不上、厂库打不开）包成暂不可用，示教器只重试，WAN 回来后自动连上。
func mqttAuthErr(err error) error {
	// 令牌没钉这台设备（D-59）：令牌本身还有效，单独标出来，回「服务不可用」不让示教器登出。
	if errors.Is(err, domain.ErrSessionClientMismatch) {
		// 包上设备不符的标记再交回。
		return fmt.Errorf("%w: %w", clientmqtt.ErrClientMismatch, err)
	}
	// 这些是这个人或这台设备真的不能再用，照原样拒。
	for _, dead := range []error{
		// 令牌、账号、设备、厂停用注销这几类。
		domain.ErrUnauthorized, domain.ErrSessionExpired, domain.ErrAccountDisabled, domain.ErrForbidden,
		domain.ErrBindingVoid, domain.ErrFactoryDisabled, domain.ErrFactoryRetired,
	} {
		// 命中就照原样交回。
		if errors.Is(err, dead) {
			return err
		}
	}
	// 其余都算厂暂不可用，包上暂不可用的标记。
	return fmt.Errorf("%w: %w", clientmqtt.ErrUnavailable, err)
}

// clientDown 当前 Broker；未起则空。
func (h *Hub) clientDown() clientBroker {
	// 取通道前先占住，避免拿到换到一半的监听。
	h.clientMu.Lock()
	// 取完就放开设备通道锁。
	defer h.clientMu.Unlock()
	return h.clientBus
}

// 编译期确认本装配能向设备下发。
var _ service.ClientDown = (*Hub)(nil)
