package server

import (
	"html/template"
	"net/http"
	"time"

	"forum-authentication/internal/chat"
	"forum-authentication/internal/handlers"
	"forum-authentication/internal/repository"
)

// New собирает mux с маршрутами приложения.
// templates — кэш шаблонов, repo — хранилище данных,
// hub — хаб WebSocket-соединений чата,
// staticDir — каталог со статическими файлами.
func New(templates map[string]*template.Template, repo *repository.Repository, hub *chat.Hub, staticDir string) http.Handler {
	h := handlers.NewHandler(repo, templates, hub)

	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static", fileServer))

	mux.HandleFunc("/auth/{provider}", h.OAuthStart)
	mux.HandleFunc("/auth/{provider}/callback", h.OAuthCallback)

	mux.HandleFunc("/", h.HomeHandler)
	mux.HandleFunc("/register", h.RegisterHandler)
	mux.HandleFunc("/login", h.LoginHandler)
	mux.HandleFunc("/logout", h.LogoutHandler)
	mux.HandleFunc("/post/create", h.PostCreateHandler)
	mux.HandleFunc("/post/reaction", h.PostReactionHandler)
	mux.HandleFunc("/post/view", h.PostViewHandler)
	mux.HandleFunc("/comment/create", h.CommentCreateHandler)
	mux.HandleFunc("/comment/reaction", h.CommentReactionHandler)
	mux.HandleFunc("/messages", h.MessagesHandler)
	mux.HandleFunc("/messages/send", h.SendMessageHandler)
	mux.HandleFunc("/ws", h.WebSocketHandler)

	return presence(repo, mux)
}

// presence обновляет last_seen текущего пользователя для каждого запроса,
// если он авторизован (имействует валидную сессию).
func presence(repo *repository.Repository, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(handlers.SessionCookie); err == nil {
			_ = repo.TouchLastSeen(cookie.Value, time.Now())
		}
		next.ServeHTTP(w, r)
	})
}
