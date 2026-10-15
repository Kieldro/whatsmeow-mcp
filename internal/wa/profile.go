package wa

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// SetStatusMessage sets this account's "About" text.
func (s *Service) SetStatusMessage(ctx context.Context, about string) error {
	about = strings.TrimSpace(about)
	if about == "" {
		return invalidf("about text is empty")
	}
	if len([]rune(about)) > 139 {
		return invalidf("about text is limited to 139 characters")
	}
	if err := s.Ready(); err != nil {
		return err
	}
	return s.Client.SetStatusMessage(ctx, types.SetStatusInput{Text: &about})
}

// SetProfilePicture sets this account's profile photo from a JPEG file, or
// removes it when path is empty.
func (s *Service) SetProfilePicture(ctx context.Context, path string) (string, error) {
	var data []byte
	if path != "" {
		var err error
		if data, err = readJPEG(path); err != nil {
			return "", err
		}
	}
	if err := s.Ready(); err != nil {
		return "", err
	}
	// whatsmeow's picture setter targets any JID; our own JID sets the profile photo.
	return s.Client.SetGroupPhoto(ctx, s.Client.Store.ID.ToNonAD(), data)
}

// Privacy is this account's privacy settings, keyed by friendly name.
type Privacy map[string]string

func privacyView(p *types.PrivacySettings) Privacy {
	return Privacy{
		"group_add": string(p.GroupAdd), "last_seen": string(p.LastSeen), "status": string(p.Status),
		"profile": string(p.Profile), "read_receipts": string(p.ReadReceipts), "online": string(p.Online),
		"call_add": string(p.CallAdd), "messages": string(p.Messages), "defense": string(p.Defense),
		"stickers": string(p.Stickers),
	}
}

// GetPrivacySettings fetches the current privacy settings from WhatsApp.
func (s *Service) GetPrivacySettings(ctx context.Context) (Privacy, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	p, err := s.Client.TryFetchPrivacySettings(ctx, true)
	if err != nil {
		return nil, err
	}
	return privacyView(p), nil
}

// SetPrivacySetting changes one privacy setting and returns all settings.
func (s *Service) SetPrivacySetting(ctx context.Context, name, value string) (Privacy, error) {
	typ, val, err := ParsePrivacySetting(name, value)
	if err != nil {
		return nil, err
	}
	if err := s.Ready(); err != nil {
		return nil, err
	}
	p, err := s.Client.SetPrivacySetting(ctx, typ, val)
	if err != nil {
		return nil, err
	}
	return privacyView(&p), nil
}
