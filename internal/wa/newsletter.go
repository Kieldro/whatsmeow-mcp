package wa

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// Newsletter is a WhatsApp channel.
type Newsletter struct {
	JID         string    `json:"jid"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Subscribers int       `json:"subscribers"`
	Verified    bool      `json:"verified,omitempty"`
	InviteLink  string    `json:"invite_link,omitempty"`
	Created     time.Time `json:"created,omitempty"`
	Role        string    `json:"role,omitempty"`
	Muted       bool      `json:"muted,omitempty"`
	State       string    `json:"state,omitempty"`
}

func newsletterView(m *types.NewsletterMetadata) Newsletter {
	t := m.ThreadMeta
	n := Newsletter{JID: m.ID.String(), Name: t.Name.Text, Description: t.Description.Text,
		Subscribers: t.SubscriberCount, Verified: t.VerificationState == types.NewsletterVerificationStateVerified,
		Created: t.CreationTime.Time, State: string(m.State.Type)}
	if t.InviteCode != "" {
		n.InviteLink = whatsmeow.NewsletterLinkPrefix + t.InviteCode
	}
	if v := m.ViewerMeta; v != nil {
		n.Role = string(v.Role)
		n.Muted = v.Mute == types.NewsletterMuteOn
	}
	return n
}

func parseNewsletter(jid string) (types.JID, error) {
	j, err := jidutil.Parse(jid)
	if err != nil {
		return j, invalidf("%v", err)
	}
	if j.Server != types.NewsletterServer {
		return j, invalidf("%q is not a channel JID (expected …@newsletter)", jid)
	}
	return j, nil
}

// ListNewsletters lists the channels this account follows.
func (s *Service) ListNewsletters(ctx context.Context) ([]Newsletter, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	list, err := s.Client.GetSubscribedNewsletters(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Newsletter, 0, len(list))
	for _, m := range list {
		if m != nil {
			out = append(out, newsletterView(m))
		}
	}
	return out, nil
}

// NewsletterInfo fetches a channel by JID (…@newsletter) or invite link.
func (s *Service) NewsletterInfo(ctx context.Context, ref string) (Newsletter, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Newsletter{}, invalidf("channel JID or invite link is required")
	}
	if err := s.Ready(); err != nil {
		return Newsletter{}, err
	}
	var m *types.NewsletterMetadata
	var err error
	if strings.HasSuffix(ref, "@"+types.NewsletterServer) {
		var j types.JID
		if j, err = parseNewsletter(ref); err != nil {
			return Newsletter{}, err
		}
		m, err = s.Client.GetNewsletterInfo(ctx, j)
	} else {
		m, err = s.Client.GetNewsletterInfoWithInvite(ctx, strings.TrimPrefix(ref, "http://"))
	}
	if err != nil {
		return Newsletter{}, err
	}
	if m == nil {
		return Newsletter{}, invalidf("channel not found")
	}
	return newsletterView(m), nil
}

// FollowNewsletter follows or unfollows a channel.
func (s *Service) FollowNewsletter(ctx context.Context, jid string, follow bool) error {
	j, err := parseNewsletter(jid)
	if err != nil {
		return err
	}
	if err := s.Ready(); err != nil {
		return err
	}
	if follow {
		return s.Client.FollowNewsletter(ctx, j)
	}
	return s.Client.UnfollowNewsletter(ctx, j)
}

// NewsletterPost is one channel message.
type NewsletterPost struct {
	ServerID  int            `json:"server_id"`
	ID        string         `json:"id,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type,omitempty"`
	Content   string         `json:"content,omitempty"`
	MediaType string         `json:"media_type,omitempty"`
	Views     int            `json:"views,omitempty"`
	Reactions map[string]int `json:"reactions,omitempty"`
}

// NewsletterMessages fetches recent channel posts, newest last. before is a
// server_id to page backwards from (0 = latest).
func (s *Service) NewsletterMessages(ctx context.Context, jid string, count, before int) ([]NewsletterPost, error) {
	j, err := parseNewsletter(jid)
	if err != nil {
		return nil, err
	}
	if count <= 0 {
		count = 20
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	msgs, err := s.Client.GetNewsletterMessages(ctx, j, &whatsmeow.GetNewsletterMessagesParams{Count: min(count, 100), Before: before})
	if err != nil {
		return nil, err
	}
	out := make([]NewsletterPost, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		p := NewsletterPost{ServerID: m.MessageServerID, ID: m.MessageID, Timestamp: m.Timestamp, Type: m.Type,
			Content: extractText(m.Message), Views: m.ViewsCount, Reactions: m.ReactionCounts}
		if mi, ok := extractMedia(m.Message, m.MessageID); ok {
			p.MediaType = mi.Type
		}
		out = append(out, p)
	}
	return out, nil
}
