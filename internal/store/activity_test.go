package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestReceipts(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	dm, grp := "15550100@s.whatsapp.net", "123456789-123@g.us"
	rows := []Receipt{
		{MessageID: "A", ChatJID: dm, Reader: dm, Type: ReceiptDelivered, Timestamp: base},
		{MessageID: "A", ChatJID: dm, Reader: dm, Type: ReceiptRead, Timestamp: base.Add(time.Minute)},
		{MessageID: "A", ChatJID: dm, Reader: dm, Type: ReceiptDelivered, Timestamp: base.Add(time.Hour)}, // repeat: ignored
		{MessageID: "B", ChatJID: grp, Reader: "1@lid", Type: ReceiptDelivered, Timestamp: base},
		{MessageID: "B", ChatJID: grp, Reader: "2@lid", Type: ReceiptPlayed, Timestamp: base.Add(time.Second)},
		{MessageID: "C", ChatJID: dm, Reader: dm, Type: ReceiptDelivered, Timestamp: base},
	}
	for _, r := range rows {
		if err := s.SaveReceipt(r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Receipts([]string{"A"})
	if err != nil || len(got) != 2 {
		t.Fatalf("Receipts(A) = %+v %v", got, err)
	}
	if !got[0].Timestamp.Equal(base) || got[0].Type != ReceiptDelivered || got[1].Type != ReceiptRead {
		t.Errorf("first delivered time must be kept, oldest first: %+v", got)
	}
	st, err := s.ReceiptStatus([]string{"A", "B", "C", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": ReceiptRead, "B": ReceiptPlayed, "C": ReceiptDelivered}
	if !reflect.DeepEqual(st, want) {
		t.Errorf("ReceiptStatus = %v, want %v", st, want)
	}
	if n, _ := s.CountReceipts(); n != 5 {
		t.Errorf("CountReceipts = %d, want 5", n)
	}
	if r, _ := s.Receipts(nil); r != nil {
		t.Errorf("Receipts(nil) = %v", r)
	}
}

func TestReactions(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	save := func(sender, emoji string, ts time.Time) {
		t.Helper()
		if err := s.SaveReaction(Reaction{MessageID: "M", ChatJID: "c@g.us", Sender: sender, Emoji: emoji, Timestamp: ts}); err != nil {
			t.Fatal(err)
		}
	}
	save("a", "👍", base)
	save("b", "❤️", base.Add(time.Second))
	save("a", "😂", base.Add(time.Minute))  // changed reaction
	save("a", "👎", base.Add(-time.Minute)) // older event arriving late: ignored
	save("b", "", base.Add(time.Hour))     // removed
	// The same person's reaction stored under another form of the chat JID: newest wins.
	_ = s.SaveReaction(Reaction{MessageID: "M", ChatJID: "other@lid", Sender: "a", Emoji: "🔥", Timestamp: base.Add(-time.Hour)})
	got, err := s.Reactions([]string{"M"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Sender != "a" || got[0].Emoji != "😂" {
		t.Fatalf("Reactions = %+v", got)
	}
}

func TestCalls(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	if err := s.SaveCall(Call{ID: "X", Caller: "15550100@s.whatsapp.net", Media: "video", Status: "ringing", Started: base}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCall(Call{ID: "X", Status: "ended", Reason: "timeout", Started: base, Updated: base.Add(30 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	// A late, repeated offer must not reopen an ended call.
	_ = s.SaveCall(Call{ID: "X", Status: "ringing", Started: base, Updated: base.Add(time.Minute)})
	_ = s.SaveCall(Call{ID: "Y", Caller: "15550101@s.whatsapp.net", Status: "ringing", Started: base.Add(time.Hour)})
	c, err := s.GetCall("X")
	if err != nil {
		t.Fatal(err)
	}
	if c.Caller != "15550100@s.whatsapp.net" || c.Media != "video" || c.Status != "ended" || c.Reason != "timeout" ||
		!c.Started.Equal(base) || !c.Updated.Equal(base.Add(30*time.Second)) {
		t.Errorf("merged call = %+v", c)
	}
	list, err := s.ListCalls(10)
	if err != nil || len(list) != 2 || list[0].ID != "Y" {
		t.Fatalf("ListCalls = %+v %v", list, err)
	}
	if _, err := s.GetCall("nope"); err != ErrNotFound {
		t.Errorf("GetCall(nope) = %v", err)
	}
}

func TestQuotedIDAndFromMe(t *testing.T) {
	s := openTemp(t)
	base := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	dm := "15550100@s.whatsapp.net"
	_ = s.UpsertChat(dm, "Alice", base)
	_ = s.StoreMessage(Message{ID: "1", ChatJID: dm, Sender: dm, Content: "question?", Timestamp: base})
	_ = s.StoreMessage(Message{ID: "2", ChatJID: dm, Sender: "me", Content: "answer", Timestamp: base.Add(time.Minute), IsFromMe: true, QuotedID: "1"})
	m, err := s.GetMessage(dm, "2")
	if err != nil || m.QuotedID != "1" {
		t.Fatalf("quoted id not stored: %+v %v", m, err)
	}
	got, err := s.ListMessages(MessageFilter{OnlyFromMe: true})
	if err != nil || len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("OnlyFromMe = %+v %v", got, err)
	}
}

// An existing messages.db from the original bridge (no direct_path or
// quoted_id columns, no activity tables) must open and gain them.
func TestMigrateOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP);
		CREATE TABLE messages (id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP, is_from_me BOOLEAN,
			media_type TEXT, filename TEXT, url TEXT, media_key BLOB, file_sha256 BLOB, file_enc_sha256 BLOB, file_length INTEGER,
			PRIMARY KEY (id, chat_jid));
		INSERT INTO chats VALUES ('a@s.whatsapp.net', 'A', '2026-01-01 00:00:00+00:00');
		INSERT INTO messages (id, chat_jid, sender, content, timestamp, is_from_me) VALUES ('1', 'a@s.whatsapp.net', 'a', 'hi', '2026-01-01 00:00:00+00:00', 1);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // reopening must be a no-op
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		m, err := s.GetMessage("", "1")
		if err != nil || m.Content != "hi" || m.QuotedID != "" {
			t.Fatalf("old row: %+v %v", m, err)
		}
		if err := s.SaveReceipt(Receipt{MessageID: "1", ChatJID: "a@s.whatsapp.net", Reader: "a@s.whatsapp.net", Type: ReceiptRead, Timestamp: time.Now()}); err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}
