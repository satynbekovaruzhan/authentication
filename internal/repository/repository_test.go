package repository

import (
	"errors"
	"testing"
	"time"

	"forum-authentication/internal/models"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	repo, err := Open(":memory:")
	if err != nil {
		t.Fatalf("repository.Open: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func createUser(t *testing.T, repo *Repository, email, username string) int {
	t.Helper()
	id, err := repo.CreateUser(NewUser{
		Email:        email,
		Username:     username,
		PasswordHash: "hashed-password",
	})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return id
}

func TestCreateUserAndDuplicates(t *testing.T) {
	repo := newTestRepo(t)

	id, err := repo.CreateUser(NewUser{Email: "alice@test.com", Username: "alice", PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	if ok, err := repo.EmailExists("alice@test.com"); err != nil || !ok {
		t.Fatalf("EmailExists = %v, %v; want true, nil", ok, err)
	}
	if ok, err := repo.UsernameExists("alice"); err != nil || !ok {
		t.Fatalf("UsernameExists = %v, %v; want true, nil", ok, err)
	}

	if _, err := repo.CreateUser(NewUser{Email: "alice@test.com", Username: "bob", PasswordHash: "hash"}); err == nil {
		t.Fatal("CreateUser with duplicate email: expected error")
	}
	if _, err := repo.CreateUser(NewUser{Email: "bob@test.com", Username: "alice", PasswordHash: "hash"}); err == nil {
		t.Fatal("CreateUser with duplicate username: expected error")
	}
}

func TestUserCredentialsByLogin(t *testing.T) {
	repo := newTestRepo(t)
	createUser(t, repo, "alice@test.com", "alice")

	id, hash, err := repo.UserCredentialsByLogin("alice@test.com")
	if err != nil || id <= 0 || hash == "" {
		t.Fatalf("UserCredentialsByLogin(email) = %d, %q, %v", id, hash, err)
	}

	id, hash, err = repo.UserCredentialsByLogin("alice")
	if err != nil || id <= 0 || hash == "" {
		t.Fatalf("UserCredentialsByLogin(username) = %d, %q, %v", id, hash, err)
	}

	if _, _, err := repo.UserCredentialsByLogin("missing@test.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	repo := newTestRepo(t)
	userID := createUser(t, repo, "alice@test.com", "alice")

	now := time.Now()
	token := "token-1"

	if err := repo.CreateSession(userID, token, now.Add(24*time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	user, err := repo.UserBySession(token, now)
	if err != nil {
		t.Fatalf("UserBySession: %v", err)
	}
	if user.Username != "alice" {
		t.Fatalf("UserBySession username = %q, want alice", user.Username)
	}

	if _, err := repo.UserBySession(token, now.Add(48*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session: expected ErrNotFound, got %v", err)
	}

	if err := repo.DeleteSession(token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := repo.UserBySession(token, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted session: expected ErrNotFound, got %v", err)
	}
}

func TestCreateSessionReplacesOld(t *testing.T) {
	repo := newTestRepo(t)
	userID := createUser(t, repo, "alice@test.com", "alice")

	if err := repo.CreateSession(userID, "token-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := repo.DeleteSessionsByUser(userID); err != nil {
		t.Fatalf("DeleteSessionsByUser: %v", err)
	}

	if _, err := repo.UserBySession("token-1", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after DeleteSessionsByUser, got %v", err)
	}
}

func TestCreatePostWithCategories(t *testing.T) {
	repo := newTestRepo(t)
	userID := createUser(t, repo, "alice@test.com", "alice")

	categories, err := repo.Categories()
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(categories) < 2 {
		t.Fatalf("expected at least 2 seeded categories, got %d", len(categories))
	}

	postID, err := repo.CreatePost(userID, "Hello", "World", []int{categories[0].ID, categories[1].ID})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	posts, err := repo.Posts(models.PostFilter{})
	if err != nil {
		t.Fatalf("Posts: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].Title != "Hello" || posts[0].Username != "alice" {
		t.Fatalf("unexpected post: %+v", posts[0])
	}
	if len(posts[0].Categories) != 2 {
		t.Fatalf("expected 2 categories, got %v", posts[0].Categories)
	}

	got, err := repo.PostByID(postID)
	if err != nil {
		t.Fatalf("PostByID: %v", err)
	}
	if got.Content != "World" {
		t.Fatalf("PostByID content = %q, want World", got.Content)
	}

	if _, err := repo.PostByID(9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing post, got %v", err)
	}
}

func TestPostsFilterByCategory(t *testing.T) {
	repo := newTestRepo(t)
	userID := createUser(t, repo, "alice@test.com", "alice")

	cats, _ := repo.Categories()

	_, err := repo.CreatePost(userID, "GoPost", "content", []int{cats[0].ID})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	_, err = repo.CreatePost(userID, "OtherPost", "content", []int{cats[1].ID})
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	posts, err := repo.Posts(models.PostFilter{CategoryID: cats[0].ID})
	if err != nil {
		t.Fatalf("Posts(category): %v", err)
	}
	if len(posts) != 1 || posts[0].Title != "GoPost" {
		t.Fatalf("expected only GoPost, got %+v", posts)
	}
}

func TestPostsFilterByAuthorAndLiked(t *testing.T) {
	repo := newTestRepo(t)
	aliceID := createUser(t, repo, "alice@test.com", "alice")
	bobID := createUser(t, repo, "bob@test.com", "bob")

	if _, err := repo.CreatePost(aliceID, "AlicePost", "content", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	if _, err := repo.CreatePost(bobID, "BobPost", "content", nil); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	authorPosts, err := repo.Posts(models.PostFilter{AuthorID: bobID})
	if err != nil {
		t.Fatalf("Posts(author): %v", err)
	}
	if len(authorPosts) != 1 || authorPosts[0].Title != "BobPost" {
		t.Fatalf("expected only BobPost, got %+v", authorPosts)
	}

	if _, err := repo.SetPostReaction(aliceID, authorPosts[0].ID, 1); err != nil {
		t.Fatalf("SetPostReaction: %v", err)
	}

	likedPosts, err := repo.Posts(models.PostFilter{LikedBy: aliceID})
	if err != nil {
		t.Fatalf("Posts(liked): %v", err)
	}
	if len(likedPosts) != 1 || likedPosts[0].Title != "BobPost" {
		t.Fatalf("expected only liked BobPost, got %+v", likedPosts)
	}
	if likedPosts[0].Likes != 1 {
		t.Fatalf("expected 1 like, got %d", likedPosts[0].Likes)
	}
}

func TestPostReactionToggle(t *testing.T) {
	repo := newTestRepo(t)
	aliceID := createUser(t, repo, "alice@test.com", "alice")
	postID, err := repo.CreatePost(aliceID, "Post", "content", nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	counts, err := repo.SetPostReaction(aliceID, postID, 1)
	if err != nil {
		t.Fatalf("SetPostReaction(like): %v", err)
	}
	want := models.ReactionCounts{Target: models.ReactionTargetPost, ID: postID, Likes: 1, Dislikes: 0}
	if counts != want {
		t.Fatalf("after like: returned counts %+v, want %+v", counts, want)
	}
	post, _ := repo.PostByID(postID)
	if post.Likes != 1 || post.Dislikes != 0 {
		t.Fatalf("after like: got likes=%d dislikes=%d, want 1/0", post.Likes, post.Dislikes)
	}

	counts, err = repo.SetPostReaction(aliceID, postID, 1)
	if err != nil {
		t.Fatalf("SetPostReaction(like again): %v", err)
	}
	if counts.Likes != 0 || counts.Dislikes != 0 {
		t.Fatalf("toggle off: returned counts %+v, want 0/0", counts)
	}
	post, _ = repo.PostByID(postID)
	if post.Likes != 0 {
		t.Fatalf("second same reaction should toggle off, got likes=%d", post.Likes)
	}

	counts, err = repo.SetPostReaction(aliceID, postID, -1)
	if err != nil {
		t.Fatalf("SetPostReaction(dislike): %v", err)
	}
	if counts.Likes != 0 || counts.Dislikes != 1 {
		t.Fatalf("after dislike: returned counts %+v, want 0/1", counts)
	}
	post, _ = repo.PostByID(postID)
	if post.Likes != 0 || post.Dislikes != 1 {
		t.Fatalf("after dislike: got likes=%d dislikes=%d, want 0/1", post.Likes, post.Dislikes)
	}
}

func TestCommentsAndReaction(t *testing.T) {
	repo := newTestRepo(t)
	aliceID := createUser(t, repo, "alice@test.com", "alice")
	bobID := createUser(t, repo, "bob@test.com", "bob")

	postID, err := repo.CreatePost(aliceID, "Post", "content", nil)
	if err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	if err := repo.CreateComment(postID, bobID, "Nice!"); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	comments, err := repo.CommentsByPost(postID)
	if err != nil {
		t.Fatalf("CommentsByPost: %v", err)
	}
	if len(comments) != 1 || comments[0].Username != "bob" || comments[0].Content != "Nice!" {
		t.Fatalf("unexpected comments: %+v", comments)
	}

	counts, err := repo.SetCommentReaction(aliceID, comments[0].ID, 1)
	if err != nil {
		t.Fatalf("SetCommentReaction: %v", err)
	}
	want := models.ReactionCounts{Target: models.ReactionTargetComment, ID: comments[0].ID, Likes: 1}
	if counts != want {
		t.Fatalf("SetCommentReaction: returned counts %+v, want %+v", counts, want)
	}
	comments, _ = repo.CommentsByPost(postID)
	if comments[0].Likes != 1 {
		t.Fatalf("expected 1 like on comment, got %d", comments[0].Likes)
	}

	if _, err := repo.SetCommentReaction(aliceID, comments[0].ID, 1); err != nil {
		t.Fatalf("SetCommentReaction(again): %v", err)
	}
	comments, _ = repo.CommentsByPost(postID)
	if comments[0].Likes != 0 {
		t.Fatalf("expected toggle off, got likes=%d", comments[0].Likes)
	}
}

func TestCreateUserProfile(t *testing.T) {
	repo := newTestRepo(t)

	id, err := repo.CreateUser(NewUser{
		Email:        "alice@test.com",
		Username:     "alice",
		PasswordHash: "hash",
		FirstName:    "Алиса",
		LastName:     "Иванова",
		Age:          25,
		Gender:       "female",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u, err := repo.UserByID(id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if u.FirstName != "Алиса" || u.LastName != "Иванова" {
		t.Fatalf("unexpected name: %+v", u)
	}
	if u.Age != 25 || u.Gender != "female" {
		t.Fatalf("unexpected age/gender: %+v", u)
	}

	// Старые пользователи без профиля после миграции имеют пустые значения.
	oldID := createUser(t, repo, "bob@test.com", "bob")
	oldUser, err := repo.UserByID(oldID)
	if err != nil {
		t.Fatalf("UserByID(bob): %v", err)
	}
	if oldUser.FirstName != "" || oldUser.Age != 0 {
		t.Fatalf("expected empty profile after migration, got %+v", oldUser)
	}
}

func TestMessages(t *testing.T) {
	repo := newTestRepo(t)
	aliceID := createUser(t, repo, "alice@test.com", "alice")
	bobID := createUser(t, repo, "bob@test.com", "bob")

	if _, err := repo.CreateMessage(aliceID, bobID, "hi bob"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if _, err := repo.CreateMessage(bobID, aliceID, "hi alice"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if _, err := repo.CreateMessage(aliceID, bobID, "bye"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	messages, err := repo.MessagesBetween(aliceID, bobID)
	if err != nil {
		t.Fatalf("MessagesBetween: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if messages[0].Content != "hi bob" {
		t.Fatalf("first message = %q, want 'hi bob'", messages[0].Content)
	}
	if messages[len(messages)-1].Content != "bye" {
		t.Fatalf("last message = %q, want 'bye'", messages[len(messages)-1].Content)
	}

	convs, err := repo.Conversations(aliceID)
	if err != nil {
		t.Fatalf("Conversations(alice): %v", err)
	}
	if len(convs) != 1 || convs[0].OtherUser.Username != "bob" {
		t.Fatalf("unexpected conversations for alice: %+v", convs)
	}
	if convs[0].LastMsg != "bye" {
		t.Fatalf("last message for alice = %q, want 'bye'", convs[0].LastMsg)
	}

	convsBob, err := repo.Conversations(bobID)
	if err != nil {
		t.Fatalf("Conversations(bob): %v", err)
	}
	if len(convsBob) != 1 || convsBob[0].OtherUser.Username != "alice" {
		t.Fatalf("unexpected conversations for bob: %+v", convsBob)
	}

	users, err := repo.UsersExcept(aliceID)
	if err != nil {
		t.Fatalf("UsersExcept: %v", err)
	}
	if len(users) != 1 || users[0].Username != "bob" {
		t.Fatalf("unexpected UsersExcept(alice): %+v", users)
	}
}

func TestTouchLastSeen(t *testing.T) {
	repo := newTestRepo(t)
	userID := createUser(t, repo, "alice@test.com", "alice")

	now := time.Now()
	if err := repo.CreateSession(userID, "token-1", now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := repo.TouchLastSeen("token-1", now.Add(time.Minute)); err != nil {
		t.Fatalf("TouchLastSeen: %v", err)
	}

	// Истёкшая сессия не должна обновлять last_seen.
	another := createUser(t, repo, "bob@test.com", "bob")
	if err := repo.CreateSession(another, "token-expired", now.Add(-time.Minute)); err != nil {
		t.Fatalf("CreateSession(expired): %v", err)
	}
	if err := repo.TouchLastSeen("token-expired", now); err != nil {
		t.Fatalf("TouchLastSeen(expired): %v", err)
	}

	contacts, err := repo.Contacts(another, now)
	if err != nil {
		t.Fatalf("Contacts(bob): %v", err)
	}
	if len(contacts) != 1 || contacts[0].User.Username != "alice" {
		t.Fatalf("expected 1 contact (alice), got %+v", contacts)
	}
	// alice недавно была онлайн, поэтому считается активной.
	if !contacts[0].Online {
		t.Fatalf("alice should be online after TouchLastSeen, got %+v", contacts[0])
	}
}

func TestContactsOrderingAndStatus(t *testing.T) {
	repo := newTestRepo(t)
	aliceID := createUser(t, repo, "alice@test.com", "alice")
	bobID := createUser(t, repo, "bob@test.com", "bob")

	now := time.Now()

	// Без сообщений: в алфавитном порядке.
	contacts, err := repo.Contacts(aliceID, now)
	if err != nil {
		t.Fatalf("Contacts: %v", err)
	}
	if len(contacts) != 1 || contacts[0].User.Username != "bob" {
		t.Fatalf("expected alphabetical contacts, got %+v", contacts)
	}
	if contacts[0].HasMsg {
		t.Fatalf("expected HasMsg=false for fresh user, got %+v", contacts[0])
	}

	// Новый пользователь без переписки.
	carolID := createUser(t, repo, "carol@test.com", "carol")
	daveID := createUser(t, repo, "dave@test.com", "dave")

	// Переписка с dave позже, чем с carol.
	if _, err := repo.CreateMessage(carolID, aliceID, "привет carol"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := repo.CreateMessage(aliceID, daveID, "привет dave"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	contacts, err = repo.Contacts(aliceID, now)
	if err != nil {
		t.Fatalf("Contacts: %v", err)
	}
	// Порядок: переписки по убыванию времени, затем bob без переписки.
	// dave писал после carol, значит dave первый.
	if len(contacts) != 3 {
		t.Fatalf("expected 3 contacts, got %d: %+v", len(contacts), contacts)
	}
	got := []string{contacts[0].User.Username, contacts[1].User.Username, contacts[2].User.Username}
	want := []string{"dave", "carol", "bob"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("contacts order = %v, want %v", got, want)
		}
	}

	// Статус online для контактов, недавно обновивших last_seen.
	if err := repo.CreateSession(bobID, "bob-token", now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(bob): %v", err)
	}
	if err := repo.TouchLastSeen("bob-token", now); err != nil {
		t.Fatalf("TouchLastSeen(bob): %v", err)
	}
	contacts, err = repo.Contacts(aliceID, now)
	if err != nil {
		t.Fatalf("Contacts: %v", err)
	}
	for _, c := range contacts {
		if c.User.Username == "bob" && !c.Online {
			t.Fatalf("bob should be online after TouchLastSeen, got %+v", c)
		}
	}
	if c, ok := contactByUsername(contacts, "carol"); !ok || !c.Online {
		t.Fatalf("carol should be online (fresh user), got %+v", c)
	}
}

func contactByUsername(contacts []models.Contact, username string) (models.Contact, bool) {
	for _, c := range contacts {
		if c.User.Username == username {
			return c, true
		}
	}
	return models.Contact{}, false
}
