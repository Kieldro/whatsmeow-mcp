package wa

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

func TestCompareLID(t *testing.T) {
	pn := types.NewJID("15550100", types.DefaultUserServer)
	oldLID := types.NewJID("111111111111111", types.HiddenUserServer)
	newLID := types.NewJID("222222222222222", types.HiddenUserServer)

	c, changed := compareLID(pn, oldLID, newLID)
	if !changed || c != (LIDChange{Phone: "15550100", OldLID: "111111111111111@lid", NewLID: "222222222222222@lid"}) {
		t.Fatalf("different LIDs: changed=%v %+v", changed, c)
	}
	// A device suffix on either side does not count as a change.
	if _, changed := compareLID(pn, oldLID, types.JID{User: oldLID.User, Server: types.HiddenUserServer, Device: 4}); changed {
		t.Error("same LID user with a device part should not be a change")
	}
	// The device part is dropped from the reported LIDs.
	if c, _ := compareLID(pn, types.JID{User: oldLID.User, Server: types.HiddenUserServer, Device: 2}, newLID); c.OldLID != "111111111111111@lid" {
		t.Errorf("old LID should be reported without device: %q", c.OldLID)
	}
	for name, tc := range map[string][2]types.JID{
		"same":       {oldLID, oldLID},
		"no cache":   {types.EmptyJID, newLID},
		"no server":  {oldLID, types.EmptyJID},
		"both empty": {types.EmptyJID, types.EmptyJID},
	} {
		if _, changed := compareLID(pn, tc[0], tc[1]); changed {
			t.Errorf("%s: should not be a change", name)
		}
	}
}

func TestRecipientCache(t *testing.T) {
	var c recipientCache
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	if c.fresh("15550100", now) {
		t.Fatal("unchecked recipient should not be fresh")
	}
	c.mark("15550100", now)
	if !c.fresh("15550100", now.Add(recipientTTL-time.Second)) {
		t.Error("recipient checked within the TTL should be fresh")
	}
	if c.fresh("15550100", now.Add(recipientTTL)) {
		t.Error("recipient should expire after the TTL")
	}
	if c.fresh("15550101", now) {
		t.Error("other recipients are unaffected")
	}
	c.forget("15550100")
	if c.fresh("15550100", now) {
		t.Error("forgotten recipient should be re-checked")
	}
}

func TestResolveRecipientSkipsNonPhoneChats(t *testing.T) {
	svc, err := New(context.Background(), Options{DataDir: t.TempDir(), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	for _, j := range []types.JID{
		types.NewJID("123456789-987654321", types.GroupServer),
		types.NewJID("111111111111111", types.HiddenUserServer),
		types.StatusBroadcastJID,
	} {
		if _, changed := svc.resolveRecipient(context.Background(), j); changed {
			t.Errorf("%v: unexpected change", j)
		}
	}
}

func TestGetMessageStatus(t *testing.T) {
	svc, err := New(context.Background(), Options{DataDir: t.TempDir(), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	ctx := context.Background()
	chat := "15550100@s.whatsapp.net"
	sent := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	if err := svc.Store.UpsertChat(chat, "", sent); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store.StoreMessage(store.Message{ID: "M1", ChatJID: chat, Sender: "15550199@s.whatsapp.net",
		Content: "hi", Timestamp: sent, IsFromMe: true}); err != nil {
		t.Fatal(err)
	}

	st, err := svc.GetMessageStatus(ctx, chat, "M1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "sent" || !st.Stored || st.SentAt == nil || !st.SentAt.Equal(sent) || st.DeliveredAt != nil {
		t.Fatalf("no receipts yet: %+v", st)
	}

	// The receipt may arrive under the contact's LID chat.
	for _, r := range []store.Receipt{
		{MessageID: "M1", ChatJID: "111111111111111@lid", Reader: "111111111111111@lid", Type: store.ReceiptDelivered, Timestamp: sent.Add(time.Second)},
		{MessageID: "M1", ChatJID: "111111111111111@lid", Reader: "111111111111111@lid", Type: store.ReceiptRead, Timestamp: sent.Add(time.Minute)},
	} {
		if err := svc.Store.SaveReceipt(r); err != nil {
			t.Fatal(err)
		}
	}
	st, err = svc.GetMessageStatus(ctx, chat, "M1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "read" || st.DeliveredAt == nil || st.ReadAt == nil || !st.ReadAt.Equal(sent.Add(time.Minute)) {
		t.Fatalf("after receipts: %+v", st)
	}

	st, err = svc.GetMessageStatus(ctx, chat, "NOPE")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "unknown" || st.Stored {
		t.Fatalf("unknown message: %+v", st)
	}
	if _, err := svc.GetMessageStatus(ctx, chat, ""); err == nil {
		t.Fatal("empty id should be rejected")
	}
}
