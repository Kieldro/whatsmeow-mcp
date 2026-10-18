package wa

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// VotePoll casts (or with no options, retracts) this account's vote in a
// tracked poll. Options are matched exactly against the poll's options.
func (s *Service) VotePoll(ctx context.Context, pollID string, options []string) (Sent, error) {
	p, err := s.Store.GetPoll(pollID)
	if err != nil {
		return Sent{}, invalidf("unknown poll %q (only polls received or sent while this client was running are tracked)", pollID)
	}
	chosen, err := MatchPollOptions(p.Options, options)
	if err != nil {
		return Sent{}, err
	}
	chat, err := jidutil.Parse(p.ChatJID)
	if err != nil {
		return Sent{}, invalidf("poll %q has no usable chat: %v", pollID, err)
	}
	if err := s.Ready(); err != nil {
		return Sent{}, err
	}
	sender, m := s.targetSender(chat, pollID)
	info := &types.MessageInfo{ID: pollID, MessageSource: types.MessageSource{
		Chat: chat, Sender: sender, IsFromMe: m != nil && m.IsFromMe, IsGroup: chat.Server == types.GroupServer,
	}}
	msg, err := s.Client.BuildPollVote(ctx, info, chosen)
	if err != nil {
		return Sent{}, err
	}
	s.resolveRecipient(ctx, chat)
	resp, err := s.Client.SendMessage(ctx, chat, msg)
	if err != nil {
		return Sent{}, err
	}
	_ = s.Store.SaveVote(pollID, chat.String(), s.ownJID().String(), chosen, resp.Timestamp)
	return Sent{ID: resp.ID, ChatJID: chat.String(), Timestamp: resp.Timestamp}, nil
}

// MatchPollOptions checks that every selected option exists in the poll and
// returns them in poll order without duplicates.
func MatchPollOptions(pollOptions, selected []string) ([]string, error) {
	want := map[string]bool{}
	for _, o := range selected {
		want[o] = true
	}
	out := []string{}
	for _, o := range pollOptions {
		if want[o] {
			out = append(out, o)
			delete(want, o)
		}
	}
	if len(want) > 0 {
		var bad []string
		for o := range want {
			bad = append(bad, o)
		}
		return nil, invalidf("not options of this poll: %q (options: %q)", bad, pollOptions)
	}
	return out, nil
}

// MentionText makes sure text contains "@<user>" for each mentioned JID,
// which is how WhatsApp clients render mentions. aliases maps an alternate
// user part (a phone number when the mention uses a LID) to the JID's user,
// so "@15550100" in the text is rewritten to the LID form. Missing mentions
// are appended.
func MentionText(text string, mentioned []types.JID, aliases map[string]string) string {
	for _, j := range mentioned {
		tag := "@" + j.User
		for alt, user := range aliases {
			if user == j.User && strings.Contains(text, "@"+alt) {
				text = strings.ReplaceAll(text, "@"+alt, tag)
			}
		}
		if !strings.Contains(text, tag) {
			text = strings.TrimRight(text, " ") + " " + tag
		}
	}
	return text
}

// mentions resolves mentioned users for chat. In LID-addressed groups
// phone numbers are converted to LIDs when the mapping is known.
func (s *Service) mentions(ctx context.Context, chat types.JID, text string, users []string) (string, []string, error) {
	jids, err := jidutil.ParseList(users)
	if err != nil {
		return "", nil, invalidf("mentions: %v", err)
	}
	useLID := false
	if chat.Server == types.GroupServer {
		if gi, err := s.Client.GetGroupInfo(ctx, chat); err == nil && gi.AddressingMode == types.AddressingModeLID {
			useLID = true
		}
	}
	aliases := map[string]string{}
	out := make([]string, 0, len(jids))
	for i, j := range jids {
		if useLID && j.Server == types.DefaultUserServer {
			if lid := s.lidForPN(ctx, j); !lid.IsEmpty() {
				aliases[j.User] = lid.User
				jids[i] = lid
			}
		}
		out = append(out, jids[i].String())
	}
	return MentionText(text, jids, aliases), out, nil
}

// BusinessProfile is a WhatsApp Business account's public profile.
type BusinessProfile struct {
	JID           string                      `json:"jid"`
	Address       string                      `json:"address,omitempty"`
	Email         string                      `json:"email,omitempty"`
	Categories    []string                    `json:"categories,omitempty"`
	Options       map[string]string           `json:"profile_options,omitempty"`
	HoursTimeZone string                      `json:"hours_time_zone,omitempty"`
	Hours         []types.BusinessHoursConfig `json:"hours,omitempty"`
}

// GetBusinessProfile fetches a business account's profile (address, email,
// categories, hours).
func (s *Service) GetBusinessProfile(ctx context.Context, user string) (BusinessProfile, error) {
	j, err := jidutil.Parse(user)
	if err != nil {
		return BusinessProfile{}, invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return BusinessProfile{}, err
	}
	bp, err := s.Client.GetBusinessProfile(ctx, j)
	if err != nil {
		return BusinessProfile{}, err
	}
	if bp == nil {
		return BusinessProfile{}, invalidf("%s has no business profile", j)
	}
	out := BusinessProfile{JID: bp.JID.String(), Address: bp.Address, Email: bp.Email, Options: bp.ProfileOptions,
		HoursTimeZone: bp.BusinessHoursTimeZone, Hours: bp.BusinessHours}
	if out.JID == "" || bp.JID.IsEmpty() {
		out.JID = j.String()
	}
	for _, c := range bp.Categories {
		out.Categories = append(out.Categories, c.Name)
	}
	return out, nil
}

// SetGroupJoinApproval turns admin approval for new members on or off.
func (s *Service) SetGroupJoinApproval(ctx context.Context, group string, on bool) error {
	g, err := parseGroup(group)
	if err != nil {
		return invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.SetGroupJoinApprovalMode(ctx, g, on)
}

// SetGroupMemberAddMode sets whether only admins or all members can add people.
func (s *Service) SetGroupMemberAddMode(ctx context.Context, group string, adminsOnly bool) error {
	g, err := parseGroup(group)
	if err != nil {
		return invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	mode := types.GroupMemberAddModeAllMember
	if adminsOnly {
		mode = types.GroupMemberAddModeAdmin
	}
	return s.Client.SetGroupMemberAddMode(ctx, g, mode)
}

// CreateCommunity creates a community (WhatsApp also creates its
// announcement group) with an optional description.
func (s *Service) CreateCommunity(ctx context.Context, name, description string) (GroupSummary, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return GroupSummary{}, invalidf("community name is required")
	}
	if err := s.Ready(); err != nil {
		return GroupSummary{}, err
	}
	info, err := s.Client.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, GroupParent: types.GroupParent{IsParent: true}})
	if err != nil {
		return GroupSummary{}, err
	}
	if description != "" {
		if err := s.Client.SetGroupTopic(ctx, info.JID, "", "", description); err != nil {
			s.log.Warnf("set community description: %v", err)
		}
	}
	return GroupSummary{JID: info.JID.String(), Name: info.Name, Members: len(info.Participants)}, nil
}

// LinkGroup adds an existing group to a community, or removes it (link=false).
func (s *Service) LinkGroup(ctx context.Context, community, group string, link bool) error {
	parent, err := parseGroup(community)
	if err != nil {
		return invalidf("community: %v", err)
	}
	child, err := parseGroup(group)
	if err != nil {
		return invalidf("group: %v", err)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	if link {
		return s.Client.LinkGroup(ctx, parent, child)
	}
	return s.Client.UnlinkGroup(ctx, parent, child)
}

// SetDefaultDisappearingTimer sets the timer applied to new chats.
func (s *Service) SetDefaultDisappearingTimer(ctx context.Context, timer string) (time.Duration, error) {
	d, err := ParseDisappearingTimer(timer)
	if err != nil {
		return 0, err
	}
	if err := s.Ready(); err != nil {
		return 0, err
	}
	return d, s.Client.SetDefaultDisappearingTimer(ctx, d)
}

// ContactQRLink returns this account's contact QR link (wa.me/qr/…); reset
// revokes the old one.
func (s *Service) ContactQRLink(ctx context.Context, reset bool) (string, error) {
	if err := s.Ready(); err != nil {
		return "", err
	}
	code, err := s.Client.GetContactQRLink(ctx, reset)
	if err != nil {
		return "", err
	}
	return whatsmeow.ContactQRLinkPrefix + code, nil
}

// ResolvedLink is the account behind a wa.me QR or business message link.
type ResolvedLink struct {
	JID          string `json:"jid"`
	Phone        string `json:"phone,omitempty"`
	Name         string `json:"name,omitempty"`
	PushName     string `json:"push_name,omitempty"`
	VerifiedName string `json:"verified_name,omitempty"`
	Message      string `json:"prefilled_message,omitempty"`
}

// linkCode extracts the code from a wa.me link (or returns a bare code).
func linkCode(link string, prefixes ...string) (string, error) {
	link = strings.TrimSpace(link)
	for _, p := range prefixes {
		link = strings.TrimPrefix(link, p)
		link = strings.TrimPrefix(link, strings.TrimPrefix(p, "https://"))
	}
	link, _, _ = strings.Cut(link, "?")
	if link == "" || strings.ContainsAny(link, "/ ") {
		return "", invalidf("expected a %s… link or its code", prefixes[0])
	}
	return link, nil
}

// ResolveLink looks up who a contact QR link (wa.me/qr/…) or business
// message link (wa.me/message/…) belongs to.
func (s *Service) ResolveLink(ctx context.Context, link string) (ResolvedLink, error) {
	business := strings.Contains(link, "/message/")
	var code string
	var err error
	if business {
		code, err = linkCode(link, whatsmeow.BusinessMessageLinkPrefix, whatsmeow.BusinessMessageLinkDirectPrefix)
	} else {
		code, err = linkCode(link, whatsmeow.ContactQRLinkPrefix, whatsmeow.ContactQRLinkDirectPrefix)
	}
	if err != nil {
		return ResolvedLink{}, err
	}
	if err := s.Ready(); err != nil {
		return ResolvedLink{}, err
	}
	var out ResolvedLink
	if business {
		t, err := s.Client.ResolveBusinessMessageLink(ctx, code)
		if err != nil {
			return ResolvedLink{}, err
		}
		out = ResolvedLink{JID: t.JID.String(), PushName: t.PushName, VerifiedName: t.VerifiedName, Message: t.Message}
	} else {
		t, err := s.Client.ResolveContactQRLink(ctx, code)
		if err != nil {
			return ResolvedLink{}, err
		}
		out = ResolvedLink{JID: t.JID.String(), PushName: t.PushName}
	}
	p := s.NewResolver().Resolve(ctx, out.JID)
	out.Phone, out.Name = p.Phone, p.Name
	return out, nil
}

// StatusAudience is who receives this account's Status posts.
type StatusAudience struct {
	Type      string   `json:"type"`
	List      []string `json:"list,omitempty"`
	IsDefault bool     `json:"is_default,omitempty"`
}

// GetStatusPrivacy returns the Status audience settings (contacts, all
// contacts except a list, or only a list).
func (s *Service) GetStatusPrivacy(ctx context.Context) ([]StatusAudience, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	sp, err := s.Client.GetStatusPrivacy(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]StatusAudience, 0, len(sp))
	for _, p := range sp {
		a := StatusAudience{Type: string(p.Type), IsDefault: p.IsDefault}
		for _, j := range p.List {
			a.List = append(a.List, j.String())
		}
		out = append(out, a)
	}
	return out, nil
}

// CreateNewsletter creates a channel owned by this account.
func (s *Service) CreateNewsletter(ctx context.Context, name, description, picture string) (Newsletter, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Newsletter{}, invalidf("channel name is required")
	}
	params := whatsmeow.CreateNewsletterParams{Name: name, Description: description}
	if picture != "" {
		data, err := readJPEG(picture)
		if err != nil {
			return Newsletter{}, err
		}
		params.Picture = data
	}
	if err := s.Ready(); err != nil {
		return Newsletter{}, err
	}
	m, err := s.Client.CreateNewsletter(ctx, params)
	if err != nil {
		return Newsletter{}, err
	}
	return newsletterView(m), nil
}

// MuteNewsletter mutes or unmutes a followed channel's notifications.
func (s *Service) MuteNewsletter(ctx context.Context, jid string, mute bool) error {
	j, err := parseNewsletter(jid)
	if err != nil {
		return err
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.NewsletterToggleMute(ctx, j, mute)
}

// ReactNewsletter reacts to a channel post (by server_id from
// newsletter_messages); an empty emoji removes the reaction.
func (s *Service) ReactNewsletter(ctx context.Context, jid string, serverID int, emoji string) error {
	j, err := parseNewsletter(jid)
	if err != nil {
		return err
	}
	if serverID <= 0 {
		return invalidf("server_id must be a positive post id from newsletter_messages")
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.NewsletterSendReaction(ctx, j, types.MessageServerID(serverID), emoji, "")
}
