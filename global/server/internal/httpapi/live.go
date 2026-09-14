package httpapi

import (
	"context"
	"errors"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"wmesh/global/internal/platform/domain"
)

// 一家厂当前钉死的连接世代。
type liveSlot struct {
	gen  uint64 // 世代号，旧连接断开不得覆盖新连接
	conn *websocket.Conn
}

// liveConns 记住每家厂当前钉死的那条连接，避免旧连接断开把新连接打成离线。
type liveConns struct {
	mu    sync.Mutex
	slots map[uuid.UUID]*liveSlot
	wait  map[string]chan channelMsg // 厂+问询号，等通道回执
}

// 空的在线连接表。
func newLiveConns() *liveConns {
	return &liveConns{slots: map[uuid.UUID]*liveSlot{}, wait: map[string]chan channelMsg{}}
}

// 厂身份加问询号。
func waitKey(id uuid.UUID, reqID string) string {
	return id.String() + "/" + reqID
}

// 换世代钉死新连接，旧世代断开不再标离线。
func (l *liveConns) acquire(id uuid.UUID) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	next := uint64(1)
	if s, ok := l.slots[id]; ok {
		next = s.gen + 1
	}
	l.slots[id] = &liveSlot{gen: next}
	return next
}

// 只把当前世代的套接字挂上，过期世代丢掉。
func (l *liveConns) attach(id uuid.UUID, gen uint64, conn *websocket.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.slots[id]
	if !ok || s.gen != gen {
		return
	}
	s.conn = conn
}

// 往钉死连接写一帧；没有连接就丢掉。
func (l *liveConns) push(ctx context.Context, id uuid.UUID, msg channelMsg) bool {
	l.mu.Lock()
	s := l.slots[id]
	var conn *websocket.Conn
	if s != nil {
		conn = s.conn
	}
	l.mu.Unlock()
	if conn == nil {
		return false
	}
	return wsjson.Write(ctx, conn, msg) == nil
}

// call 经钉死通道问厂端，等带同一问询号的回执。
func (l *liveConns) call(ctx context.Context, id uuid.UUID, req channelMsg) (channelMsg, error) {
	if req.ReqID == "" {
		req.ReqID = uuid.NewString()
	}
	ch := make(chan channelMsg, 1)
	if !l.park(id, req.ReqID, ch) {
		return channelMsg{}, domain.ErrFactoryOffline
	}
	defer l.unpark(id, req.ReqID)
	if !l.push(ctx, id, req) {
		return channelMsg{}, domain.ErrFactoryOffline
	}
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return channelMsg{}, ctx.Err()
		}
		return channelMsg{}, domain.ErrFactoryOffline
	case msg := <-ch:
		if msg.Typ == "error" {
			return channelMsg{}, mapChannelErr(msg.Error)
		}
		return msg, nil
	}
}

// 登记等回执的槽位；通道不在则失败。
func (l *liveConns) park(id uuid.UUID, reqID string, ch chan channelMsg) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.slots[id]
	if s == nil || s.conn == nil {
		return false
	}
	l.wait[waitKey(id, reqID)] = ch
	return true
}

// 问询结束清掉等槽，避免把迟到回执交给下一问。
func (l *liveConns) unpark(id uuid.UUID, reqID string) {
	l.mu.Lock()
	delete(l.wait, waitKey(id, reqID))
	l.mu.Unlock()
}

// 把回执交给对应问询；没有等槽就丢。
func (l *liveConns) deliver(id uuid.UUID, msg channelMsg) {
	if msg.ReqID == "" {
		return
	}
	l.mu.Lock()
	ch := l.wait[waitKey(id, msg.ReqID)]
	l.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

// 把厂端英文错误收回本侧业务错误。
func mapChannelErr(msg string) error {
	switch msg {
	case domain.ErrNotFound.Error():
		return domain.ErrNotFound
	case domain.ErrForbidden.Error():
		return domain.ErrForbidden
	case domain.ErrAssetNotAvailable.Error():
		return domain.ErrAssetNotAvailable
	case domain.ErrAssetNotCopyable.Error():
		return domain.ErrAssetNotCopyable
	case domain.ErrIntegrity.Error():
		return domain.ErrIntegrity
	case domain.ErrAssetDependency.Error():
		return domain.ErrAssetDependency
	case "":
		return domain.ErrFactoryOffline
	default:
		return errors.New(msg)
	}
}

// drop 关掉当前连接，让厂端回连后拿到新状态。
func (l *liveConns) drop(id uuid.UUID) {
	l.mu.Lock()
	s := l.slots[id]
	var conn *websocket.Conn
	if s != nil {
		conn = s.conn
	}
	l.mu.Unlock()
	if conn != nil {
		conn.CloseNow()
	}
}

// release 只有当前世代断开才标离线。
func (l *liveConns) release(id uuid.UUID, gen uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	s, ok := l.slots[id]
	if !ok || s.gen != gen {
		return false
	}
	delete(l.slots, id)
	return true
}
