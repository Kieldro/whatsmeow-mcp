package wa

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// MessageView is a stored message with its sender resolved.
type MessageView struct {
	ID        string    `json:"id"`
	ChatJID   string    `json:"chat_jid"`
	ChatName  string    `json:"chat_name,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	FromMe    bool      `json:"from_me"`
	Sender    Person    `json:"sender"`
	Content   string    `json:"content,omitempty"`
	MediaType string    `json:"media_type,omitempty"`
	Filename  string    `json:"filename,omitempty"`
}

func (s *Service) view(ctx context.Context, r *Resolver, m store.Message) MessageView {
	v := MessageView{ID: m.ID, ChatJID: m.ChatJID, ChatName: m.ChatName, Timestamp: m.Timestamp,
		FromMe: m.IsFromMe, Content: m.Content, MediaType: m.MediaType, Filename: m.Filename}
	if m.IsFromMe {
		v.Sender = Person{Name: "Me"}
		if id := s.Client.Store.ID; id != nil {
			v.Sender.JID, v.Sender.Phone = id.ToNonAD().String(), id.User
		}
	} else {
		v.Sender = r.Resolve(ctx, m.Sender)
	}
	return v
}

// ResolveChat turns user input (phone, JID or chat name) into candidate chat JIDs.
func (s *Service) ResolveChat(ctx context.Context, input string) ([]string, error) {
	if c := s.ChatCandidates(ctx, input); len(c) > 0 {
		return c, nil
	}
	s.FixChatNames(ctx)
	chats, err := s.Store.ListChats(store.ChatFilter{Query: input, Limit: 10})
	if err != nil {
		return nil, err
	}
	if len(chats) == 0 {
		return nil, fmt.Errorf("no chat matches %q; use search_contacts or list_chats to find the JID", input)
	}
	for _, c := range chats {
		if strings.EqualFold(c.Name, input) {
			return s.ChatCandidates(ctx, c.JID), nil
		}
	}
	if len(chats) == 1 {
		return s.ChatCandidates(ctx, chats[0].JID), nil
	}
	var names []string
	for _, c := range chats {
		names = append(names, fmt.Sprintf("%s (%s)", c.Name, c.JID))
	}
	return nil, fmt.Errorf("%q is ambiguous, matches: %s", input, strings.Join(names, "; "))
}

// MessageQuery filters ListMessages.
type MessageQuery struct {
	Chat          string
	Sender        string
	Query         string
	After, Before time.Time
	Limit, Page   int
	IncludeStatus bool
	OnlyMedia     bool
}

// ListMessages returns matching messages, newest first.
func (s *Service) ListMessages(ctx context.Context, q MessageQuery) ([]MessageView, error) {
	f := store.MessageFilter{Query: q.Query, After: q.After, Before: q.Before, Limit: q.Limit,
		IncludeStatus: q.IncludeStatus, OnlyMedia: q.OnlyMedia}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	f.Offset = max(q.Page, 0) * f.Limit
	if q.Chat != "" {
		c, err := s.ResolveChat(ctx, q.Chat)
		if err != nil {
			return nil, err
		}
		f.ChatJIDs = c
		if !q.IncludeStatus {
			for _, j := range c {
				if j == jidutil.StatusBroadcast {
					f.IncludeStatus = true // explicitly asked for the Status chat
				}
			}
		}
	}
	if q.Sender != "" {
		f.Senders = s.SenderCandidates(ctx, q.Sender)
		if len(f.Senders) == 0 {
			return nil, fmt.Errorf("sender %q is not a phone number or JID", q.Sender)
		}
	}
	msgs, err := s.Store.ListMessages(f)
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	out := make([]MessageView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, s.view(ctx, r, m))
	}
	return out, nil
}

// MessageContext is a message with its neighbours.
type MessageContext struct {
	Message MessageView   `json:"message"`
	Before  []MessageView `json:"before"`
	After   []MessageView `json:"after"`
}

// GetMessageContext returns messages around one message. chat may be empty.
func (s *Service) GetMessageContext(ctx context.Context, chat, id string, before, after int) (MessageContext, error) {
	chatJID := ""
	if chat != "" {
		c, err := s.ResolveChat(ctx, chat)
		if err != nil {
			return MessageContext{}, err
		}
		chatJID = c[0]
	}
	m, err := s.Store.GetMessage(chatJID, id)
	if err != nil {
		return MessageContext{}, fmt.Errorf("message %s not found", id)
	}
	prev, next, err := s.Store.Context(m, before, after)
	if err != nil {
		return MessageContext{}, err
	}
	r := s.NewResolver()
	out := MessageContext{Message: s.view(ctx, r, m), Before: []MessageView{}, After: []MessageView{}}
	for _, p := range prev {
		out.Before = append(out.Before, s.view(ctx, r, p))
	}
	for _, n := range next {
		out.After = append(out.After, s.view(ctx, r, n))
	}
	return out, nil
}

// ChatView is a chat with resolved last-message sender.
type ChatView struct {
	JID             string    `json:"jid"`
	Name            string    `json:"name"`
	IsGroup         bool      `json:"is_group"`
	Phone           string    `json:"phone,omitempty"`
	LastMessageTime time.Time `json:"last_message_time"`
	LastMessage     string    `json:"last_message,omitempty"`
	LastSender      string    `json:"last_sender,omitempty"`
	LastIsFromMe    bool      `json:"last_is_from_me,omitempty"`
}

func (s *Service) chatView(ctx context.Context, r *Resolver, c store.Chat) ChatView {
	v := ChatView{JID: c.JID, Name: c.Name, IsGroup: c.IsGroup(), LastMessageTime: c.LastMessageTime,
		LastMessage: c.LastMessage, LastIsFromMe: c.LastIsFromMe}
	if !v.IsGroup && c.JID != jidutil.StatusBroadcast {
		v.Phone = r.Resolve(ctx, c.JID).Phone
	}
	if c.LastIsFromMe {
		v.LastSender = "Me"
	} else if c.LastSender != "" {
		p := r.Resolve(ctx, c.LastSender)
		v.LastSender = p.Name
		if v.LastSender == "" {
			v.LastSender = p.JID
		}
	}
	return v
}

// ListChats lists chats, most recent first by default.
func (s *Service) ListChats(ctx context.Context, f store.ChatFilter) ([]ChatView, error) {
	s.FixChatNames(ctx)
	chats, err := s.Store.ListChats(f)
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	out := make([]ChatView, 0, len(chats))
	for _, c := range chats {
		out = append(out, s.chatView(ctx, r, c))
	}
	return out, nil
}

// GetChat returns one chat by phone, JID or name.
func (s *Service) GetChat(ctx context.Context, input string) (ChatView, error) {
	cands, err := s.ResolveChat(ctx, input)
	if err != nil {
		return ChatView{}, err
	}
	r := s.NewResolver()
	var best *store.Chat
	for _, j := range cands {
		c, err := s.Store.GetChat(j)
		if err != nil {
			continue
		}
		if best == nil || c.LastMessageTime.After(best.LastMessageTime) {
			best = &c
		}
	}
	if best == nil {
		return ChatView{}, fmt.Errorf("no stored chat for %q", input)
	}
	return s.chatView(ctx, r, *best), nil
}

// Contact is a search_contacts hit.
type Contact struct {
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
	// JID is the chat that holds the conversation (often a LID chat), usable
	// directly as list_messages' chat and as a send recipient.
	JID string `json:"jid"`
	LID string `json:"lid,omitempty"`
	// HasChat reports whether any messages with this contact are stored.
	HasChat bool `json:"has_chat"`
}

// SearchContacts finds people by name or number across stored chats and the
// synced address book.
func (s *Service) SearchContacts(ctx context.Context, query string, limit int) ([]Contact, error) {
	if limit <= 0 {
		limit = 50
	}
	s.FixChatNames(ctx)
	r := s.NewResolver()
	found := map[string]*Contact{} // keyed by phone (or JID if unknown)
	var order []string
	add := func(jid, name string, hasChat bool) {
		p := r.Resolve(ctx, jid)
		key := p.Phone
		if key == "" {
			key = p.JID
		}
		if name == "" {
			name = p.Name
		}
		if c, ok := found[key]; ok {
			if c.Name == "" {
				c.Name = name
			}
			if hasChat && !c.HasChat {
				c.JID, c.HasChat = p.JID, true
			}
			return
		}
		found[key] = &Contact{Name: name, Phone: p.Phone, JID: p.JID, LID: p.LID, HasChat: hasChat}
		order = append(order, key)
	}
	chats, err := s.Store.ListChats(store.ChatFilter{Query: query, OnlyDirect: true, Limit: limit})
	if err != nil {
		return nil, err
	}
	for _, c := range chats {
		add(c.JID, c.Name, true)
	}
	if cs := s.Client.Store.Contacts; cs != nil {
		all, err := cs.GetAllContacts(ctx)
		if err == nil {
			ql := strings.ToLower(query)
			qd := jidutil.DigitsOnly(query)
			var hits []types.JID
			for j, ci := range all {
				match := strings.Contains(strings.ToLower(contactName(ci)), ql) ||
					strings.Contains(strings.ToLower(ci.FullName), ql) ||
					strings.Contains(strings.ToLower(ci.PushName), ql) ||
					(qd != "" && strings.Contains(j.User, qd))
				if match {
					hits = append(hits, j)
				}
			}
			sort.Slice(hits, func(a, b int) bool { return hits[a].String() < hits[b].String() })
			for _, j := range hits {
				if len(order) >= limit {
					break
				}
				// Prefer an existing chat (phone or LID) for this person.
				jid, has := j.String(), false
				for _, c := range s.ChatCandidates(ctx, jid) {
					if s.Store.ChatExists(c) {
						jid, has = c, true
						break
					}
				}
				add(jid, contactName(all[j]), has)
			}
		}
	}
	out := make([]Contact, 0, len(order))
	for _, k := range order {
		out = append(out, *found[k])
	}
	return out, nil
}
