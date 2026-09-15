package hub

import (
	"context"

	"github.com/google/uuid"

	"wmesh/factory/internal/platform/clientmqtt"
	"wmesh/factory/internal/service"
)

// clientBroker 本厂 Client MQTT 的最小面，测试可替换。
type clientBroker interface {
	Publish(factoryID, clientID uuid.UUID, payload []byte) error
	Addr() string
	Close() error
}

// StartClientBroker 起本厂 Client MQTT；一机只能订自己的 down。
func (h *Hub) StartClientBroker(addr string) error {
	bus, err := clientmqtt.Listen(addr, clientmqtt.Hooks{
		Auth: func(factoryID, clientID uuid.UUID, token string) error {
			svc, err := h.Service(context.Background(), factoryID)
			if err != nil {
				return err
			}
			return svc.Node.AuthClientMQTT(context.Background(), clientID, token)
		},
		Up: func(factoryID, clientID uuid.UUID, payload []byte) {
			svc, err := h.Service(context.Background(), factoryID)
			if err != nil {
				return
			}
			svc.Closure.HandleClientUp(context.Background(), clientID, payload)
		},
	})
	if err != nil {
		return err
	}
	h.clientMu.Lock()
	old := h.clientBus
	h.clientBus = bus
	h.clientMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// ClientMQTTAddr 实际监听地址，给登录回给平板。
func (h *Hub) ClientMQTTAddr() string {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	if h.clientBus == nil {
		return ""
	}
	return h.clientBus.Addr()
}

// PublishDown 向指定本机投控制面小信封。
func (h *Hub) PublishDown(factoryID, clientID uuid.UUID, payload []byte) {
	if bus := h.clientDown(); bus != nil {
		_ = bus.Publish(factoryID, clientID, payload)
	}
}

// clientDown 当前 Broker；未起则空。
func (h *Hub) clientDown() clientBroker {
	h.clientMu.Lock()
	defer h.clientMu.Unlock()
	return h.clientBus
}

var _ service.ClientDown = (*Hub)(nil)
