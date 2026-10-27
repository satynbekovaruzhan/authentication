package handlers

import (
	"log"
	"net/http"
	"strconv"

	"forum-authentication/internal/models"
)

// HomeHandler отдаёт главную страницу со списком постов.
// Поддерживает фильтры: ?category=N, ?filter=created, ?filter=liked.
func (h *Handler) HomeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		h.renderError(w, r, "Страница не найдена", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	user := h.currentUser(r)

	categories, err := h.Repo.Categories()
	if err != nil {
		log.Printf("handlers: categories: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	rawCategory := r.URL.Query().Get("category")
	categoryID, _ := strconv.Atoi(rawCategory)

	filter := r.URL.Query().Get("filter")
	filterSpec := models.PostFilter{CategoryID: categoryID}
	switch filter {
	case "created":
		if user != nil {
			filterSpec.AuthorID = user.ID
		}
	case "liked":
		if user != nil {
			filterSpec.LikedBy = user.ID
		}
	}

	posts, err := h.Repo.Posts(filterSpec)
	if err != nil {
		log.Printf("handlers: posts: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	data := models.TemplateData{
		User:           user,
		Contacts:       h.contacts(r, user),
		Posts:          posts,
		Categories:     categories,
		ActiveFilter:   filter,
		ActiveCategory: categoryID,
	}
	h.render(w, "index", data)
}
