package wa

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// Person is a resolved WhatsApp identity.
type Person struct {
	JID   string `json:"jid"`
	Phone string `json:"phone,omitempty"`
	LID   string `json:"lid,omitempty"`
	Name  string `json:"name,omitempty"`
}

// Resolver maps JIDs to phone numbers and display names, caching results for
// its lifetime. Create one per request.
type Resolver struct {
	s     *Service
	cache map[string]Person
}

// NewResolver returns a request-scoped resolver.
func (s *Service) NewResolver() *Resolver {
	return &Resolver{s: s, cache: map[string]Person{}}
}

func contactName(ci types.ContactInfo) string {
	for _, n := range []string{ci.FullName, ci.FirstName, ci.PushName, ci.BusinessName} {
		if n != "" {
			return n
		}
	}
	return ""
}

// pnForLID returns the phone-number JID of a LID, if known.
func (s *Service) pnForLID(ctx context.Context, lid types.JID) types.JID {
	if s.container == nil || s.container.LIDMap == nil {
		return types.EmptyJID
	}
	pn, err := s.container.LIDMap.GetPNForLID(ctx, lid)
	if err != nil {
		return types.EmptyJID
	}
	return pn
}

// lidForPN returns the LID of a phone-number JID, if known.
func (s *Service) lidForPN(ctx context.Context, pn types.JID) types.JID {
	if s.container == nil || s.container.LIDMap == nil {
		return types.EmptyJID
	}
	lid, err := s.container.LIDMap.GetLIDForPN(ctx, pn)
	if err != nil {
		return types.EmptyJID
	}
	return lid
}

// addressBookName looks a user up in the synced address book.
func (s *Service) addressBookName(ctx context.Context, j types.JID) string {
	cs := s.Client.Store.Contacts
	if cs == nil || j.IsEmpty() {
		return ""
	}
	ci, err := cs.GetContact(ctx, j)
	if err != nil || !ci.Found {
		return ""
	}
	return contactName(ci)
}

// Resolve returns phone, LID and best-known name for a JID (string or stored sender value).
func (r *Resolver) Resolve(ctx context.Context, raw string) Person {
	if p, ok := r.cache[raw]; ok {
		return p
	}
	j, ok := jidutil.FromStoredSender(raw)
	if !ok {
		return Person{JID: raw}
	}
	p := Person{JID: j.String()}
	var pn, lid types.JID
	switch j.Server {
	case types.HiddenUserServer:
		lid = j
		pn = r.s.pnForLID(ctx, j)
	case types.DefaultUserServer:
		pn = j
		lid = r.s.lidForPN(ctx, j)
	}
	if !pn.IsEmpty() {
		p.Phone = pn.User
	}
	if !lid.IsEmpty() {
		p.LID = lid.String()
	}
	if own := r.s.Client.Store.ID; own != nil && (pn.User == own.User || (r.s.Client.Store.LID.User != "" && lid.User == r.s.Client.Store.LID.User)) {
		p.Name = "Me"
	}
	if p.Name == "" {
		p.Name = r.s.addressBookName(ctx, pn)
	}
	if p.Name == "" {
		p.Name = r.s.addressBookName(ctx, lid)
	}
	if p.Name == "" && j.Server == types.GroupServer {
		p.Name = r.s.Store.ChatName(j.String())
	}
	if p.Name == "" {
		// A DM chat with this person may carry a name from history sync.
		for _, cand := range []types.JID{pn, lid} {
			if cand.IsEmpty() {
				continue
			}
			if n := r.s.Store.ChatName(cand.String()); n != "" && n != cand.User {
				p.Name = n
				break
			}
		}
	}
	r.cache[raw] = p
	return p
}

// chatNameFor picks a display name for a chat being stored for the first time.
func (s *Service) chatNameFor(ctx context.Context, chat types.JID) string {
	if n := s.Store.ChatName(chat.String()); n != "" {
		return n
	}
	switch chat.Server {
	case types.GroupServer:
		if s.Client.IsConnected() {
			if gi, err := s.Client.GetGroupInfo(ctx, chat); err == nil && gi.Name != "" {
				return gi.Name
			}
		}
		return ""
	case types.BroadcastServer:
		if chat.User == "status" {
			return "Status"
		}
		return ""
	default:
		return s.NewResolver().Resolve(ctx, chat.String()).Name
	}
}

// FixChatNames fills in DM chats stored without a real name from the address book.
func (s *Service) FixChatNames(ctx context.Context) {
	jids, err := s.Store.UnnamedDirectChats()
	if err != nil {
		return
	}
	r := s.NewResolver()
	for _, j := range jids {
		if p := r.Resolve(ctx, j); p.Name != "" && p.Name != "Me" {
			_ = s.Store.SetChatName(j, p.Name)
		}
	}
}

// ChatCandidates expands user input (phone number, JID or chat name) into
// the chat JIDs that may hold that conversation: a contact can have both a
// phone-number chat and a LID chat.
func (s *Service) ChatCandidates(ctx context.Context, input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	j, err := jidutil.Parse(input)
	if err != nil {
		// Treat as a chat name.
		return nil
	}
	out := []string{j.String()}
	switch j.Server {
	case types.DefaultUserServer:
		if lid := s.lidForPN(ctx, j); !lid.IsEmpty() {
			out = append(out, lid.String())
		}
	case types.HiddenUserServer:
		if pn := s.pnForLID(ctx, j); !pn.IsEmpty() {
			out = append(out, pn.String())
		}
	}
	return out
}

// SenderCandidates expands a sender (phone or JID) into every stored form:
// full phone JID, LID JID, and the bare user parts older rows used.
func (s *Service) SenderCandidates(ctx context.Context, input string) []string {
	var out []string
	for _, c := range s.ChatCandidates(ctx, input) {
		out = append(out, c, jidutil.UserPart(c))
	}
	return out
}

// SendTarget picks the JID to send to for a recipient: phone numbers stay
// phone JIDs (whatsmeow handles LID addressing), JIDs are used as-is.
func SendTarget(recipient string) (types.JID, error) {
	return jidutil.Parse(recipient)
}
