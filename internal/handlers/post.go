package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"forum-authentication/internal/models"
	"forum-authentication/internal/repository"
)

const maxPostTitleLen = 200
const maxPostContentLen = 5000

// PostCreateHandler создаёт новый пост (только для авторизованных, метод POST).
func (h *Handler) PostCreateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, "Некорректные данные формы", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	content := r.FormValue("content")

	if len(title) > maxPostTitleLen {
		h.renderError(w, r, "Заголовок слишком длинный", http.StatusBadRequest)
		return
	}
	if content == "" {
		h.renderError(w, r, "Вы забыли написать сам пост!", http.StatusBadRequest)
		return
	}
	if len(content) > maxPostContentLen {
		h.renderError(w, r, "Текст поста слишком длинный", http.StatusBadRequest)
		return
	}

	categoryIDs, err := parseCategoryIDs(r.Form["category"])
	if err != nil {
		h.renderError(w, r, "Некорректная категория", http.StatusBadRequest)
		return
	}

	if _, err := h.Repo.CreatePost(user.ID, title, content, categoryIDs); err != nil {
		log.Printf("handlers: create post: %v", err)
		h.renderError(w, r, "Ошибка сохранения поста", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// PostReactionHandler ставит/снимает реакцию на пост (метод POST).
func (h *Handler) PostReactionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	postID, err := strconv.Atoi(r.FormValue("post_id"))
	if err != nil {
		h.renderError(w, r, "Некорректный идентификатор поста", http.StatusBadRequest)
		return
	}
	vote, ok := parseVote(r.FormValue("vote"))
	if !ok {
		h.renderError(w, r, "Некорректный голос", http.StatusBadRequest)
		return
	}

	counts, err := h.Repo.SetPostReaction(user.ID, postID, vote)
	if err != nil {
		log.Printf("handlers: set post reaction: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}
	h.broadcastReaction(counts)

	redirectBack(w, r)
}

// PostViewHandler показывает пост и его комментарии (метод GET).
func (h *Handler) PostViewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		h.renderError(w, r, "Пост не найден", http.StatusNotFound)
		return
	}

	post, err := h.Repo.PostByID(id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.renderError(w, r, "Пост не найден", http.StatusNotFound)
			return
		}
		log.Printf("handlers: post by id: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	comments, err := h.Repo.CommentsByPost(id)
	if err != nil {
		log.Printf("handlers: comments by post: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}

	user := h.currentUser(r)
	data := models.TemplateData{
		User:     user,
		Contacts: h.contacts(r, user),
		Post:     *post,
		Comments: comments,
	}
	h.render(w, "post_view", data)
}

// parseCategoryIDs преобразует строковые id категорий в числа и проверяет их.
func parseCategoryIDs(rawIDs []string) ([]int, error) {
	ids := make([]int, 0, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := strconv.Atoi(raw)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
