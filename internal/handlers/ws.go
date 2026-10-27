package handlers

import (
	"log"
	"net/http"
	"time"

	"forum-authentication/internal/chat"

	"github.com/gorilla/websocket"
)

// upgrader настраивает переход HTTP-соединения на WebSocket.
// Origin не проверяется: сайт и сокет лежат на одном хосте.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// maxWSMessageSize — ограничение размера входящего сообщения.
const maxWSMessageSize = 512

// WebSocketHandler поднимает WebSocket-соединение для авторизованного
// пользователя и регистрирует его в хабе, чтобы получать события чата.
func (h *Handler) WebSocketHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	user := h.currentUser(r)
	if user == nil {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("handlers: ws upgrade: %v", err)
		return
	}

	client := h.Chat.Register(user.ID)
	go client.WritePump(conn)

	defer func() {
		h.Chat.Unregister(client)
		conn.Close()
	}()

	conn.SetReadLimit(maxWSMessageSize)
	conn.SetReadDeadline(time.Now().Add(chat.PongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(chat.PongWait))
		return nil
	})

	for {
		// Сообщения от клиента здесь игнорируются; цикл чтения нужен,
		// чтобы ловить закрытие соединения и обновлять дедлайн.
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
