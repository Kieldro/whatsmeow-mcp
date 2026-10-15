package wa

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// OnWhatsApp is the registration status of one phone number.
type OnWhatsApp struct {
	Query        string `json:"query"`
	IsOn         bool   `json:"is_on_whatsapp"`
	JID          string `json:"jid,omitempty"`
	Phone        string `json:"phone,omitempty"`
	BusinessName string `json:"business_name,omitempty"`
}

// IsOnWhatsApp checks which phone numbers have WhatsApp accounts.
func (s *Service) IsOnWhatsApp(ctx context.Context, phones []string) ([]OnWhatsApp, error) {
	if len(phones) == 0 {
		return nil, invalidf("no phone numbers given")
	}
	if len(phones) > 50 {
		return nil, invalidf("at most 50 numbers per call")
	}
	query := make([]string, 0, len(phones))
	for _, p := range phones {
		d := jidutil.DigitsOnly(p)
		if len(d) < 5 || len(d) > 15 {
			return nil, invalidf("%q is not a phone number with country code", p)
		}
		query = append(query, "+"+d)
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	res, err := s.Client.IsOnWhatsApp(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]OnWhatsApp, 0, len(res))
	for _, r := range res {
		o := OnWhatsApp{Query: r.Query, IsOn: r.IsIn}
		if r.IsIn {
			o.JID = r.JID.String()
			o.Phone = r.PhoneNumber.User
		}
		if r.VerifiedName != nil && r.VerifiedName.Details != nil {
			o.BusinessName = r.VerifiedName.Details.GetVerifiedName()
		}
		out = append(out, o)
	}
	return out, nil
}

// UserInfo is the public profile of a WhatsApp user.
type UserInfo struct {
	JID          string `json:"jid"`
	Name         string `json:"name,omitempty"`
	About        string `json:"about,omitempty"`
	PictureID    string `json:"picture_id,omitempty"`
	LID          string `json:"lid,omitempty"`
	Devices      int    `json:"devices"`
	BusinessName string `json:"business_name,omitempty"`
}

// GetUserInfo fetches the "about" text, picture ID and business name of users.
func (s *Service) GetUserInfo(ctx context.Context, users []string) ([]UserInfo, error) {
	if len(users) == 0 {
		return nil, invalidf("no users given")
	}
	jids, err := jidutil.ParseList(users)
	if err != nil {
		return nil, invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	res, err := s.Client.GetUserInfo(ctx, jids)
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	out := make([]UserInfo, 0, len(res))
	for _, j := range jids {
		info, ok := res[j]
		if !ok {
			out = append(out, UserInfo{JID: j.String(), Name: r.Resolve(ctx, j.String()).Name})
			continue
		}
		u := UserInfo{JID: j.String(), Name: r.Resolve(ctx, j.String()).Name, About: info.Status,
			PictureID: info.PictureID, Devices: len(info.Devices)}
		if !info.LID.IsEmpty() {
			u.LID = info.LID.String()
		}
		if info.VerifiedName != nil && info.VerifiedName.Details != nil {
			u.BusinessName = info.VerifiedName.Details.GetVerifiedName()
		}
		out = append(out, u)
	}
	return out, nil
}

// ProfilePicture is a downloaded profile or group photo.
type ProfilePicture struct {
	JID   string `json:"jid"`
	ID    string `json:"id"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// GetProfilePicture downloads a user's, group's or community's photo into the
// media directory and returns its path.
func (s *Service) GetProfilePicture(ctx context.Context, target string, preview, community bool) (ProfilePicture, error) {
	j, err := jidutil.Parse(target)
	if err != nil {
		return ProfilePicture{}, invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return ProfilePicture{}, err
	}
	info, err := s.Client.GetProfilePictureInfo(ctx, j, &whatsmeow.GetProfilePictureParams{Preview: preview, IsCommunity: community})
	if err != nil {
		return ProfilePicture{}, err
	}
	if info == nil || info.URL == "" {
		return ProfilePicture{}, whatsmeow.ErrProfilePictureNotSet
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URL, nil)
	if err != nil {
		return ProfilePicture{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ProfilePicture{}, fmt.Errorf("download picture: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProfilePicture{}, fmt.Errorf("download picture: HTTP %d", resp.StatusCode)
	}
	dir := filepath.Join(s.MediaDir, "profile_pictures")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ProfilePicture{}, err
	}
	name := sanitizeFilename(j.User + "_" + j.Server)
	if preview {
		name += "_preview"
	}
	path := filepath.Join(dir, name+".jpg")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return ProfilePicture{}, err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 20<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return ProfilePicture{}, err
	}
	return ProfilePicture{JID: j.String(), ID: info.ID, Path: path, Bytes: n}, nil
}

// SetBlocked blocks or unblocks a user and returns the updated block list.
func (s *Service) SetBlocked(ctx context.Context, user string, block bool) ([]string, error) {
	j, err := jidutil.Parse(user)
	if err != nil {
		return nil, invalidf("%v", err)
	}
	if j.Server != types.DefaultUserServer && j.Server != types.HiddenUserServer {
		return nil, invalidf("%s is not a user JID", j)
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	action := events.BlocklistChangeActionUnblock
	if block {
		action = events.BlocklistChangeActionBlock
	}
	bl, err := s.Client.UpdateBlocklist(ctx, j, action)
	if err != nil {
		return nil, err
	}
	return blockedJIDs(bl), nil
}

// Blocklist returns the blocked users with resolved names.
func (s *Service) Blocklist(ctx context.Context) ([]Person, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	bl, err := s.Client.GetBlocklist(ctx)
	if err != nil {
		return nil, err
	}
	r := s.NewResolver()
	out := []Person{}
	for _, j := range blockedJIDs(bl) {
		out = append(out, r.Resolve(ctx, j))
	}
	return out, nil
}

func blockedJIDs(bl *types.Blocklist) []string {
	out := []string{}
	if bl == nil {
		return out
	}
	for _, j := range bl.JIDs {
		out = append(out, j.String())
	}
	return out
}
