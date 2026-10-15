package wa

import (
	"context"
	"errors"
	"fmt"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// GroupSummary is one joined group.
type GroupSummary struct {
	JID     string `json:"jid"`
	Name    string `json:"name"`
	Members int    `json:"members"`
}

// ListGroups returns the groups this account is in.
func (s *Service) ListGroups(ctx context.Context) ([]GroupSummary, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	groups, err := s.Client.GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GroupSummary, 0, len(groups))
	for _, g := range groups {
		out = append(out, GroupSummary{JID: g.JID.String(), Name: g.Name, Members: len(g.Participants)})
		_ = s.Store.UpsertChat(g.JID.String(), g.Name, g.GroupCreated)
	}
	return out, nil
}

// Participant is a group member.
type Participant struct {
	JID        string `json:"jid"`
	LID        string `json:"lid,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Name       string `json:"name,omitempty"`
	Admin      bool   `json:"admin,omitempty"`
	SuperAdmin bool   `json:"super_admin,omitempty"`
}

// GroupInfo is a group's metadata and members.
type GroupInfo struct {
	JID          string        `json:"jid"`
	Name         string        `json:"name"`
	Topic        string        `json:"topic,omitempty"`
	Owner        string        `json:"owner,omitempty"`
	Count        int           `json:"count"`
	Participants []Participant `json:"participants"`
}

// GroupInfo fetches a group's metadata and members.
func (s *Service) GroupInfo(ctx context.Context, jid string) (GroupInfo, error) {
	g, err := parseGroup(jid)
	if err != nil {
		return GroupInfo{}, err
	}
	if err := s.Ready(); err != nil {
		return GroupInfo{}, err
	}
	info, err := s.Client.GetGroupInfo(ctx, g)
	if err != nil {
		return GroupInfo{}, err
	}
	r := s.NewResolver()
	out := GroupInfo{JID: info.JID.String(), Name: info.Name, Topic: info.Topic, Count: len(info.Participants)}
	if !info.OwnerJID.IsEmpty() {
		out.Owner = info.OwnerJID.String()
	}
	for _, p := range info.Participants {
		pp := Participant{JID: p.JID.String(), Phone: p.PhoneNumber.User, Admin: p.IsAdmin, SuperAdmin: p.IsSuperAdmin}
		if !p.LID.IsEmpty() {
			pp.LID = p.LID.String()
		}
		person := r.Resolve(ctx, p.JID.String())
		if pp.Phone == "" {
			pp.Phone = person.Phone
		}
		pp.Name = person.Name
		if pp.Name == "" {
			pp.Name = p.DisplayName
		}
		out.Participants = append(out.Participants, pp)
	}
	return out, nil
}

func parseGroup(jid string) (types.JID, error) {
	g, err := jidutil.Parse(jid)
	if err != nil {
		return g, err
	}
	if g.Server != types.GroupServer {
		return g, fmt.Errorf("%q is not a group JID (expected …@g.us)", jid)
	}
	return g, nil
}

// ParticipantResult is the per-member outcome of a membership change.
type ParticipantResult struct {
	JID   string `json:"jid"`
	Error int    `json:"error,omitempty"`
}

// UpdateParticipants adds, removes, promotes or demotes group members.
func (s *Service) UpdateParticipants(ctx context.Context, group, action string, members []string) ([]ParticipantResult, error) {
	actions := map[string]whatsmeow.ParticipantChange{
		"add": whatsmeow.ParticipantChangeAdd, "remove": whatsmeow.ParticipantChangeRemove,
		"promote": whatsmeow.ParticipantChangePromote, "demote": whatsmeow.ParticipantChangeDemote,
	}
	act, ok := actions[action]
	if !ok {
		return nil, errors.New("action must be add, remove, promote or demote")
	}
	g, err := parseGroup(group)
	if err != nil {
		return nil, err
	}
	jids, err := jidutil.ParseList(members)
	if err != nil {
		return nil, err
	}
	if len(jids) == 0 {
		return nil, errors.New("no participants given")
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	res, err := s.Client.UpdateGroupParticipants(ctx, g, jids, act)
	out := make([]ParticipantResult, 0, len(res))
	for _, p := range res {
		out = append(out, ParticipantResult{JID: p.JID.String(), Error: p.Error})
	}
	return out, err
}

// CreateGroup creates a group with the given members.
func (s *Service) CreateGroup(ctx context.Context, name string, members []string) (GroupSummary, error) {
	if name == "" {
		return GroupSummary{}, errors.New("group name is required")
	}
	jids, err := jidutil.ParseList(members)
	if err != nil {
		return GroupSummary{}, err
	}
	if err := s.Ready(); err != nil {
		return GroupSummary{}, err
	}
	info, err := s.Client.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: jids})
	if err != nil {
		return GroupSummary{}, err
	}
	_ = s.Store.UpsertChat(info.JID.String(), info.Name, info.GroupCreated)
	return GroupSummary{JID: info.JID.String(), Name: info.Name, Members: len(info.Participants)}, nil
}

// RenameGroup changes a group's subject.
func (s *Service) RenameGroup(ctx context.Context, group, name string) error {
	g, err := parseGroup(group)
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("new name is required")
	}
	if err := s.Ready(); err != nil {
		return err
	}
	if err := s.Client.SetGroupName(ctx, g, name); err != nil {
		return err
	}
	return s.Store.SetChatName(g.String(), name)
}
