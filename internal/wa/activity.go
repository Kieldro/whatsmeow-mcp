package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// ownJID returns this account's phone-number JID (no device), or EmptyJID.
func (s *Service) ownJID() types.JID {
	if id := s.Client.Store.ID; id != nil {
		return id.ToNonAD()
	}
	return types.EmptyJID
}

// isOwnUser reports whether j is this account (by phone number or LID).
func (s *Service) isOwnUser(j types.JID) bool {
	if j.IsEmpty() {
		return false
	}
	if id := s.Client.Store.ID; id != nil && j.User == id.User && j.Server == types.DefaultUserServer {
		return true
	}
	lid := s.Client.Store.LID
	return lid.User != "" && j.User == lid.User && j.Server == types.HiddenUserServer
}

// storedReceiptType maps a whatsmeow receipt type to the stored type. Only
// delivered, read and played receipts are tracked; self, sender, retry and
// other protocol receipts are not.
func storedReceiptType(t types.ReceiptType) (string, bool) {
	switch t {
	case types.ReceiptTypeDelivered:
		return store.ReceiptDelivered, true
	case types.ReceiptTypeRead:
		return store.ReceiptRead, true
	case types.ReceiptTypePlayed:
		return store.ReceiptPlayed, true
	}
	return "", false
}

// receiptRows converts a receipt event from another user into rows to store.
// isOwn reports whether a JID is this account; receipts from it are skipped.
func receiptRows(evt *events.Receipt, isOwn func(types.JID) bool) []store.Receipt {
	typ, ok := storedReceiptType(evt.Type)
	if !ok || evt.IsFromMe || evt.Sender.IsEmpty() || isOwn(evt.Sender) {
		return nil
	}
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	chat := evt.Chat.ToNonAD().String()
	reader := evt.Sender.ToNonAD().String()
	out := make([]store.Receipt, 0, len(evt.MessageIDs))
	for _, id := range evt.MessageIDs {
		if id == "" {
			continue
		}
		out = append(out, store.Receipt{MessageID: id, ChatJID: chat, Reader: reader, Type: typ, Timestamp: ts})
	}
	return out
}

func (s *Service) handleReceipt(evt *events.Receipt) {
	for _, r := range receiptRows(evt, s.isOwnUser) {
		if err := s.Store.SaveReceipt(r); err != nil {
			s.log.Warnf("store receipt: %v", err)
			return
		}
	}
}

// recordReaction stores a (possibly encrypted) reaction message and reports
// whether msg was a reaction at all.
func (s *Service) recordReaction(ctx context.Context, evt *events.Message) bool {
	msg := evt.Message
	r := msg.GetReactionMessage()
	if r == nil {
		if msg.GetEncReactionMessage() == nil {
			return false
		}
		dec, err := s.Client.DecryptReaction(ctx, evt)
		if err != nil {
			s.log.Debugf("decrypt reaction %s: %v", evt.Info.ID, err)
			return true
		}
		r = dec
		if r.GetKey() == nil {
			r.Key = msg.GetEncReactionMessage().GetTargetMessageKey()
		}
	}
	target := r.GetKey().GetID()
	if target == "" {
		return true
	}
	sender := evt.Info.Sender.ToNonAD()
	if evt.Info.IsFromMe || s.isOwnUser(sender) {
		sender = s.ownJID()
	}
	ts := evt.Info.Timestamp
	if ms := r.GetSenderTimestampMS(); ms > 0 {
		ts = time.UnixMilli(ms)
	}
	if err := s.Store.SaveReaction(store.Reaction{MessageID: target, ChatJID: evt.Info.Chat.ToNonAD().String(),
		Sender: sender.String(), Emoji: r.GetText(), Timestamp: ts}); err != nil {
		s.log.Warnf("store reaction: %v", err)
	}
	return true
}

// quotedID returns the id of the message msg replies to, if any. Every
// message type carries its reply context in a contextInfo field.
func quotedID(msg *waE2E.Message) string {
	return contextInfo(msg).GetStanzaID()
}

func (s *Service) handleDeleteForMe(evt *events.DeleteForMe) {
	if err := s.Store.DeleteMessage(evt.ChatJID.ToNonAD().String(), evt.MessageID); err != nil {
		s.log.Warnf("delete-for-me sync: %v", err)
	}
}

// callerOf picks the JID that started a call.
func callerOf(m types.BasicCallMeta) types.JID {
	if !m.CallCreator.IsEmpty() {
		return m.CallCreator.ToNonAD()
	}
	return m.From.ToNonAD()
}

func callTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// callRecord converts a call event into a row update; ok is false for
// events that are not tracked.
func callRecord(evt any) (store.Call, bool) {
	base := func(m types.BasicCallMeta, status string) store.Call {
		c := store.Call{ID: m.CallID, Caller: callerOf(m).String(), Status: status, Started: callTime(m.Timestamp), Updated: callTime(m.Timestamp)}
		if !m.GroupJID.IsEmpty() {
			c.GroupJID, c.IsGroup = m.GroupJID.String(), true
		}
		return c
	}
	switch v := evt.(type) {
	case *events.CallOffer:
		c := base(v.BasicCallMeta, "ringing")
		c.Media = "audio"
		if v.Data != nil {
			if _, ok := v.Data.GetOptionalChildByTag("video"); ok {
				c.Media = "video"
			}
		}
		return c, true
	case *events.CallOfferNotice:
		c := base(v.BasicCallMeta, "ringing")
		c.Media = v.Media
		c.IsGroup = c.IsGroup || v.Type == "group"
		return c, true
	case *events.CallAccept:
		return base(v.BasicCallMeta, "accepted"), true
	case *events.CallReject:
		return base(v.BasicCallMeta, "rejected"), true
	case *events.CallTerminate:
		c := base(v.BasicCallMeta, "ended")
		c.Reason = v.Reason
		return c, true
	}
	return store.Call{}, false
}

func (s *Service) handleCall(evt any) {
	c, ok := callRecord(evt)
	if !ok || c.ID == "" {
		return
	}
	if c.Status != "ringing" {
		// Updates only move the status; the start time stays from the offer.
		if old, err := s.Store.GetCall(c.ID); err == nil {
			c.Started = old.Started
		}
	}
	if err := s.Store.SaveCall(c); err != nil {
		s.log.Warnf("store call: %v", err)
	}
}

// CallView is a stored call with the caller resolved.
type CallView struct {
	ID        string    `json:"call_id"`
	Caller    Person    `json:"caller"`
	GroupJID  string    `json:"group_jid,omitempty"`
	IsGroup   bool      `json:"is_group,omitempty"`
	Media     string    `json:"media,omitempty"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ListCalls returns calls seen by this client, newest first.
func (s *Service) ListCalls(ctx context.Context, limit int) ([]CallView, error) {
	calls, err := s.Store.ListCalls(min(max(limit, 0), 200))
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	out := make([]CallView, 0, len(calls))
	for _, c := range calls {
		out = append(out, CallView{ID: c.ID, Caller: r.Resolve(ctx, c.Caller), GroupJID: c.GroupJID, IsGroup: c.IsGroup,
			Media: c.Media, Status: c.Status, Reason: c.Reason, StartedAt: c.Started, UpdatedAt: c.Updated})
	}
	return out, nil
}

// RejectCall declines an incoming call. caller may be empty if the call was
// recorded by this client (see ListCalls).
func (s *Service) RejectCall(ctx context.Context, callID, caller string) error {
	if callID == "" {
		return invalidf("call_id is required")
	}
	var from types.JID
	if caller != "" {
		j, err := jidutil.Parse(caller)
		if err != nil {
			return invalidf("%v", err)
		}
		from = j
	} else {
		c, err := s.Store.GetCall(callID)
		if err != nil {
			return invalidf("unknown call %q; pass caller, or see list_calls", callID)
		}
		j, ok := jidutil.FromStoredSender(c.Caller)
		if !ok {
			return invalidf("call %q has no known caller; pass caller", callID)
		}
		from = j
	}
	if err := s.Ready(); err != nil {
		return err
	}
	if err := s.Client.RejectCall(ctx, from, callID); err != nil {
		return err
	}
	_ = s.Store.SaveCall(store.Call{ID: callID, Caller: from.String(), Status: "rejected", Started: time.Now(), Updated: time.Now()})
	return nil
}

// ReaderReceipt is when one reader's device reached each receipt state.
type ReaderReceipt struct {
	Reader      Person     `json:"reader"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	PlayedAt    *time.Time `json:"played_at,omitempty"`
}

// MessageReceipts summarizes the receipts of one message.
type MessageReceipts struct {
	ID        string     `json:"id"`
	ChatJID   string     `json:"chat_jid,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	FromMe    bool       `json:"from_me"`
	Content   string     `json:"content,omitempty"`
	// Status is the furthest state any reader reached: delivered, read or
	// played; empty when no receipt was recorded.
	Status      string          `json:"status,omitempty"`
	DeliveredAt *time.Time      `json:"delivered_at,omitempty"`
	ReadAt      *time.Time      `json:"read_at,omitempty"`
	PlayedAt    *time.Time      `json:"played_at,omitempty"`
	Readers     []ReaderReceipt `json:"readers"`
}

func earliest(dst **time.Time, t time.Time) {
	if *dst == nil || t.Before(**dst) {
		tt := t
		*dst = &tt
	}
}

// summarizeReceipts groups receipt rows per reader (merging a person's phone
// and LID identities via key) and computes the overall status.
func summarizeReceipts(rows []store.Receipt, key func(reader string) (string, Person)) (status string, delivered, read, played *time.Time, readers []ReaderReceipt) {
	byKey := map[string]*ReaderReceipt{}
	var order []string
	for _, r := range rows {
		k, p := key(r.Reader)
		rr, ok := byKey[k]
		if !ok {
			rr = &ReaderReceipt{Reader: p}
			byKey[k] = rr
			order = append(order, k)
		}
		switch r.Type {
		case store.ReceiptDelivered:
			earliest(&rr.DeliveredAt, r.Timestamp)
			earliest(&delivered, r.Timestamp)
		case store.ReceiptRead:
			earliest(&rr.ReadAt, r.Timestamp)
			earliest(&read, r.Timestamp)
		case store.ReceiptPlayed:
			earliest(&rr.PlayedAt, r.Timestamp)
			earliest(&played, r.Timestamp)
		}
		if store.ReceiptRank(r.Type) > store.ReceiptRank(status) {
			status = r.Type
		}
	}
	readers = make([]ReaderReceipt, 0, len(order))
	for _, k := range order {
		readers = append(readers, *byKey[k])
	}
	return
}

// GetMessageReceipts reports delivered/read/played receipts for messages.
// With ids empty it covers the account's latest sent messages in chat.
func (s *Service) GetMessageReceipts(ctx context.Context, chat string, ids []string, limit int) ([]MessageReceipts, error) {
	var chats []string
	if chat != "" {
		c, err := s.ResolveChat(ctx, chat)
		if err != nil {
			return nil, invalidf("%v", err)
		}
		chats = c
	}
	var msgs []store.Message
	switch {
	case len(ids) > 0:
		if len(ids) > 200 {
			return nil, invalidf("at most 200 message_ids per call")
		}
		for _, id := range ids {
			m, err := s.findMessage(chats, id)
			if err != nil {
				m = store.Message{ID: id}
			}
			msgs = append(msgs, m)
		}
	case len(chats) > 0:
		if limit <= 0 {
			limit = 20
		}
		var err error
		msgs, err = s.Store.ListMessages(store.MessageFilter{ChatJIDs: chats, OnlyFromMe: true, IncludeStatus: true, Limit: min(limit, 200)})
		if err != nil {
			return nil, err
		}
	default:
		return nil, invalidf("pass chat and/or message_ids")
	}
	idList := make([]string, len(msgs))
	for i, m := range msgs {
		idList[i] = m.ID
	}
	rows, err := s.Store.Receipts(idList)
	if err != nil {
		return nil, err
	}
	perMsg := map[string][]store.Receipt{}
	for _, r := range rows {
		perMsg[r.MessageID] = append(perMsg[r.MessageID], r)
	}
	res := s.NewResolver()
	key := func(reader string) (string, Person) {
		p := res.Resolve(ctx, reader)
		if p.Phone != "" {
			return p.Phone, p
		}
		return p.JID, p
	}
	out := make([]MessageReceipts, 0, len(msgs))
	for _, m := range msgs {
		mr := MessageReceipts{ID: m.ID, ChatJID: m.ChatJID, FromMe: m.IsFromMe, Content: m.Content}
		if !m.Timestamp.IsZero() {
			t := m.Timestamp
			mr.Timestamp = &t
		}
		if mr.ChatJID == "" && len(perMsg[m.ID]) > 0 {
			mr.ChatJID = perMsg[m.ID][0].ChatJID
		}
		mr.Status, mr.DeliveredAt, mr.ReadAt, mr.PlayedAt, mr.Readers = summarizeReceipts(perMsg[m.ID], key)
		out = append(out, mr)
	}
	return out, nil
}

// findMessage looks a message up in any of chats (or any chat if none).
func (s *Service) findMessage(chats []string, id string) (store.Message, error) {
	if len(chats) == 0 {
		return s.Store.GetMessage("", id)
	}
	for _, c := range chats {
		if m, err := s.Store.GetMessage(c, id); err == nil {
			return m, nil
		}
	}
	return store.Message{}, store.ErrNotFound
}

// ReactionView is one person's current reaction to a message.
type ReactionView struct {
	Emoji     string    `json:"emoji"`
	From      Person    `json:"from"`
	Timestamp time.Time `json:"timestamp"`
}

// MessageReactions lists the reactions to one message.
type MessageReactions struct {
	ID        string         `json:"id"`
	Reactions []ReactionView `json:"reactions"`
}

// GetMessageReactions returns the current reactions to each message.
func (s *Service) GetMessageReactions(ctx context.Context, ids []string) ([]MessageReactions, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, invalidf("pass 1-200 message_ids")
	}
	rows, err := s.Store.Reactions(ids)
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	per := map[string][]ReactionView{}
	for _, x := range rows {
		per[x.MessageID] = append(per[x.MessageID], ReactionView{Emoji: x.Emoji, From: r.Resolve(ctx, x.Sender), Timestamp: x.Timestamp})
	}
	out := make([]MessageReactions, 0, len(ids))
	for _, id := range ids {
		rv := per[id]
		if rv == nil {
			rv = []ReactionView{}
		}
		out = append(out, MessageReactions{ID: id, Reactions: rv})
	}
	return out, nil
}

// ReactionBrief is a compact reaction shown inline in message listings.
type ReactionBrief struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}

// enrich adds receipt status (own messages) and reactions to message views.
func (s *Service) enrich(ctx context.Context, r *Resolver, views []MessageView) {
	if len(views) == 0 {
		return
	}
	var all, mine []string
	for _, v := range views {
		all = append(all, v.ID)
		if v.FromMe {
			mine = append(mine, v.ID)
		}
	}
	status, err := s.Store.ReceiptStatus(mine)
	if err != nil {
		s.log.Warnf("receipt status: %v", err)
	}
	reactions, err := s.Store.Reactions(all)
	if err != nil {
		s.log.Warnf("reactions: %v", err)
	}
	per := map[string][]ReactionBrief{}
	for _, x := range reactions {
		p := r.Resolve(ctx, x.Sender)
		from := p.Name
		if from == "" {
			from = p.Phone
		}
		if from == "" {
			from = p.JID
		}
		per[x.MessageID] = append(per[x.MessageID], ReactionBrief{Emoji: x.Emoji, From: from})
	}
	for i := range views {
		if views[i].FromMe {
			views[i].Status = status[views[i].ID]
		}
		views[i].Reactions = per[views[i].ID]
	}
}
