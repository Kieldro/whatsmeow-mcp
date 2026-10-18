package wa

import (
	"context"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// recipientTTL is how long a phone number's LID, checked against the server
// before a send, is trusted before it is checked again.
const recipientTTL = 10 * time.Minute

// recipientCache remembers when each phone number's LID was last checked.
type recipientCache struct {
	mu      sync.Mutex
	checked map[string]time.Time // phone number (JID user) -> last check
}

func (c *recipientCache) fresh(user string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.checked[user]
	return ok && now.Sub(t) < recipientTTL
}

func (c *recipientCache) mark(user string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checked == nil {
		c.checked = map[string]time.Time{}
	}
	c.checked[user] = now
}

func (c *recipientCache) forget(user string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.checked, user)
}

// LIDChange describes a phone number whose LID differs from the cached one.
type LIDChange struct {
	Phone  string `json:"phone"`
	OldLID string `json:"old_lid"`
	NewLID string `json:"new_lid"`
}

// compareLID reports whether the server's LID for a phone number differs
// from the cached mapping. A missing cached mapping is not a change (there
// was nothing stale to send to), and neither is a missing server LID.
func compareLID(pn, cached, fresh types.JID) (LIDChange, bool) {
	if cached.IsEmpty() || fresh.IsEmpty() || cached.User == fresh.User {
		return LIDChange{}, false
	}
	return LIDChange{Phone: pn.User, OldLID: cached.ToNonAD().String(), NewLID: fresh.ToNonAD().String()}, true
}

// refreshLID asks the server for pn's current LID (and devices), stores the
// mapping through whatsmeow's LID store, and reports whether it changed.
func (s *Service) refreshLID(ctx context.Context, pn types.JID) (LIDChange, bool, error) {
	pn = pn.ToNonAD()
	cached, err := s.Client.Store.LIDs.GetLIDForPN(ctx, pn)
	if err != nil {
		s.log.Warnf("read cached LID for %s: %v", pn, err)
	}
	// GetUserInfo is a USync query; whatsmeow also writes the returned
	// PN->LID mapping into its store as a side effect.
	info, err := s.Client.GetUserInfo(ctx, []types.JID{pn})
	if err != nil {
		return LIDChange{}, false, err
	}
	fresh := info[pn].LID.ToNonAD()
	if !fresh.IsEmpty() && fresh.User != cached.User {
		// Make sure the store points at the new LID even if the side effect
		// above failed (PutLIDMapping replaces any row for this PN or LID).
		if err := s.Client.Store.LIDs.PutLIDMapping(ctx, fresh, pn); err != nil {
			s.log.Warnf("update LID mapping for %s: %v", pn, err)
		}
	}
	change, changed := compareLID(pn, cached, fresh)
	return change, changed, nil
}

// resolveRecipient re-checks a phone-number recipient's LID against the
// server (at most once per recipientTTL) before sending, so a contact who
// re-registered WhatsApp is not messaged at a stale identity. Lookup errors
// are logged and do not block the send.
func (s *Service) resolveRecipient(ctx context.Context, chat types.JID) (LIDChange, bool) {
	if chat.Server != types.DefaultUserServer {
		return LIDChange{}, false
	}
	pn := chat.ToNonAD()
	now := time.Now()
	if s.recipients.fresh(pn.User, now) {
		return LIDChange{}, false
	}
	change, changed, err := s.refreshLID(ctx, pn)
	if err != nil {
		s.log.Warnf("resolve recipient %s before send: %v", pn, err)
		return LIDChange{}, false
	}
	s.recipients.mark(pn.User, now)
	if changed {
		s.log.Warnf("Identity changed for %s: LID %s -> %s (updated mapping before sending)", pn, change.OldLID, change.NewLID)
		if err := s.Store.SaveIdentityChange(store.IdentityChange{JID: pn.String(), Phone: pn.User,
			OldLID: change.OldLID, NewLID: change.NewLID, Source: store.IdentitySourceSend, Timestamp: now}); err != nil {
			s.log.Warnf("store identity change: %v", err)
		}
	}
	return change, changed
}

// handleIdentityChange records an identity-change event (a contact's new
// primary device, or an untrusted identity seen while decrypting), forgets
// the recipient check so the next send re-resolves, and refreshes the
// contact's LID mapping from the server.
func (s *Service) handleIdentityChange(evt *events.IdentityChange) {
	jid := evt.JID.ToNonAD()
	s.log.Warnf("Identity change for %s (implicit=%v)", jid, evt.Implicit)
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rec := store.IdentityChange{JID: jid.String(), Source: store.IdentitySourceNotification, Implicit: evt.Implicit, Timestamp: ts}
		pn := jid
		if jid.Server == types.HiddenUserServer {
			rec.OldLID = jid.String()
			if p, err := s.Client.Store.LIDs.GetPNForLID(ctx, jid); err == nil {
				pn = p
			}
		}
		if pn.Server == types.DefaultUserServer {
			rec.Phone = pn.User
			s.recipients.forget(pn.User)
			if s.Ready() == nil {
				change, changed, err := s.refreshLID(ctx, pn)
				switch {
				case err != nil:
					s.log.Warnf("refresh LID for %s after identity change: %v", pn, err)
				case changed:
					rec.OldLID, rec.NewLID = change.OldLID, change.NewLID
					s.log.Warnf("Identity change for %s moved LID %s -> %s", pn, change.OldLID, change.NewLID)
				default:
					s.recipients.mark(pn.User, time.Now())
				}
			}
		}
		if err := s.Store.SaveIdentityChange(rec); err != nil {
			s.log.Warnf("store identity change: %v", err)
		}
	}()
}

// IdentityChangeView is a recorded identity change with the contact's name.
type IdentityChangeView struct {
	store.IdentityChange
	Name string `json:"name,omitempty"`
}

// ListIdentityChanges returns recent identity changes, newest first.
func (s *Service) ListIdentityChanges(ctx context.Context, limit int) ([]IdentityChangeView, error) {
	rows, err := s.Store.IdentityChanges(min(max(limit, 0), 200))
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	out := make([]IdentityChangeView, 0, len(rows))
	for _, c := range rows {
		v := IdentityChangeView{IdentityChange: c}
		key := c.JID
		if c.Phone != "" {
			key = c.Phone + "@" + types.DefaultUserServer
		}
		v.Name = r.Resolve(ctx, key).Name
		out = append(out, v)
	}
	return out, nil
}

// MessageStatus is where one message stands: sent, delivered, read or played.
type MessageStatus struct {
	ID      string `json:"id"`
	ChatJID string `json:"chat_jid"`
	// Stored reports whether the message is in the local store.
	Stored      bool            `json:"stored"`
	FromMe      bool            `json:"from_me"`
	Status      string          `json:"status"`
	SentAt      *time.Time      `json:"sent_at,omitempty"`
	DeliveredAt *time.Time      `json:"delivered_at,omitempty"`
	ReadAt      *time.Time      `json:"read_at,omitempty"`
	PlayedAt    *time.Time      `json:"played_at,omitempty"`
	Readers     []ReaderReceipt `json:"readers"`
}

// GetMessageStatus reports the delivery status of one message: "sent" when
// no receipt has arrived yet, otherwise the furthest of delivered, read and
// played. "unknown" means the message is neither stored nor has receipts.
func (s *Service) GetMessageStatus(ctx context.Context, chatJID, id string) (MessageStatus, error) {
	if id == "" {
		return MessageStatus{}, invalidf("id is required")
	}
	rs, err := s.GetMessageReceipts(ctx, chatJID, []string{id}, 0)
	if err != nil {
		return MessageStatus{}, err
	}
	r := rs[0]
	out := MessageStatus{ID: r.ID, ChatJID: r.ChatJID, Stored: r.Timestamp != nil, FromMe: r.FromMe, Status: r.Status,
		SentAt: r.Timestamp, DeliveredAt: r.DeliveredAt, ReadAt: r.ReadAt, PlayedAt: r.PlayedAt, Readers: r.Readers}
	if out.ChatJID == "" {
		out.ChatJID = chatJID
	}
	if out.Status == "" {
		out.Status = "sent"
		if !out.Stored {
			out.Status = "unknown"
		}
	}
	return out, nil
}
