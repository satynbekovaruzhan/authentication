package handlers

import (
	"html/template"

	"forum-authentication/internal/chat"
	"forum-authentication/internal/repository"
)

// Handler содержит зависимые от запроса зависимости: хранилище и кэш шаблонов.
type Handler struct {
	Repo      *repository.Repository
	Templates map[string]*template.Template
	Chat      *chat.Hub
}

// NewHandler создаёт обработчик.
func NewHandler(repo *repository.Repository, templates map[string]*template.Template, hub *chat.Hub) *Handler {
	return &Handler{
		Repo:      repo,
		Templates: templates,
		Chat:      hub,
	}
}
