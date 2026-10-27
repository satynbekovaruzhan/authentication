package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"forum-authentication/internal/chat"
	"forum-authentication/internal/models"
	"forum-authentication/internal/repository"
)

// SessionCookie — имя куки с токеном сессии.
const SessionCookie = "session_token"

// loginSessionTTL — срок жизни сессии.
const loginSessionTTL = 24 * time.Hour

// currentUser возвращает пользователя по куке сессии или nil, если он не авторизован.
func (h *Handler) currentUser(r *http.Request) *models.User {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil
	}

	user, err := h.Repo.UserBySession(cookie.Value, time.Now())
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			log.Printf("handlers: currentUser: %v", err)
		}
		return nil
	}
	return user
}

// requireUser возвращает авторизованного пользователя; если его нет —
// отвечает редиректом на /login и возвращает ok=false.
func (h *Handler) requireUser(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	user := h.currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil, false
	}
	return user, true
}

// contacts возвращает список контактов чата для авторизованного пользователя.
func (h *Handler) contacts(r *http.Request, user *models.User) []models.Contact {
	if user == nil {
		return nil
	}
	list, err := h.Repo.Contacts(user.ID, time.Now())
	if err != nil {
		log.Printf("handlers: contacts: %v", err)
		return nil
	}
	return list
}

// render выполняет шаблон по имени и пишет результат в ответ.
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	ts, ok := h.Templates[name]
	if !ok {
		log.Printf("handlers: template %q not found", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := ts.Execute(w, data); err != nil {
		log.Printf("handlers: execute template %q: %v", name, err)
	}
}

// renderError отдаёт страницу ошибки с заданным HTTP-статусом.
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, message string, code int) {
	user := h.currentUser(r)
	w.WriteHeader(code)
	h.render(w, "error", struct {
		Code       int
		Message    string
		User       *models.User
		Contacts   []models.Contact
		ActiveWith int
	}{
		Code:     code,
		Message:  message,
		User:     user,
		Contacts: h.contacts(r, user),
	})
}

// broadcastReaction рассылает новые счётчики реакций всем подключённым по WebSocket,
// чтобы лайки обновлялись без перезагрузки страницы.
func (h *Handler) broadcastReaction(counts models.ReactionCounts) {
	h.Chat.BroadcastAll(chat.Event{Type: chat.EventReaction, Reaction: &counts})
}

// parseVote разбирает значение голоса из формы ("1" или "-1").
func parseVote(raw string) (int, bool) {
	vote, err := strconv.Atoi(raw)
	if err != nil || (vote != 1 && vote != -1) {
		return 0, false
	}
	return vote, true
}

// redirectBack возвращает на страницу, с которой был отправлен запрос,
// либо на главную, если источник неизвестен.
func redirectBack(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("Referer")
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
