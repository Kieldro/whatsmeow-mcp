package wa

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// lastMessage returns the timestamp and key of the newest stored message in
// a chat, which archive/mark-read patches use as their message range.
func (s *Service) lastMessage(chat types.JID) (time.Time, *waCommon.MessageKey) {
	msgs, err := s.Store.ListMessages(store.MessageFilter{ChatJIDs: []string{chat.String()}, Limit: 1, IncludeStatus: true})
	if err != nil || len(msgs) == 0 {
		return time.Time{}, nil
	}
	m := msgs[0]
	sender := s.Client.Store.ID.ToNonAD()
	if !m.IsFromMe {
		sender, _ = s.targetSender(chat, m.ID)
		if sender.IsEmpty() {
			sender = chat
		}
	}
	return m.Timestamp, s.Client.BuildMessageKey(chat, sender, m.ID)
}

// chatForAppState parses and checks a chat JID for app-state changes.
func (s *Service) chatForAppState(chatJID string) (types.JID, error) {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return chat, invalidf("%v", err)
	}
	if err := s.Ready(); err != nil {
		return chat, err
	}
	return chat, nil
}

// ArchiveChat archives or unarchives a chat on all of this account's devices.
func (s *Service) ArchiveChat(ctx context.Context, chatJID string, archive bool) error {
	chat, err := s.chatForAppState(chatJID)
	if err != nil {
		return err
	}
	ts, key := s.lastMessage(chat)
	return s.Client.SendAppState(ctx, appstate.BuildArchive(chat, archive, ts, key))
}

// PinChat pins or unpins a chat at the top of the chat list.
func (s *Service) PinChat(ctx context.Context, chatJID string, pin bool) error {
	chat, err := s.chatForAppState(chatJID)
	if err != nil {
		return err
	}
	return s.Client.SendAppState(ctx, appstate.BuildPin(chat, pin))
}

// MuteChat mutes a chat for duration (0 = forever) or unmutes it.
func (s *Service) MuteChat(ctx context.Context, chatJID, duration string, unmute bool) (string, error) {
	var d time.Duration
	if !unmute {
		var err error
		if d, err = ParseMuteDuration(duration); err != nil {
			return "", err
		}
	}
	chat, err := s.chatForAppState(chatJID)
	if err != nil {
		return "", err
	}
	if err := s.Client.SendAppState(ctx, appstate.BuildMute(chat, !unmute, d)); err != nil {
		return "", err
	}
	switch {
	case unmute:
		return "unmuted", nil
	case d == 0:
		return "muted forever", nil
	}
	return "muted until " + time.Now().Add(d).Format(time.RFC3339), nil
}

// MarkChatRead marks a whole chat as read or unread (the unread badge).
func (s *Service) MarkChatRead(ctx context.Context, chatJID string, read bool) error {
	chat, err := s.chatForAppState(chatJID)
	if err != nil {
		return err
	}
	ts, key := s.lastMessage(chat)
	return s.Client.SendAppState(ctx, appstate.BuildMarkChatAsRead(chat, read, ts, key))
}

// SetDisappearingTimer sets a chat's disappearing-messages timer (off, 24h, 7d or 90d).
func (s *Service) SetDisappearingTimer(ctx context.Context, chatJID, timer string) (time.Duration, error) {
	d, err := ParseDisappearingTimer(timer)
	if err != nil {
		return 0, err
	}
	chat, err := s.chatForAppState(chatJID)
	if err != nil {
		return 0, err
	}
	return d, s.Client.SetDisappearingTimer(ctx, chat, d, time.Now())
}
