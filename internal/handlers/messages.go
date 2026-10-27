package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"forum-authentication/internal/chat"
	"forum-authentication/internal/models"
	"forum-authentication/internal/repository"
)

const maxMessageLen = 1000

// messagePageSize — количество сообщений, показываемых за один раз.
// Следующая страница догружается при прокрутке вверх (см. realtime.js).
const messagePageSize = 10

// MessagesHandler показывает список контактов чата (кто онлайн/не в сети)
// и переписку с выбранным пользователем (метод GET, только для авторизованных).
// При наличии параметра before (id самого старого показанного сообщения)
// отвечает JSON-страницей более старых сообщений для подгрузки при прокрутке.
func (h *Handler) MessagesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	otherID, _ := strconv.Atoi(r.URL.Query().Get("with"))

	// Подгрузка старых сообщений через JSON (прокрутка вверх).
	if raw := r.URL.Query().Get("before"); raw != "" {
		beforeID, err := strconv.Atoi(raw)
		if err != nil || beforeID <= 0 || otherID <= 0 {
			h.renderError(w, r, "Некорректные параметры подгрузки", http.StatusBadRequest)
			return
		}
		messages, err := h.Repo.MessagePage(user.ID, otherID, beforeID, messagePageSize)
		if err != nil {
			log.Printf("handlers: message page: %v", err)
			h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(messages); err != nil {
			log.Printf("handlers: encode messages: %v", err)
		}
		return
	}

	contacts, err := h.Repo.Contacts(user.ID, time.Now())
	if err != nil {
		log.Printf("handlers: contacts: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	users, err := h.Repo.UsersExcept(user.ID)
	if err != nil {
		log.Printf("handlers: users: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	data := struct {
		User       *models.User
		Contacts   []models.Contact
		Users      []models.User
		With       *models.User
		WithOnline bool
		Messages   []models.Message
		ActiveWith int
		HasMore    bool
		OldestID   int
	}{
		User:       user,
		Contacts:   contacts,
		Users:      users,
		With:       nil,
		Messages:   []models.Message{},
		ActiveWith: otherID,
		HasMore:    false,
		OldestID:   0,
	}

	if otherID != 0 {
		otherUser, err := h.Repo.UserByID(otherID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				h.renderError(w, r, "Пользователь не найден", http.StatusNotFound)
				return
			}
			log.Printf("handlers: user by id: %v", err)
			h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
			return
		}
		messages, err := h.Repo.MessagePage(user.ID, otherID, 0, messagePageSize+1)
		if err != nil {
			log.Printf("handlers: message page: %v", err)
			h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
			return
		}
		if len(messages) > messagePageSize {
			data.HasMore = true
			messages = messages[len(messages)-messagePageSize:]
		}
		if len(messages) > 0 {
			data.OldestID = messages[0].ID
		}
		data.With = otherUser
		data.WithOnline = isOnline(otherUser, contacts)
		data.Messages = messages
	}

	h.render(w, "messages", data)
}

// isOnline определяет онлайн-статус собеседника по списку контактов.
func isOnline(other *models.User, contacts []models.Contact) bool {
	for _, c := range contacts {
		if c.User.ID == other.ID {
			return c.Online
		}
	}
	return false
}

// SendMessageHandler отправляет личное сообщение (метод POST).
func (h *Handler) SendMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	receiverID, err := strconv.Atoi(r.FormValue("receiver_id"))
	if err != nil {
		h.renderError(w, r, "Некорректный идентификатор получателя", http.StatusBadRequest)
		return
	}
	if receiverID == user.ID {
		h.renderError(w, r, "Нельзя отправить сообщение самому себе", http.StatusBadRequest)
		return
	}

	if _, err := h.Repo.UserByID(receiverID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.renderError(w, r, "Получатель не найден", http.StatusBadRequest)
			return
		}
		log.Printf("handlers: check receiver: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	content := r.FormValue("content")
	if content == "" {
		h.renderError(w, r, "Сообщение не может быть пустым", http.StatusBadRequest)
		return
	}
	if len(content) > maxMessageLen {
		h.renderError(w, r, "Сообщение слишком длинное (максимум 1000 символов)", http.StatusBadRequest)
		return
	}

	id, err := h.Repo.CreateMessage(user.ID, receiverID, content)
	if err != nil {
		log.Printf("handlers: create message: %v", err)
		h.renderError(w, r, "Ошибка отправки сообщения", http.StatusInternalServerError)
		return
	}

	// Мгновенно сообщаем получателю о новом сообщении (WebSocket).
	if msg, err := h.Repo.MessageByID(id); err != nil {
		log.Printf("handlers: message by id: %v", err)
	} else {
		h.Chat.Broadcast(receiverID, chat.Event{Type: chat.EventMessage, Message: &msg})
	}

	target := "/messages?with=" + strconv.Itoa(receiverID)
	http.Redirect(w, r, target, http.StatusSeeOther)
}
