package handlers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"forum-authentication/internal/chat"
	"forum-authentication/internal/models"
	"forum-authentication/internal/repository"
	"forum-authentication/internal/server"
	"forum-authentication/internal/templating"

	"github.com/gorilla/websocket"
)

type testEnv struct {
	Server http.Handler
	Repo   *repository.Repository
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	repo, err := repository.Open(":memory:")
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })

	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")

	templates, err := templating.Load(filepath.Join(root, "ui", "templates"))
	if err != nil {
		t.Fatalf("templating.Load: %v", err)
	}

	mux := server.New(templates, repo, chat.NewHub(), filepath.Join(root, "ui", "static"))
	return &testEnv{Server: mux, Repo: repo}
}

func doRequest(env *testEnv, method, path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	env.Server.ServeHTTP(rec, req)
	return rec
}

func registerUser(t *testing.T, env *testEnv, email, username, password string) []*http.Cookie {
	t.Helper()
	form := url.Values{
		"email":      {email},
		"username":   {username},
		"password":   {password},
		"first_name": {"Test"},
		"last_name":  {"User"},
		"age":        {"22"},
		"gender":     {"male"},
	}
	rec := doRequest(env, http.MethodPost, "/register", form, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("register %s: status = %d, want 303", email, rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("register %s: no session cookie set", email)
	}
	return cookies
}

func TestRegisterLoginLogout(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{
		"email":      {"alice@test.com"},
		"username":   {"alice"},
		"password":   {"secret123"},
		"first_name": {"Алиса"},
		"last_name":  {"Иванова"},
		"age":        {"25"},
		"gender":     {"female"},
	}

	rec := doRequest(env, http.MethodPost, "/register", form, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("register status = %d, want 303", rec.Code)
	}
	cookie := rec.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != "session_token" {
		t.Fatalf("expected session_token cookie, got %v", cookie)
	}

	rec = doRequest(env, http.MethodGet, "/", nil, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("home status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Алиса Иванова") {
		t.Fatalf("home page should show profile name 'Алиса Иванова'")
	}

	rec = doRequest(env, http.MethodPost, "/logout", nil, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want 303", rec.Code)
	}

	rec = doRequest(env, http.MethodGet, "/", nil, nil)
	if strings.Contains(rec.Body.String(), "Алиса Иванова") {
		t.Fatalf("home page should not show profile name after logout")
	}
}

func TestRegisterDuplicates(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{
		"email":      {"alice@test.com"},
		"username":   {"alice"},
		"password":   {"secret123"},
		"first_name": {"Алиса"},
		"last_name":  {"Иванова"},
		"age":        {"25"},
		"gender":     {"female"},
	}
	doRequest(env, http.MethodPost, "/register", form, nil)

	dupe := url.Values{
		"email":      {"alice@test.com"},
		"username":   {"other"},
		"password":   {"secret123"},
		"first_name": {"Боб"},
		"last_name":  {"Смирнов"},
		"age":        {"30"},
		"gender":     {"male"},
	}
	rec := doRequest(env, http.MethodPost, "/register", dupe, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate email status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Email уже занят") {
		t.Fatalf("expected 'Email уже занят' message, got: %s", rec.Body.String())
	}
}

func TestRegisterValidation(t *testing.T) {
	env := newTestEnv(t)

	valid := url.Values{
		"email":      {"alice@test.com"},
		"username":   {"alice"},
		"password":   {"secret123"},
		"first_name": {"Алиса"},
		"last_name":  {"Иванова"},
		"age":        {"25"},
		"gender":     {"female"},
	}

	cases := []struct {
		name     string
		override url.Values
		wantHint string
	}{
		{name: "missing first name", override: url.Values{"first_name": {""}}, wantHint: "Укажите имя"},
		{name: "missing last name", override: url.Values{"last_name": {""}}, wantHint: "Укажите фамилию"},
		{name: "missing age", override: url.Values{"age": {"0"}}, wantHint: "Укажите корректный возраст"},
		{name: "invalid gender", override: url.Values{"gender": {""}}, wantHint: "Укажите пол"},
		{name: "bad email", override: url.Values{"email": {"not-an-email"}}, wantHint: "Некорректный email"},
		{name: "short password", override: url.Values{"password": {"123"}}, wantHint: "Пароль должен содержать минимум 6 символов"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			for k, v := range valid {
				form[k] = append([]string(nil), v...)
			}
			for k, v := range tc.override {
				form[k] = v
			}
			rec := doRequest(env, http.MethodPost, "/register", form, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantHint) {
				t.Fatalf("expected hint %q, got body: %s", tc.wantHint, rec.Body.String())
			}
		})
	}
}

func TestLoginWrongPassword(t *testing.T) {
	env := newTestEnv(t)
	registerUser(t, env, "alice@test.com", "alice", "secret123")

	form := url.Values{"login": {"alice@test.com"}, "password": {"wrong-pass"}}
	rec := doRequest(env, http.MethodPost, "/login", form, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Неверный логин или пароль") {
		t.Fatalf("expected error hint, got: %s", rec.Body.String())
	}
}

func TestLoginByEmailAndUsername(t *testing.T) {
	env := newTestEnv(t)
	registerUser(t, env, "alice@test.com", "alice", "secret123")

	// Вход по email
	rec := doRequest(env, http.MethodPost, "/login",
		url.Values{"login": {"alice@test.com"}, "password": {"secret123"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login by email status = %d, want 303", rec.Code)
	}

	// Вход по никнейму
	rec = doRequest(env, http.MethodPost, "/login",
		url.Values{"login": {"alice"}, "password": {"secret123"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login by username status = %d, want 303", rec.Code)
	}
	if len(rec.Result().Cookies()) != 1 {
		t.Fatalf("login by username: expected session cookie")
	}
}

func TestAuthRequiredRoutesRedirect(t *testing.T) {
	env := newTestEnv(t)

	routes := []struct{ method, path string }{
		{http.MethodPost, "/post/create"},
		{http.MethodPost, "/post/reaction"},
		{http.MethodPost, "/comment/create"},
		{http.MethodPost, "/comment/reaction"},
	}

	for _, rt := range routes {
		rec := doRequest(env, rt.method, rt.path, url.Values{"post_id": {"1"}}, nil)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s %s status = %d, want 303 redirect", rt.method, rt.path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Fatalf("%s %s Location = %q, want /login", rt.method, rt.path, loc)
		}
	}
}

func TestCreatePostAndView(t *testing.T) {
	env := newTestEnv(t)
	cookies := registerUser(t, env, "alice@test.com", "alice", "secret123")

	form := url.Values{
		"title":    {"Hello forum"},
		"content":  {"My first post"},
		"category": {"1", "2"},
	}
	rec := doRequest(env, http.MethodPost, "/post/create", form, cookies)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create post status = %d, want 303", rec.Code)
	}

	rec = doRequest(env, http.MethodGet, "/", nil, cookies)
	body := rec.Body.String()
	if !strings.Contains(body, "Hello forum") || !strings.Contains(body, "My first post") {
		t.Fatalf("home should show created post, got: %s", body)
	}

	rec = doRequest(env, http.MethodGet, "/post/view?id=1", nil, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("post view status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "My first post") {
		t.Fatalf("post view should show post content")
	}
}

func TestPostReactionToggle(t *testing.T) {
	env := newTestEnv(t)
	alice := registerUser(t, env, "alice@test.com", "alice", "secret123")
	bob := registerUser(t, env, "bob@test.com", "bob", "secret123")

	doRequest(env, http.MethodPost, "/post/create",
		url.Values{"title": {"Post"}, "content": {"content"}}, alice)

	rec := doRequest(env, http.MethodPost, "/post/reaction",
		url.Values{"post_id": {"1"}, "vote": {"1"}}, bob)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reaction status = %d, want 303", rec.Code)
	}

	body := doRequest(env, http.MethodGet, "/", nil, nil).Body.String()
	if !strings.Contains(body, likesMarkup(1)) {
		t.Fatalf("expected 1 like after reaction, got: %s", body)
	}

	doRequest(env, http.MethodPost, "/post/reaction",
		url.Values{"post_id": {"1"}, "vote": {"1"}}, bob)

	body = doRequest(env, http.MethodGet, "/", nil, nil).Body.String()
	if !strings.Contains(body, likesMarkup(0)) {
		t.Fatalf("expected like toggled off, got: %s", body)
	}
}

// likesMarkup — фрагмент HTML со счётчиком лайков, как его рендерят шаблоны
// (число обёрнуто в span, который обновляет realtime.js).
func likesMarkup(n int) string {
	return `<span data-count="likes">` + strconv.Itoa(n) + `</span>`
}

func TestCommentsAndReactions(t *testing.T) {
	env := newTestEnv(t)
	alice := registerUser(t, env, "alice@test.com", "alice", "secret123")

	doRequest(env, http.MethodPost, "/post/create",
		url.Values{"title": {"Post"}, "content": {"content"}}, alice)

	rec := doRequest(env, http.MethodPost, "/comment/create",
		url.Values{"post_id": {"1"}, "content": {"Nice post!"}}, alice)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("comment create status = %d, want 303", rec.Code)
	}

	body := doRequest(env, http.MethodGet, "/post/view?id=1", nil, nil).Body.String()
	if !strings.Contains(body, "Nice post!") {
		t.Fatalf("post view should contain comment, got: %s", body)
	}

	doRequest(env, http.MethodPost, "/comment/reaction",
		url.Values{"comment_id": {"1"}, "vote": {"1"}}, alice)

	body = doRequest(env, http.MethodGet, "/post/view?id=1", nil, nil).Body.String()
	if !strings.Contains(body, likesMarkup(1)) {
		t.Fatalf("expected 1 like on comment, got: %s", body)
	}
	if !strings.Contains(body, `data-comment-id="1"`) {
		t.Fatalf("comment reaction form should carry data-comment-id, got: %s", body)
	}
}

func TestHomeFilters(t *testing.T) {
	env := newTestEnv(t)
	alice := registerUser(t, env, "alice@test.com", "alice", "secret123")
	bob := registerUser(t, env, "bob@test.com", "bob", "secret123")

	doRequest(env, http.MethodPost, "/post/create",
		url.Values{"title": {"Alice post"}, "content": {"c"}, "category": {"1"}}, alice)
	doRequest(env, http.MethodPost, "/post/create",
		url.Values{"title": {"Bob post"}, "content": {"c"}, "category": {"2"}}, bob)

	// Фильтр по автору
	body := doRequest(env, http.MethodGet, "/?filter=created", nil, alice).Body.String()
	if !strings.Contains(body, "Alice post") || strings.Contains(body, "Bob post") {
		t.Fatalf("filter=created for alice should show only Alice post")
	}

	// Фильтр по категории
	body = doRequest(env, http.MethodGet, "/?category=1", nil, nil).Body.String()
	if !strings.Contains(body, "Alice post") || strings.Contains(body, "Bob post") {
		t.Fatalf("filter by category 1 should show only Alice post")
	}

	// Фильтр по лайкам
	doRequest(env, http.MethodPost, "/post/reaction",
		url.Values{"post_id": {"1"}, "vote": {"1"}}, bob)
	body = doRequest(env, http.MethodGet, "/?filter=liked", nil, bob).Body.String()
	if !strings.Contains(body, "Alice post") {
		t.Fatalf("filter=liked for bob should show liked post")
	}

	// Неавторизованный пользователь просто видит все посты
	body = doRequest(env, http.MethodGet, "/?filter=created", nil, nil).Body.String()
	if !strings.Contains(body, "Alice post") || !strings.Contains(body, "Bob post") {
		t.Fatalf("anonymous user should see all posts despite filter param")
	}
}

func TestErrors(t *testing.T) {
	env := newTestEnv(t)

	rec := doRequest(env, http.MethodGet, "/nonexistent-page", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown page status = %d, want 404", rec.Code)
	}

	rec = doRequest(env, http.MethodGet, "/post/create", nil, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /post/create status = %d, want 405", rec.Code)
	}

	rec = doRequest(env, http.MethodGet, "/post/view?id=missing", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bad post id status = %d, want 404", rec.Code)
	}
}

func TestLogoutButtonOnAllPages(t *testing.T) {
	env := newTestEnv(t)
	cookies := registerUser(t, env, "alice@test.com", "alice", "secret123")

	// Создаём пост, чтобы была страница просмотра.
	doRequest(env, http.MethodPost, "/post/create",
		url.Values{"title": {"Post"}, "content": {"content"}}, cookies)

	pages := []string{
		"/",
		"/post/view?id=1",
		"/messages",
		"/login",
		"/register",
		"/nonexistent-page", // страница ошибки 404
	}

	for _, page := range pages {
		rec := doRequest(env, http.MethodGet, page, nil, cookies)
		if rec.Code == http.StatusInternalServerError {
			t.Fatalf("GET %s status = 500", page)
		}
		if !strings.Contains(rec.Body.String(), ">Выйти<") || !strings.Contains(rec.Body.String(), "/logout") {
			t.Fatalf("GET %s (status %d): expected logout button, got: %s", page, rec.Code, rec.Body.String())
		}
	}

	// Неавторизованный пользователь не должен видеть кнопку выхода.
	rec := doRequest(env, http.MethodGet, "/nonexistent-page", nil, nil)
	if strings.Contains(rec.Body.String(), ">Выйти<") {
		t.Fatalf("anonymous user should not see logout button on error page")
	}
}

func TestChatPanelAlwaysVisible(t *testing.T) {
	env := newTestEnv(t)
	cookies := registerUser(t, env, "alice@test.com", "alice", "secret123")
	registerUser(t, env, "bob@test.com", "bob", "secret123")

	// Панель «Кто онлайн» присутствует на всех страницах для авторизованных.
	for _, page := range []string{"/", "/login", "/register", "/messages", "/nonexistent-page"} {
		body := doRequest(env, http.MethodGet, page, nil, cookies).Body.String()
		if !strings.Contains(body, "Кто онлайн") {
			t.Fatalf("GET %s: chat panel should be visible, got: %s", page, body)
		}
		if !strings.Contains(body, "/messages?with=") {
			t.Fatalf("GET %s: chat panel should link to contacts, got: %s", page, body)
		}
	}

	// Аноним панель не видит.
	anon := doRequest(env, http.MethodGet, "/", nil, nil).Body.String()
	if strings.Contains(anon, "Кто онлайн") {
		t.Fatalf("anonymous user should not see chat panel")
	}

	// Другой пользователь в сети — показывается зелёная точка.
	body := doRequest(env, http.MethodGet, "/", nil, cookies).Body.String()
	if !strings.Contains(body, "online-dot on") || !strings.Contains(body, "@bob") {
		t.Fatalf("expected online bob in chat panel, got: %s", body)
	}
}

func TestMessagePagination(t *testing.T) {
	env := newTestEnv(t)
	registerUser(t, env, "alice@test.com", "alice", "secret123")
	bobCookies := registerUser(t, env, "bob@test.com", "bob", "secret123")

	aliceID, _, err := env.Repo.UserCredentialsByLogin("alice@test.com")
	if err != nil {
		t.Fatalf("alice id: %v", err)
	}
	bobID, _, err := env.Repo.UserCredentialsByLogin("bob@test.com")
	if err != nil {
		t.Fatalf("bob id: %v", err)
	}

	// 15 сообщений от alice к bob (id = 1..15).
	for i := 1; i <= 15; i++ {
		if _, err := env.Repo.CreateMessage(aliceID, bobID, "hi-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("CreateMessage: %v", err)
		}
	}

	// Начальная страница — последние 10 сообщений (hi-6..hi-15).
	body := doRequest(env, http.MethodGet, "/messages?with="+strconv.Itoa(aliceID), nil, bobCookies).Body.String()
	if !strings.Contains(body, "hi-6") || !strings.Contains(body, "hi-15") {
		t.Fatalf("page should contain last 10 messages (hi-6..hi-15), got: %s", body)
	}
	if strings.Contains(body, "hi-5") {
		t.Fatalf("page should NOT contain hi-5 (only last 10), got: %s", body)
	}
	if !strings.Contains(body, `data-has-more="true"`) {
		t.Fatalf("expected data-has-more=true, got: %s", body)
	}
	// Самое старое показанное сообщение — hi-6 (id 6).
	if !strings.Contains(body, `data-oldest="6"`) {
		t.Fatalf("expected data-oldest=\"6\", got: %s", body)
	}

	// Подгрузка старых сообщений (прокрутка вверх) — JSON hi-1..hi-5.
	rec := doRequest(env, http.MethodGet,
		"/messages?with="+strconv.Itoa(aliceID)+"&before=6", nil, bobCookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("older page status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("older page Content-Type = %q, want application/json", ct)
	}
	var older []models.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &older); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(older) != 5 {
		t.Fatalf("expected 5 older messages, got %d", len(older))
	}
	if older[0].Content != "hi-1" || older[4].Content != "hi-5" {
		t.Fatalf("older messages must be ascending hi-1..hi-5, got %+v", older)
	}
}

func TestWebSocketRealTimeMessage(t *testing.T) {
	env := newTestEnv(t)
	aliceCookies := registerUser(t, env, "alice@test.com", "alice", "secret123")
	bobCookies := registerUser(t, env, "bob@test.com", "bob", "secret123")

	aliceID, _, err := env.Repo.UserCredentialsByLogin("alice@test.com")
	if err != nil {
		t.Fatalf("alice id: %v", err)
	}

	srv := httptest.NewServer(env.Server)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	// Alice подключается по WebSocket со своей кукой.
	header := http.Header{}
	parts := make([]string, 0, len(aliceCookies))
	for _, c := range aliceCookies {
		parts = append(parts, c.String())
	}
	header.Set("Cookie", strings.Join(parts, "; "))
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	// bob отправляет сообщение через обычный POST.
	rec := doRequest(env, http.MethodPost, "/messages/send",
		url.Values{"receiver_id": {strconv.Itoa(aliceID)}, "content": {"привет в реальном времени"}}, bobCookies)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("send status = %d, want 303", rec.Code)
	}

	// Alice получает событие без перезагрузки страницы.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var ev chat.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal ws event: %v\npayload: %s", err, data)
	}
	if ev.Type != chat.EventMessage || ev.Message == nil || ev.Message.Sender != "bob" || ev.Message.Content != "привет в реальном времени" {
		t.Fatalf("unexpected ws event: %+v", ev)
	}
	if ev.Message.SenderID != 0 && ev.Message.ID == 0 {
		t.Fatalf("message should carry id, got %+v", ev.Message)
	}
}

// dialWS открывает WebSocket-соединение к тестовому серверу от имени пользователя с данными куками.
func dialWS(t *testing.T, srv *httptest.Server, cookies []*http.Cookie) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	header := http.Header{}
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.String())
	}
	header.Set("Cookie", strings.Join(parts, "; "))
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readEvent читает одно событие из WebSocket-соединения.
func readEvent(t *testing.T, conn *websocket.Conn) chat.Event {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var ev chat.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal ws event: %v\npayload: %s", err, data)
	}
	return ev
}

func TestWebSocketRealTimeReactions(t *testing.T) {
	env := newTestEnv(t)
	alice := registerUser(t, env, "alice@test.com", "alice", "secret123")
	bob := registerUser(t, env, "bob@test.com", "bob", "secret123")

	doRequest(env, http.MethodPost, "/post/create",
		url.Values{"title": {"Post"}, "content": {"content"}}, alice)
	doRequest(env, http.MethodPost, "/comment/create",
		url.Values{"post_id": {"1"}, "content": {"Nice post!"}}, alice)

	srv := httptest.NewServer(env.Server)
	defer srv.Close()

	// Событие публичное: его получает и другой пользователь, и сам голосовавший.
	aliceConn := dialWS(t, srv, alice)
	bobConn := dialWS(t, srv, bob)

	// bob лайкает пост.
	rec := doRequest(env, http.MethodPost, "/post/reaction",
		url.Values{"post_id": {"1"}, "vote": {"1"}}, bob)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("post reaction status = %d, want 303", rec.Code)
	}
	wantPost := models.ReactionCounts{Target: models.ReactionTargetPost, ID: 1, Likes: 1, Dislikes: 0}
	for _, conn := range []*websocket.Conn{aliceConn, bobConn} {
		ev := readEvent(t, conn)
		if ev.Type != chat.EventReaction || ev.Reaction == nil || *ev.Reaction != wantPost {
			t.Fatalf("unexpected post reaction event: %+v", ev)
		}
		if ev.Message != nil {
			t.Fatalf("reaction event should not carry message: %+v", ev.Message)
		}
	}

	// bob дизлайкает комментарий.
	rec = doRequest(env, http.MethodPost, "/comment/reaction",
		url.Values{"comment_id": {"1"}, "vote": {"-1"}}, bob)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("comment reaction status = %d, want 303", rec.Code)
	}
	wantComment := models.ReactionCounts{Target: models.ReactionTargetComment, ID: 1, Likes: 0, Dislikes: 1}
	ev := readEvent(t, aliceConn)
	if ev.Type != chat.EventReaction || ev.Reaction == nil || *ev.Reaction != wantComment {
		t.Fatalf("unexpected comment reaction event: %+v", ev)
	}
}

func TestPrivateMessages(t *testing.T) {
	env := newTestEnv(t)
	alice := registerUser(t, env, "alice@test.com", "alice", "secret123")
	bob := registerUser(t, env, "bob@test.com", "bob", "secret123")

	// Неавторизованный пользователь не может смотреть сообщения
	rec := doRequest(env, http.MethodGet, "/messages", nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /messages anonymous status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("anonymous GET /messages Location = %q, want /login", loc)
	}

	// Нельзя отправить сообщение самому себе
	rec = doRequest(env, http.MethodPost, "/messages/send",
		url.Values{"receiver_id": {"1"}, "content": {"self"}}, alice)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("send to self status = %d, want 400", rec.Code)
	}

	// Отправка сообщения alice -> bob
	rec = doRequest(env, http.MethodPost, "/messages/send",
		url.Values{"receiver_id": {"2"}, "content": {"hello bob"}}, alice)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("send message status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/messages?with=2" {
		t.Fatalf("send message Location = %q, want /messages?with=2", loc)
	}

	// Bob видит переписку с alice
	body := doRequest(env, http.MethodGet, "/messages?with=1", nil, bob).Body.String()
	if !strings.Contains(body, "hello bob") {
		t.Fatalf("thread should show message 'hello bob', got: %s", body)
	}
	// Каждое сообщение содержит шапку: имя отправителя и дату отправки.
	if !strings.Contains(body, "msg-meta") || !strings.Contains(body, "@alice") {
		t.Fatalf("messages should show sender name in header (msg-meta), got: %s", body)
	}

	// Alice в списке контактов видит bob-а
	body = doRequest(env, http.MethodGet, "/messages", nil, alice).Body.String()
	if !strings.Contains(body, "@bob") {
		t.Fatalf("inbox should show contact @bob, got: %s", body)
	}

	// Пустая переписка у нового пользователя — показывается форма сообщения
	tt := registerUser(t, env, "tt@test.com", "tt", "secret123")
	body = doRequest(env, http.MethodGet, "/messages", nil, tt).Body.String()
	if !strings.Contains(body, "Новое сообщение") {
		t.Fatalf("new user should see new-message form, got: %s", body)
	}
}
