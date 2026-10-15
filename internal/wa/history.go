package wa

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// HistoryAnchor selects which stored message a backfill request counts back from.
type HistoryAnchor int

const (
	// AnchorOldest asks for messages older than the oldest stored one (default).
	AnchorOldest HistoryAnchor = iota
	// AnchorLatest asks for the most recent messages.
	AnchorLatest
	// AnchorMissingSender re-requests rows stored without a real sender
	// (group messages whose sender equals the chat), so they get overwritten.
	AnchorMissingSender
)

// HistoryRequest is the result of RequestHistory. Messages arrive
// asynchronously and are stored as they come in.
type HistoryRequest struct {
	ChatJID string `json:"chat_jid"`
	Anchor  string `json:"anchor_message_id"`
	Count   int    `json:"count"`
	Note    string `json:"note"`
}

// RequestHistory asks the primary phone to re-send up to count messages of a chat.
func (s *Service) RequestHistory(ctx context.Context, chatJID string, count int, mode HistoryAnchor) (HistoryRequest, error) {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return HistoryRequest{}, err
	}
	if count <= 0 || count > 500 {
		count = 50
	}
	if err := s.Ready(); err != nil {
		return HistoryRequest{}, err
	}
	var anchor store.Message
	switch mode {
	case AnchorOldest:
		anchor, err = s.Store.OldestMessage(chat.String())
	case AnchorMissingSender:
		anchor, err = s.Store.AnchorAfterMissingSender(chat.String(), []string{chat.User, chat.String()})
	default:
		err = store.ErrNotFound
	}
	if errors.Is(err, store.ErrNotFound) {
		// No anchor: a synthetic "now" message makes the phone return the latest batch.
		anchor = store.Message{ID: s.Client.GenerateMessageID(), Timestamp: time.Now()}
	} else if err != nil {
		return HistoryRequest{}, err
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, IsFromMe: anchor.IsFromMe, IsGroup: chat.Server == types.GroupServer},
		ID:            anchor.ID,
		Timestamp:     anchor.Timestamp,
	}
	if _, err := s.Client.SendPeerMessage(ctx, s.Client.BuildHistorySyncRequest(info, count)); err != nil {
		return HistoryRequest{}, err
	}
	return HistoryRequest{ChatJID: chat.String(), Anchor: anchor.ID, Count: count,
		Note: "Requested from the phone; messages arrive asynchronously (the phone must be online). Re-run list_messages in a few seconds."}, nil
}
