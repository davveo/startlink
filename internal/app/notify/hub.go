package notify

import (
	"encoding/json"
	"sync"

	"github.com/starlink/push/internal/domain"
)

const RedisChannel = "starlink:notify:events"

// Event SSE / Redis 推送载荷
type Event struct {
	Type         string               `json:"type"` // notification | unread
	UnreadCount  int64                `json:"unread_count"`
	Notification *domain.Notification `json:"notification,omitempty"`
}

// Hub 进程内多客户端 fan-out（单 API 实例）
type Hub struct {
	mu      sync.RWMutex
	clients map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan Event]struct{})}
}

// Subscribe 注册客户端；返回只读 channel 与取消函数
func (h *Hub) Subscribe() (<-chan Event, func()) {
	if h == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan Event, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
		close(ch)
	}
}

// Broadcast 向所有已连接客户端推送；慢客户端丢弃（不阻塞）
func (h *Hub) Broadcast(evt Event) {
	if h == nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- evt:
		default:
		}
	}
}

func EncodeEvent(evt Event) ([]byte, error) {
	return json.Marshal(evt)
}

func DecodeEvent(b []byte) (Event, error) {
	var evt Event
	err := json.Unmarshal(b, &evt)
	return evt, err
}
