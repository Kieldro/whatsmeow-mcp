package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// LastSeen is the most recent presence update received for a user.
type LastSeen struct {
	JID       string     `json:"jid"`
	Online    bool       `json:"online"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
	Known     bool       `json:"known"`
	Note      string     `json:"note,omitempty"`
}

func (s *Service) recordPresence(evt *events.Presence) {
	ls := LastSeen{JID: evt.From.ToNonAD().String(), Online: !evt.Unavailable, UpdatedAt: time.Now(), Known: true}
	if !evt.LastSeen.IsZero() {
		t := evt.LastSeen
		ls.LastSeen = &t
	}
	s.presenceMu.Lock()
	s.presence[ls.JID] = ls
	s.presenceMu.Unlock()
}

// SendPresence sets this account's global online/offline presence.
func (s *Service) SendPresence(ctx context.Context, state string) error {
	p, err := ParsePresence(state)
	if err != nil {
		return err
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.SendPresence(ctx, p)
}

// SendChatPresence shows typing/recording (or stops it) in a chat.
func (s *Service) SendChatPresence(ctx context.Context, chatJID, state string) error {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return invalidf("%v", err)
	}
	cp, media, err := ParseChatPresence(state)
	if err != nil {
		return err
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.SendChatPresence(ctx, chat, cp, media)
}

// SubscribePresence asks WhatsApp to push a user's online/last-seen updates.
func (s *Service) SubscribePresence(ctx context.Context, user string) error {
	j, err := jidutil.Parse(user)
	if err != nil {
		return invalidf("%v", err)
	}
	if j.Server != types.DefaultUserServer && j.Server != types.HiddenUserServer {
		return invalidf("%s is not a user JID", j)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.SubscribePresence(ctx, j)
}

// GetLastSeen returns the latest presence update seen for a user (after subscribe_presence).
func (s *Service) GetLastSeen(ctx context.Context, user string) (LastSeen, error) {
	j, err := jidutil.Parse(user)
	if err != nil {
		return LastSeen{}, invalidf("%v", err)
	}
	keys := []string{j.String()}
	switch j.Server {
	case types.DefaultUserServer:
		if lid := s.lidForPN(ctx, j); !lid.IsEmpty() {
			keys = append(keys, lid.String())
		}
	case types.HiddenUserServer:
		if pn := s.pnForLID(ctx, j); !pn.IsEmpty() {
			keys = append(keys, pn.String())
		}
	}
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	var best LastSeen
	for _, k := range keys {
		if ls, ok := s.presence[k]; ok && ls.UpdatedAt.After(best.UpdatedAt) {
			best = ls
		}
	}
	if !best.Known {
		return LastSeen{JID: j.String(), Note: "no presence update received yet; call subscribe_presence first (and send_presence available), then retry. Users who hide last seen never report it"}, nil
	}
	return best, nil
}
