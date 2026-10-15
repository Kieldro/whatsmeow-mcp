package wa

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// InviteLink is a group's invite link.
type InviteLink struct {
	GroupJID string `json:"group_jid"`
	Link     string `json:"link"`
	Reset    bool   `json:"reset,omitempty"`
}

// GroupInviteLink returns (and optionally revokes and regenerates) a group's invite link.
func (s *Service) GroupInviteLink(ctx context.Context, group string, reset bool) (InviteLink, error) {
	g, err := parseGroup(group)
	if err != nil {
		return InviteLink{}, invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return InviteLink{}, err
	}
	link, err := s.Client.GetGroupInviteLink(ctx, g, reset)
	if err != nil {
		return InviteLink{}, err
	}
	return InviteLink{GroupJID: g.String(), Link: link, Reset: reset}, nil
}

// inviteCode accepts a full chat.whatsapp.com link or a bare code.
func inviteCode(link string) (string, error) {
	link = strings.TrimSpace(link)
	for _, p := range []string{whatsmeow.InviteLinkPrefix, "http://chat.whatsapp.com/", "chat.whatsapp.com/"} {
		link = strings.TrimPrefix(link, p)
	}
	link, _, _ = strings.Cut(link, "?")
	if link == "" || strings.ContainsAny(link, "/ ") {
		return "", invalidf("expected a https://chat.whatsapp.com/… invite link or code")
	}
	return link, nil
}

// LinkPreview describes a group reachable through an invite link.
type LinkPreview struct {
	JID          string    `json:"jid"`
	Name         string    `json:"name"`
	Topic        string    `json:"topic,omitempty"`
	Owner        string    `json:"owner,omitempty"`
	Created      time.Time `json:"created,omitempty"`
	Participants int       `json:"participants"`
	IsCommunity  bool      `json:"is_community,omitempty"`
	Community    string    `json:"community,omitempty"`
	Announce     bool      `json:"announce,omitempty"`
	Locked       bool      `json:"locked,omitempty"`
	NeedApproval bool      `json:"requires_approval,omitempty"`
}

func linkPreview(info *types.GroupInfo) LinkPreview {
	p := LinkPreview{JID: info.JID.String(), Name: info.Name, Topic: info.Topic, Created: info.GroupCreated,
		Participants: max(info.ParticipantCount, len(info.Participants)), IsCommunity: info.IsParent,
		Announce: info.IsAnnounce, Locked: info.IsLocked, NeedApproval: info.IsJoinApprovalRequired}
	if !info.OwnerJID.IsEmpty() {
		p.Owner = info.OwnerJID.String()
	}
	if !info.LinkedParentJID.IsEmpty() {
		p.Community = info.LinkedParentJID.String()
	}
	return p
}

// GroupInfoFromLink previews a group from its invite link without joining.
func (s *Service) GroupInfoFromLink(ctx context.Context, link string) (LinkPreview, error) {
	code, err := inviteCode(link)
	if err != nil {
		return LinkPreview{}, err
	}
	if err := s.Ready(); err != nil {
		return LinkPreview{}, err
	}
	info, err := s.Client.GetGroupInfoFromLink(ctx, code)
	if err != nil {
		return LinkPreview{}, err
	}
	return linkPreview(info), nil
}

// JoinGroupWithLink joins a group through its invite link.
func (s *Service) JoinGroupWithLink(ctx context.Context, link string) (GroupSummary, error) {
	code, err := inviteCode(link)
	if err != nil {
		return GroupSummary{}, err
	}
	if err := s.Ready(); err != nil {
		return GroupSummary{}, err
	}
	jid, err := s.Client.JoinGroupWithLink(ctx, code)
	if err != nil {
		return GroupSummary{}, err
	}
	out := GroupSummary{JID: jid.String()}
	// Groups needing admin approval return before membership is granted.
	if info, err := s.Client.GetGroupInfo(ctx, jid); err == nil {
		out.Name, out.Members = info.Name, len(info.Participants)
		_ = s.Store.UpsertChat(jid.String(), info.Name, time.Now())
	}
	return out, nil
}

// LeaveGroup leaves a group.
func (s *Service) LeaveGroup(ctx context.Context, group string) error {
	g, err := parseGroup(group)
	if err != nil {
		return invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.LeaveGroup(ctx, g)
}

// SetGroupDescription sets (or with an empty string clears) a group's description.
func (s *Service) SetGroupDescription(ctx context.Context, group, description string) error {
	g, err := parseGroup(group)
	if err != nil {
		return invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	// With empty IDs whatsmeow looks up the previous topic ID and generates a new one.
	return s.Client.SetGroupTopic(ctx, g, "", "", description)
}

// readJPEG reads a JPEG file for use as a group or profile photo.
func readJPEG(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, invalidf("read image: %v", err)
	}
	if !IsJPEG(data) {
		return nil, invalidf("%s is not a JPEG; WhatsApp only accepts JPEG photos (convert it first, ideally square, 640x640)", path)
	}
	return data, nil
}

// SetGroupPhoto sets a group's photo from a JPEG file, or removes it when path is empty.
func (s *Service) SetGroupPhoto(ctx context.Context, group, path string) (string, error) {
	g, err := parseGroup(group)
	if err != nil {
		return "", invalidf("%v", err)
	}
	var data []byte
	if path != "" {
		if data, err = readJPEG(path); err != nil {
			return "", err
		}
	}
	if err := s.Ready(); err != nil {
		return "", err
	}
	return s.Client.SetGroupPhoto(ctx, g, data)
}

// SetGroupAnnounce toggles admins-only messaging.
func (s *Service) SetGroupAnnounce(ctx context.Context, group string, on bool) error {
	g, err := parseGroup(group)
	if err != nil {
		return invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.SetGroupAnnounce(ctx, g, on)
}

// SetGroupLocked toggles admins-only editing of group info.
func (s *Service) SetGroupLocked(ctx context.Context, group string, on bool) error {
	g, err := parseGroup(group)
	if err != nil {
		return invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.SetGroupLocked(ctx, g, on)
}

// JoinRequest is a pending request to join a group.
type JoinRequest struct {
	JID         string    `json:"jid"`
	Phone       string    `json:"phone,omitempty"`
	Name        string    `json:"name,omitempty"`
	RequestedAt time.Time `json:"requested_at"`
}

// ListJoinRequests lists pending join requests (requires admin).
func (s *Service) ListJoinRequests(ctx context.Context, group string) ([]JoinRequest, error) {
	g, err := parseGroup(group)
	if err != nil {
		return nil, invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	reqs, err := s.Client.GetGroupRequestParticipants(ctx, g)
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	out := make([]JoinRequest, 0, len(reqs))
	for _, q := range reqs {
		p := r.Resolve(ctx, q.JID.String())
		out = append(out, JoinRequest{JID: q.JID.String(), Phone: p.Phone, Name: p.Name, RequestedAt: q.RequestedAt})
	}
	return out, nil
}

// UpdateJoinRequests approves or rejects pending join requests (requires admin).
func (s *Service) UpdateJoinRequests(ctx context.Context, group, action string, members []string) ([]ParticipantResult, error) {
	var act whatsmeow.ParticipantRequestChange
	switch action {
	case "approve":
		act = whatsmeow.ParticipantChangeApprove
	case "reject":
		act = whatsmeow.ParticipantChangeReject
	default:
		return nil, invalidf("action must be approve or reject")
	}
	g, err := parseGroup(group)
	if err != nil {
		return nil, invalidf("%v", err)
	}
	jids, err := jidutil.ParseList(members)
	if err != nil {
		return nil, invalidf("%v", err)
	}
	if len(jids) == 0 {
		return nil, invalidf("no participants given")
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	res, err := s.Client.UpdateGroupRequestParticipants(ctx, g, jids, act)
	out := make([]ParticipantResult, 0, len(res))
	for _, p := range res {
		out = append(out, ParticipantResult{JID: p.JID.String(), Error: p.Error})
	}
	return out, err
}

// Community is a community (parent group) with its linked groups.
type Community struct {
	JID    string         `json:"jid"`
	Name   string         `json:"name"`
	Groups []GroupSummary `json:"joined_groups"`
}

// ListCommunities groups the joined groups by their community.
func (s *Service) ListCommunities(ctx context.Context) ([]Community, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	groups, err := s.Client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}
	byJID := map[string]*Community{}
	var order []string
	get := func(jid string) *Community {
		c, ok := byJID[jid]
		if !ok {
			c = &Community{JID: jid, Groups: []GroupSummary{}}
			byJID[jid] = c
			order = append(order, jid)
		}
		return c
	}
	for _, g := range groups {
		switch {
		case g.IsParent:
			get(g.JID.String()).Name = g.Name
		case !g.LinkedParentJID.IsEmpty():
			c := get(g.LinkedParentJID.String())
			c.Groups = append(c.Groups, GroupSummary{JID: g.JID.String(), Name: g.Name, Members: len(g.Participants)})
		}
	}
	out := make([]Community, 0, len(order))
	for _, j := range order {
		out = append(out, *byJID[j])
	}
	return out, nil
}

// SubGroup is a group linked to a community.
type SubGroup struct {
	JID          string `json:"jid"`
	Name         string `json:"name"`
	Announcement bool   `json:"is_announcement_group,omitempty"`
}

// CommunityGroups lists every group linked to a community, joined or not.
func (s *Service) CommunityGroups(ctx context.Context, community string) ([]SubGroup, error) {
	c, err := parseGroup(community)
	if err != nil {
		return nil, invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	subs, err := s.Client.GetSubGroups(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("get community groups (is %s a community?): %w", c, err)
	}
	out := make([]SubGroup, 0, len(subs))
	for _, g := range subs {
		out = append(out, SubGroup{JID: g.JID.String(), Name: g.Name, Announcement: g.IsDefaultSubGroup})
	}
	return out, nil
}
