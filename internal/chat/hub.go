package chat

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"forum-authentication/internal/models"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	PongWait   = 60 * time.Second
	pingPeriod = (PongWait * 9) / 10
	sendBuffer = 16
)

// Типы событий (поле Event.Type).
const (
	EventMessage  = "message"  // новое личное сообщение, заполнено Message
	EventReaction = "reaction" // изменились счётчики реакций, заполнено Reaction
)

// Event — событие, отправляемое клиенту по WebSocket.
// Заполнено только поле, соответствующее Type.
type Event struct {
	Type     string                 `json:"type"`
	Message  *models.Message        `json:"message,omitempty"`
	Reaction *models.ReactionCounts `json:"reaction,omitempty"`
}

// Client — одно WebSocket-соединение пользователя.
type Client struct {
	UserID int
	send   chan []byte
	once   sync.Once
}

func (c *Client) close() {
	c.once.Do(func() { close(c.send) })
}

// Hub хранит активные WebSocket-соединения всех пользователей.
type Hub struct {
	mu    sync.Mutex
	users map[int]map[*Client]struct{}
}

// NewHub создаёт пустой хаб.
func NewHub() *Hub {
	return &Hub{users: map[int]map[*Client]struct{}{}}
}

// Register регистрирует новое соединение пользователя.
func (h *Hub) Register(userID int) *Client {
	c := &Client{UserID: userID, send: make(chan []byte, sendBuffer)}
	h.mu.Lock()
	if h.users[userID] == nil {
		h.users[userID] = map[*Client]struct{}{}
	}
	h.users[userID][c] = struct{}{}
	h.mu.Unlock()
	return c
}

// Unregister удаляет соединение и закрывает его канал.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	if set := h.users[c.UserID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.users, c.UserID)
		}
	}
	h.mu.Unlock()
	c.close()
}

// Broadcast отправляет событие всем соединениям пользователя userID.
func (h *Hub) Broadcast(userID int, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("chat: marshal event: %v", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	sendAll(h.users[userID], data)
}

// BroadcastAll отправляет событие всем подключённым пользователям.
// Используется для публичных данных (счётчики реакций): клиент сам решает,
// есть ли на его странице затронутый пост или комментарий.
func (h *Hub) BroadcastAll(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("chat: marshal event: %v", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.users {
		sendAll(set, data)
	}
}

// sendAll кладёт data в очередь каждого соединения из set.
// Медленные соединения пропускаются, чтобы не блокировать отправителя.
// Вызывать под h.mu.
func sendAll(set map[*Client]struct{}, data []byte) {
	for c := range set {
		select {
		case c.send <- data:
		default:
		}
	}
}

// WritePump пишет исходящие события и периодически пингует соединение.
func (c *Client) WritePump(conn *websocket.Conn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
