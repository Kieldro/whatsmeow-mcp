package wa

import (
	"context"
	"reflect"
	"testing"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

func TestReceiptRows(t *testing.T) {
	me := types.NewJID("15550199", types.DefaultUserServer)
	myLID := types.NewJID("999999999999999", types.HiddenUserServer)
	isOwn := func(j types.JID) bool { return j.User == me.User || j.User == myLID.User }
	alice := types.JID{User: "15550100", Server: types.DefaultUserServer, Device: 3}
	ts := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	rcpt := func(typ types.ReceiptType, sender types.JID, fromMe bool, ids ...string) *events.Receipt {
		return &events.Receipt{MessageSource: types.MessageSource{Chat: alice.ToNonAD(), Sender: sender, IsFromMe: fromMe},
			MessageIDs: ids, Timestamp: ts, Type: typ}
	}

	got := receiptRows(rcpt(types.ReceiptTypeRead, alice, false, "A", "B"), isOwn)
	want := []store.Receipt{
		{MessageID: "A", ChatJID: "15550100@s.whatsapp.net", Reader: "15550100@s.whatsapp.net", Type: "read", Timestamp: ts},
		{MessageID: "B", ChatJID: "15550100@s.whatsapp.net", Reader: "15550100@s.whatsapp.net", Type: "read", Timestamp: ts},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read receipt rows = %+v", got)
	}
	if got := receiptRows(rcpt(types.ReceiptTypeDelivered, alice, false, "A"), isOwn); len(got) != 1 || got[0].Type != "delivered" {
		t.Errorf("delivered receipt (empty type) = %+v", got)
	}
	if got := receiptRows(rcpt(types.ReceiptTypePlayed, alice, false, "A"), isOwn); len(got) != 1 || got[0].Type != "played" {
		t.Errorf("played receipt = %+v", got)
	}
	skipped := []*events.Receipt{
		rcpt(types.ReceiptTypeReadSelf, alice, false, "A"),
		rcpt(types.ReceiptTypePlayedSelf, alice, false, "A"),
		rcpt(types.ReceiptTypeRetry, alice, false, "A"),
		rcpt(types.ReceiptTypeSender, alice, false, "A"),
		rcpt(types.ReceiptTypeServerError, alice, false, "A"),
		rcpt(types.ReceiptTypeInactive, alice, false, "A"),
		rcpt(types.ReceiptTypeRead, alice, true, "A"),           // from one of my devices
		rcpt(types.ReceiptTypeDelivered, myLID, false, "A"),     // my LID
		rcpt(types.ReceiptTypeRead, types.EmptyJID, false, "A"), // no reader
	}
	for _, r := range skipped {
		if got := receiptRows(r, isOwn); len(got) != 0 {
			t.Errorf("receipt %q from %v (from_me=%v) should be skipped: %+v", r.Type, r.Sender, r.IsFromMe, got)
		}
	}
	// Zero timestamps fall back to now.
	r := rcpt(types.ReceiptTypeRead, alice, false, "A")
	r.Timestamp = time.Time{}
	if got := receiptRows(r, isOwn); got[0].Timestamp.IsZero() {
		t.Error("zero timestamp should be replaced")
	}
}

func TestSummarizeReceipts(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	rows := []store.Receipt{
		{MessageID: "M", Reader: "1@lid", Type: "delivered", Timestamp: t0},
		{MessageID: "M", Reader: "15550100@s.whatsapp.net", Type: "read", Timestamp: t0.Add(2 * time.Minute)},
		{MessageID: "M", Reader: "2@lid", Type: "delivered", Timestamp: t0.Add(time.Minute)},
	}
	// 1@lid and the phone JID are the same person.
	key := func(reader string) (string, Person) {
		if reader == "1@lid" || reader == "15550100@s.whatsapp.net" {
			return "15550100", Person{JID: reader, Phone: "15550100", Name: "Alice"}
		}
		return reader, Person{JID: reader}
	}
	status, delivered, read, played, readers := summarizeReceipts(rows, key)
	if status != "read" || !delivered.Equal(t0) || !read.Equal(t0.Add(2*time.Minute)) || played != nil {
		t.Errorf("summary = %s %v %v %v", status, delivered, read, played)
	}
	if len(readers) != 2 || readers[0].Reader.Name != "Alice" || readers[0].DeliveredAt == nil || readers[0].ReadAt == nil ||
		readers[1].ReadAt != nil {
		t.Errorf("readers = %+v", readers)
	}
	if status, _, _, _, readers := summarizeReceipts(nil, key); status != "" || len(readers) != 0 || readers == nil {
		t.Errorf("empty summary = %q %#v", status, readers)
	}
}

func TestCallRecord(t *testing.T) {
	ts := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	caller := types.JID{User: "15550100", Server: types.DefaultUserServer, Device: 7}
	meta := types.BasicCallMeta{From: caller, CallCreator: caller, CallID: "CALL1", Timestamp: ts}
	video := &waBinary.Node{Tag: "offer", Content: []waBinary.Node{{Tag: "audio"}, {Tag: "video"}}}
	c, ok := callRecord(&events.CallOffer{BasicCallMeta: meta, Data: video})
	if !ok || c.ID != "CALL1" || c.Caller != "15550100@s.whatsapp.net" || c.Media != "video" || c.Status != "ringing" || !c.Started.Equal(ts) {
		t.Errorf("offer = %+v", c)
	}
	c, _ = callRecord(&events.CallOffer{BasicCallMeta: meta, Data: &waBinary.Node{Tag: "offer"}})
	if c.Media != "audio" {
		t.Errorf("audio offer media = %q", c.Media)
	}
	gmeta := meta
	gmeta.GroupJID = types.NewJID("123-456", types.GroupServer)
	c, _ = callRecord(&events.CallOfferNotice{BasicCallMeta: gmeta, Media: "audio", Type: "group"})
	if !c.IsGroup || c.GroupJID != "123-456@g.us" || c.Media != "audio" {
		t.Errorf("group notice = %+v", c)
	}
	c, _ = callRecord(&events.CallTerminate{BasicCallMeta: meta, Reason: "timeout"})
	if c.Status != "ended" || c.Reason != "timeout" {
		t.Errorf("terminate = %+v", c)
	}
	for evt, want := range map[any]string{&events.CallAccept{BasicCallMeta: meta}: "accepted", &events.CallReject{BasicCallMeta: meta}: "rejected"} {
		if c, ok := callRecord(evt); !ok || c.Status != want {
			t.Errorf("%T = %+v", evt, c)
		}
	}
	if _, ok := callRecord(&events.CallRelayLatency{BasicCallMeta: meta}); ok {
		t.Error("relay latency should not be tracked")
	}
	// CallCreator wins over From.
	m2 := meta
	m2.From = types.NewJID("1234567890123456", types.HiddenUserServer)
	if c, _ := callRecord(&events.CallAccept{BasicCallMeta: m2}); c.Caller != "15550100@s.whatsapp.net" {
		t.Errorf("caller = %s", c.Caller)
	}
}

func TestQuotedID(t *testing.T) {
	ci := &waE2E.ContextInfo{StanzaID: proto.String("Q1")}
	cases := []struct {
		msg  *waE2E.Message
		want string
	}{
		{&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("x"), ContextInfo: ci}}, "Q1"},
		{&waE2E.Message{ImageMessage: &waE2E.ImageMessage{ContextInfo: ci}}, "Q1"},
		{&waE2E.Message{StickerMessage: &waE2E.StickerMessage{ContextInfo: ci}}, "Q1"},
		{&waE2E.Message{Conversation: proto.String("plain")}, ""},
		{&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("x")}}, ""},
		{nil, ""},
	}
	for i, c := range cases {
		if got := quotedID(c.msg); got != c.want {
			t.Errorf("case %d: quotedID = %q, want %q", i, got, c.want)
		}
	}
}

func TestExtractTextMoreTypes(t *testing.T) {
	cases := []struct {
		msg  *waE2E.Message
		want string
	}{
		{&waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{Contacts: []*waE2E.ContactMessage{
			{DisplayName: proto.String("Ann")}, {DisplayName: proto.String("Bob")}}}}, "[contacts] Ann, Bob"},
		{&waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{DegreesLatitude: proto.Float64(1.5), DegreesLongitude: proto.Float64(-2)}},
			"[live location] 1.500000,-2.000000"},
		{&waE2E.Message{GroupInviteMessage: &waE2E.GroupInviteMessage{GroupName: proto.String("Club")}}, "[group invite] Club"},
		{&waE2E.Message{EventMessage: &waE2E.EventMessage{Name: proto.String("Party"), StartTime: proto.Int64(1767225600),
			Location: &waE2E.LocationMessage{Name: proto.String("Home")}}}, "[event] Party at 2026-01-01T00:00:00Z @ Home"},
		{&waE2E.Message{PtvMessage: &waE2E.VideoMessage{Caption: proto.String("hey")}}, "hey"},
	}
	for _, c := range cases {
		if got := extractText(c.msg); got != c.want {
			t.Errorf("extractText = %q, want %q", got, c.want)
		}
	}
	if mi, ok := extractMedia(&waE2E.Message{PtvMessage: &waE2E.VideoMessage{}}, "ID"); !ok || mi.Type != "video" {
		t.Errorf("video note media = %+v %v", mi, ok)
	}
}

func TestMentionText(t *testing.T) {
	alice := types.NewJID("15550100", types.DefaultUserServer)
	bobLID := types.NewJID("123456789012345", types.HiddenUserServer)
	got := MentionText("hi @15550100 and @15550101", []types.JID{alice, bobLID}, map[string]string{"15550101": "123456789012345"})
	if want := "hi @15550100 and @123456789012345"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := MentionText("hello ", []types.JID{alice}, nil); got != "hello @15550100" {
		t.Errorf("missing mention should be appended: %q", got)
	}
}

func TestMatchPollOptions(t *testing.T) {
	opts := []string{"Thu", "Fri", "Sat"}
	got, err := MatchPollOptions(opts, []string{"Sat", "Thu", "Sat"})
	if err != nil || !reflect.DeepEqual(got, []string{"Thu", "Sat"}) {
		t.Errorf("got %v %v", got, err)
	}
	if got, err := MatchPollOptions(opts, nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("empty vote: %#v %v", got, err)
	}
	if _, err := MatchPollOptions(opts, []string{"thu"}); err == nil {
		t.Error("unknown option should be rejected")
	}
}

func TestLinkCode(t *testing.T) {
	for _, in := range []string{"https://wa.me/qr/ABCDEF", "wa.me/qr/ABCDEF", "https://api.whatsapp.com/qr/ABCDEF", "ABCDEF", "https://wa.me/qr/ABCDEF?x=1"} {
		if got, err := linkCode(in, "https://wa.me/qr/", "https://api.whatsapp.com/qr/"); err != nil || got != "ABCDEF" {
			t.Errorf("linkCode(%q) = %q %v", in, got, err)
		}
	}
	if _, err := linkCode("https://example.com/x/y", "https://wa.me/qr/"); err == nil {
		t.Error("foreign link should be rejected")
	}
}

func TestReplacedBackoff(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		if got := ReplacedBackoff(i + 1); got != w {
			t.Errorf("ReplacedBackoff(%d) = %s, want %s", i+1, got, w)
		}
	}
	if got := ReplacedBackoff(100); got != 30*time.Minute {
		t.Errorf("ReplacedBackoff(100) = %s", got)
	}
}

func TestStreamReplacedSchedulesReconnect(t *testing.T) {
	svc, err := New(context.Background(), Options{DataDir: t.TempDir(), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	svc.handleStreamReplaced()
	svc.handleStreamReplaced()
	svc.replacedMu.Lock()
	n, timer := svc.replacedCount, svc.reconnectTimer
	svc.replacedMu.Unlock()
	if n != 2 || timer == nil {
		t.Fatalf("count=%d timer=%v", n, timer)
	}
	if _, ok := svc.Status()["last_stream_replaced"]; !ok {
		t.Error("status should report the replacement")
	}
	svc.Close()
	if timer.Stop() {
		t.Error("Close should stop the pending reconnect")
	}
	svc.handleStreamReplaced() // after Close: no new timer
	if svc.reconnectTimer != timer {
		t.Error("no reconnect may be scheduled after Close")
	}
}
