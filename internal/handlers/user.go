package handlers

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"forum-authentication/internal/repository"
)

const (
	minPasswordLength = 6
	maxUsernameLength = 40
	maxNameLength     = 60
	maxAge            = 120
)

// genderLabels — допустимые значения поля «Пол».
var genderLabels = map[string]bool{
	"male":   true,
	"female": true,
	"other":  true,
}

// RegisterHandler обрабатывает GET (форма) и POST (регистрация) на /register.
func (h *Handler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if r.Method == http.MethodGet {
		h.render(w, "register", map[string]any{"User": user, "Contacts": h.contacts(r, user)})
		return
	}
	if r.Method != http.MethodPost {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	firstName := strings.TrimSpace(r.FormValue("first_name"))
	lastName := strings.TrimSpace(r.FormValue("last_name"))
	email := strings.TrimSpace(r.FormValue("email"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	ageRaw := r.FormValue("age")
	gender := r.FormValue("gender")

	data := map[string]any{
		"User":      user,
		"Contacts":  h.contacts(r, user),
		"Email":     email,
		"Username":  username,
		"FirstName": firstName,
		"LastName":  lastName,
		"Age":       ageRaw,
		"Gender":    gender,
	}
	fail := func(msg string) {
		data["Error"] = msg
		w.WriteHeader(http.StatusBadRequest)
		h.render(w, "register", data)
	}

	if firstName == "" {
		fail("Укажите имя")
		return
	}
	if len(firstName) > maxNameLength {
		fail("Имя слишком длинное (максимум 60 символов)")
		return
	}
	if lastName == "" {
		fail("Укажите фамилию")
		return
	}
	if len(lastName) > maxNameLength {
		fail("Фамилия слишком длинная (максимум 60 символов)")
		return
	}

	age, err := strconv.Atoi(ageRaw)
	if err != nil || age < 1 || age > maxAge {
		fail("Укажите корректный возраст (от 1 до 120)")
		return
	}

	if !genderLabels[gender] {
		fail("Укажите пол")
		return
	}

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address == "" {
		fail("Некорректный email")
		return
	}
	email = parsed.Address

	if username == "" {
		fail("Никнейм не может быть пустым")
		return
	}
	if len(username) > maxUsernameLength {
		fail("Никнейм слишком длинный (максимум 40 символов)")
		return
	}
	if len(password) < minPasswordLength {
		fail("Пароль должен содержать минимум 6 символов")
		return
	}

	exists, err := h.Repo.EmailExists(email)
	if err != nil {
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}
	if exists {
		fail("Email уже занят")
		return
	}

	exists, err = h.Repo.UsernameExists(username)
	if err != nil {
		h.renderError(w, r, "Ошибка базы данных", http.StatusInternalServerError)
		return
	}
	if exists {
		fail("Никнейм уже занят")
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("handlers: hash password: %v", err)
		h.renderError(w, r, "Ошибка сервера", http.StatusInternalServerError)
		return
	}

	userID, err := h.Repo.CreateUser(repository.NewUser{
		Email:        email,
		Username:     username,
		PasswordHash: string(hashed),
		FirstName:    firstName,
		LastName:     lastName,
		Age:          age,
		Gender:       gender,
	})
	if err != nil {
		log.Printf("handlers: create user: %v", err)
		h.renderError(w, r, "Ошибка сервера", http.StatusInternalServerError)
		return
	}

	if err := h.createSession(w, userID); err != nil {
		log.Printf("handlers: create session: %v", err)
		h.renderError(w, r, "Ошибка сервера", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// LoginHandler обрабатывает GET (форма) и POST (вход) на /login.
func (h *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(r)
	if r.Method == http.MethodGet {
		h.render(w, "login", map[string]any{"User": user, "Contacts": h.contacts(r, user)})
		return
	}
	if r.Method != http.MethodPost {
		h.renderError(w, r, "Метод не разрешён", http.StatusMethodNotAllowed)
		return
	}

	email := strings.TrimSpace(r.FormValue("login"))
	password := r.FormValue("password")

	data := map[string]any{"Login": email, "User": user, "Contacts": h.contacts(r, user)}
	fail := func() {
		data["Error"] = "Неверный логин или пароль"
		w.WriteHeader(http.StatusUnauthorized)
		h.render(w, "login", data)
	}

	id, hashed, err := h.Repo.UserCredentialsByLogin(email)
	if errors.Is(err, repository.ErrNotFound) {
		fail()
		return
	}
	if err != nil {
		log.Printf("handlers: get credentials: %v", err)
		h.renderError(w, r, "Ошибка сервера", http.StatusInternalServerError)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(hashed), []byte(password)) != nil {
		fail()
		return
	}

	if err := h.createSession(w, id); err != nil {
		log.Printf("handlers: create session: %v", err)
		h.renderError(w, r, "Ошибка сервера", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// LogoutHandler удаляет сессию и куку.
func (h *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookie); err == nil {
		if err := h.Repo.DeleteSession(cookie.Value); err != nil {
			log.Printf("handlers: delete session: %v", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// createSession создаёт токен, сохраняет сессию и выставляет куку.
func (h *Handler) createSession(w http.ResponseWriter, userID int) error {
	token := uuid.New().String()
	expiry := time.Now().Add(loginSessionTTL)

	if err := h.Repo.DeleteSessionsByUser(userID); err != nil {
		return err
	}
	if err := h.Repo.CreateSession(userID, token, expiry); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Expires:  expiry,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}
