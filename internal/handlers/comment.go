package handlers

import (
	"log"
	"net/http"
	"strconv"
)

const maxCommentLen = 500

// CommentCreateHandler сохраняет новый комментарий (метод POST).
func (h *Handler) CommentCreateHandler(w http.ResponseWriter, r *http.Request) {
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
	content := r.FormValue("content")

	if content == "" {
		h.renderError(w, r, "Комментарий не может быть пустым", http.StatusBadRequest)
		return
	}
	if len(content) > maxCommentLen {
		h.renderError(w, r, "Комментарий слишком длинный (максимум 500 символов)", http.StatusBadRequest)
		return
	}

	if err := h.Repo.CreateComment(postID, user.ID, content); err != nil {
		log.Printf("handlers: create comment: %v", err)
		h.renderError(w, r, "Ошибка сохранения комментария", http.StatusInternalServerError)
		return
	}

	target := "/post/view?id=" + strconv.Itoa(postID)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// CommentReactionHandler ставит/снимает реакцию на комментарий (метод POST).
func (h *Handler) CommentReactionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	user, ok := h.requireUser(w, r)
	if !ok {
		return
	}

	commentID, err := strconv.Atoi(r.FormValue("comment_id"))
	if err != nil {
		h.renderError(w, r, "Некорректный идентификатор комментария", http.StatusBadRequest)
		return
	}
	vote, ok := parseVote(r.FormValue("vote"))
	if !ok {
		h.renderError(w, r, "Некорректный голос", http.StatusBadRequest)
		return
	}

	counts, err := h.Repo.SetCommentReaction(user.ID, commentID, vote)
	if err != nil {
		log.Printf("handlers: set comment reaction: %v", err)
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}
	h.broadcastReaction(counts)

	redirectBack(w, r)
}
