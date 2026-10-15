package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildMessageQuery(t *testing.T) {
	q, args := BuildMessageQuery(MessageFilter{})
	if !strings.Contains(q, "m.chat_jid != 'status@broadcast'") {
		t.Error("status should be excluded by default")
	}
	if !reflect.DeepEqual(args, []any{20, 0}) {
		t.Errorf("default args = %v", args)
	}
	q, _ = BuildMessageQuery(MessageFilter{IncludeStatus: true})
	if strings.Contains(q, "status@broadcast") {
		t.Error("include_status should drop the status filter")
	}
	after := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	q, args = BuildMessageQuery(MessageFilter{ChatJIDs: []string{"a@s.whatsapp.net", "b@lid"}, Senders: []string{"x"},
		Query: "hello", After: after, Limit: 5, Offset: 10, OnlyMedia: true})
	for _, frag := range []string{"m.chat_jid IN (?, ?)", "m.sender IN (?)", "LOWER(m.content) LIKE LOWER(?)", "m.timestamp > ?", "media_type"} {
		if !strings.Contains(q, frag) {
			t.Errorf("query missing %q: %s", frag, q)
		}
	}
	want := []any{"a@s.whatsapp.net", "b@lid", "x", "%hello%", after, 5, 10}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMessageFiltering(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	dm, grp, status := "15550100@s.whatsapp.net", "123456789-123@g.us", "status@broadcast"
	for _, c := range []struct{ jid, name string }{{dm, "Alice"}, {grp, "Book club"}, {status, "Status"}} {
		if err := s.UpsertChat(c.jid, c.name, base); err != nil {
			t.Fatal(err)
		}
	}
	msgs := []Message{
		{ID: "1", ChatJID: dm, Sender: dm, Content: "Hello there", Timestamp: base},
		{ID: "2", ChatJID: dm, Sender: "15550199@s.whatsapp.net", Content: "reply", Timestamp: base.Add(time.Minute), IsFromMe: true},
		{ID: "3", ChatJID: grp, Sender: "123456789012345@lid", Content: "group hello", Timestamp: base.Add(2 * time.Minute)},
		{ID: "4", ChatJID: status, Sender: dm, Content: "my story hello", Timestamp: base.Add(3 * time.Minute)},
		{ID: "5", ChatJID: grp, Sender: "15550100", MediaType: "image", Filename: "image_5.jpg", Timestamp: base.Add(4 * time.Minute)},
		{ID: "6", ChatJID: grp, Sender: "x"}, // no content, no media: skipped
	}
	for _, m := range msgs {
		if err := s.StoreMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(f MessageFilter) []string {
		got, err := s.ListMessages(f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range got {
			out = append(out, m.ID)
		}
		return out
	}
	check := func(name string, f MessageFilter, want ...string) {
		t.Helper()
		if got := ids(f); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	check("default excludes status", MessageFilter{}, "5", "3", "2", "1")
	check("include status", MessageFilter{IncludeStatus: true}, "5", "4", "3", "2", "1")
	check("query", MessageFilter{Query: "HELLO"}, "3", "1")
	check("chat", MessageFilter{ChatJIDs: []string{dm}}, "2", "1")
	check("sender (LID)", MessageFilter{Senders: []string{"123456789012345@lid"}}, "3")
	check("sender (bare user part)", MessageFilter{Senders: []string{"15550100", dm}}, "5", "1")
	check("after", MessageFilter{After: base.Add(90 * time.Second)}, "5", "3")
	check("before", MessageFilter{Before: base.Add(90 * time.Second)}, "2", "1")
	check("only media", MessageFilter{OnlyMedia: true}, "5")
	check("paging", MessageFilter{Limit: 2, Offset: 2}, "2", "1")

	m, err := s.GetMessage("", "3")
	if err != nil || m.ChatName != "Book club" {
		t.Fatalf("GetMessage: %+v %v", m, err)
	}
	prev, next, err := s.Context(m, 5, 5)
	if err != nil || len(prev) != 0 || len(next) != 1 || next[0].ID != "5" {
		t.Fatalf("Context: %v %v %v", prev, next, err)
	}

	chats, err := s.ListChats(ChatFilter{})
	if err != nil || len(chats) != 2 {
		t.Fatalf("ListChats default should hide status: %+v %v", chats, err)
	}
	// Empty name never overwrites an existing name.
	if err := s.UpsertChat(dm, "", base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.GetChat(dm); c.Name != "Alice" || !c.LastMessageTime.Equal(base.Add(time.Hour)) {
		t.Fatalf("UpsertChat: %+v", c)
	}
}

func TestPollsAndVotes(t *testing.T) {
	s := openTemp(t)
	p := Poll{ID: "P1", ChatJID: "123456789-123@g.us", Question: "When?", Options: []string{"Thu", "Fri"}}
	if err := s.SavePoll(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPoll("P1")
	if err != nil || !reflect.DeepEqual(got.Options, p.Options) {
		t.Fatalf("GetPoll: %+v %v", got, err)
	}
	_ = s.SaveVote("P1", p.ChatJID, "15550100@s.whatsapp.net", []string{"Thu"}, time.Now())
	_ = s.SaveVote("P1", p.ChatJID, "15550101@s.whatsapp.net", []string{"Thu", "Fri"}, time.Now())
	_ = s.SaveVote("P1", p.ChatJID, "15550100@s.whatsapp.net", []string{"Fri"}, time.Now()) // changed vote
	votes, err := s.Votes("P1")
	if err != nil {
		t.Fatal(err)
	}
	tally := TallyVotes(p.Options, votes)
	if len(tally["Thu"]) != 1 || len(tally["Fri"]) != 2 {
		t.Fatalf("tally = %v", tally)
	}
	if _, err := s.GetPoll("nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
