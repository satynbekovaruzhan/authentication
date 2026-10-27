package chat

import (
	"encoding/json"
	"testing"
	"time"

	"forum-authentication/internal/models"
)

func TestHubBroadcastAndUnregister(t *testing.T) {
	hub := NewHub()
	alice := hub.Register(1)
	bob := hub.Register(2)

	// Отправка пользователю 1.
	hub.Broadcast(1, Event{Type: EventMessage, Message: &models.Message{ID: 7, SenderID: 2, Content: "hi"}})

	ev := receive(t, alice)
	if ev.Type != EventMessage || ev.Message == nil || ev.Message.Content != "hi" || ev.Message.ID != 7 {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.Reaction != nil {
		t.Fatalf("message event should not carry reaction, got %+v", ev.Reaction)
	}

	// Пользователь 2 не должен получить чужое сообщение.
	select {
	case <-bob.send:
		t.Fatal("bob received a message not addressed to him")
	default:
	}

	// После unregister закрывается канал соединения.
	hub.Unregister(alice)
	if _, ok := <-alice.send; ok {
		t.Fatal("expected closed send channel after unregister")
	}

	// Эффективность: повторный unregister не паникует.
	hub.Unregister(alice)
}

func TestHubBroadcastAll(t *testing.T) {
	hub := NewHub()
	alice := hub.Register(1)
	bob := hub.Register(2)
	bobTab2 := hub.Register(2) // вторая вкладка того же пользователя

	counts := models.ReactionCounts{Target: models.ReactionTargetPost, ID: 3, Likes: 2, Dislikes: 1}
	hub.BroadcastAll(Event{Type: EventReaction, Reaction: &counts})

	for _, c := range []*Client{alice, bob, bobTab2} {
		ev := receive(t, c)
		if ev.Type != EventReaction || ev.Reaction == nil || *ev.Reaction != counts {
			t.Fatalf("unexpected event for user %d: %+v", c.UserID, ev)
		}
		if ev.Message != nil {
			t.Fatalf("reaction event should not carry message, got %+v", ev.Message)
		}
	}
}

// receive читает одно событие из очереди клиента или проваливает тест по таймауту.
func receive(t *testing.T, c *Client) Event {
	t.Helper()
	select {
	case data := <-c.send:
		var ev Event
		if err := json.Unmarshal(data, &ev); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		return ev
	case <-time.After(time.Second):
		t.Fatalf("client of user %d did not receive event", c.UserID)
		return Event{}
	}
}
