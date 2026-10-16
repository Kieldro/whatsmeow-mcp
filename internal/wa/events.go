package wa

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// extractText returns the human-readable text of a message, including media
// captions and a one-line summary for polls, locations and contacts.
func extractText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	switch {
	case m.GetConversation() != "":
		return m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetCaption()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetCaption()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetCaption()
	case m.GetLocationMessage() != nil:
		l := m.GetLocationMessage()
		return fmt.Sprintf("[location] %s %.6f,%.6f", l.GetName(), l.GetDegreesLatitude(), l.GetDegreesLongitude())
	case m.GetContactMessage() != nil:
		return "[contact] " + m.GetContactMessage().GetDisplayName()
	case m.GetContactsArrayMessage() != nil:
		var names []string
		for _, c := range m.GetContactsArrayMessage().GetContacts() {
			names = append(names, c.GetDisplayName())
		}
		return "[contacts] " + strings.Join(names, ", ")
	case m.GetLiveLocationMessage() != nil:
		l := m.GetLiveLocationMessage()
		return strings.TrimSpace(fmt.Sprintf("[live location] %.6f,%.6f %s", l.GetDegreesLatitude(), l.GetDegreesLongitude(), l.GetCaption()))
	case m.GetGroupInviteMessage() != nil:
		g := m.GetGroupInviteMessage()
		return strings.TrimSpace(fmt.Sprintf("[group invite] %s %s", g.GetGroupName(), g.GetCaption()))
	case m.GetEventMessage() != nil:
		e := m.GetEventMessage()
		out := "[event] " + e.GetName()
		if st := e.GetStartTime(); st > 0 {
			out += " at " + time.Unix(st, 0).UTC().Format(time.RFC3339)
		}
		if loc := e.GetLocation().GetName(); loc != "" {
			out += " @ " + loc
		}
		if d := e.GetDescription(); d != "" {
			out += ": " + d
		}
		return out
	case m.GetPtvMessage() != nil:
		return m.GetPtvMessage().GetCaption()
	}
	if q, opts, ok := pollCreation(m); ok {
		return fmt.Sprintf("[poll] %s (%s)", q, strings.Join(opts, " / "))
	}
	return ""
}

// contextInfo returns the reply/mention context of a message, whichever
// message type it is (every content type has a contextInfo field).
func contextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	if m == nil {
		return nil
	}
	var out *waE2E.ContextInfo
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		sub := v.Message()
		f := sub.Descriptor().Fields().ByName("contextInfo")
		if f == nil || f.Kind() != protoreflect.MessageKind || !sub.Has(f) {
			return true
		}
		if ci, ok := sub.Get(f).Message().Interface().(*waE2E.ContextInfo); ok {
			out = ci
			return false
		}
		return true
	})
	return out
}

// pollCreation extracts the question and options from any poll-creation variant.
func pollCreation(m *waE2E.Message) (string, []string, bool) {
	pc := m.GetPollCreationMessage()
	if pc == nil {
		pc = m.GetPollCreationMessageV2()
	}
	if pc == nil {
		pc = m.GetPollCreationMessageV3()
	}
	if pc == nil {
		return "", nil, false
	}
	var opts []string
	for _, o := range pc.GetOptions() {
		opts = append(opts, o.GetOptionName())
	}
	return pc.GetName(), opts, true
}

// mediaInfo describes the attachment of a message, if any.
type mediaInfo struct {
	Type, Filename, URL, DirectPath string
	MediaKey, SHA256, EncSHA256     []byte
	Length                          uint64
}

func extractMedia(m *waE2E.Message, id string) (mediaInfo, bool) {
	if m == nil {
		return mediaInfo{}, false
	}
	safeID := sanitizeFilename(id)
	switch {
	case m.GetImageMessage() != nil:
		x := m.GetImageMessage()
		return mediaInfo{"image", "image_" + safeID + extFor(x.GetMimetype(), ".jpg"), x.GetURL(), x.GetDirectPath(), x.GetMediaKey(), x.GetFileSHA256(), x.GetFileEncSHA256(), x.GetFileLength()}, true
	case m.GetVideoMessage() != nil:
		x := m.GetVideoMessage()
		return mediaInfo{"video", "video_" + safeID + extFor(x.GetMimetype(), ".mp4"), x.GetURL(), x.GetDirectPath(), x.GetMediaKey(), x.GetFileSHA256(), x.GetFileEncSHA256(), x.GetFileLength()}, true
	case m.GetPtvMessage() != nil: // round "video note"
		x := m.GetPtvMessage()
		return mediaInfo{"video", "video_" + safeID + extFor(x.GetMimetype(), ".mp4"), x.GetURL(), x.GetDirectPath(), x.GetMediaKey(), x.GetFileSHA256(), x.GetFileEncSHA256(), x.GetFileLength()}, true
	case m.GetAudioMessage() != nil:
		x := m.GetAudioMessage()
		return mediaInfo{"audio", "audio_" + safeID + extFor(x.GetMimetype(), ".ogg"), x.GetURL(), x.GetDirectPath(), x.GetMediaKey(), x.GetFileSHA256(), x.GetFileEncSHA256(), x.GetFileLength()}, true
	case m.GetDocumentMessage() != nil:
		x := m.GetDocumentMessage()
		name := sanitizeFilename(x.GetFileName())
		if name == "" {
			name = "document_" + safeID
		}
		return mediaInfo{"document", name, x.GetURL(), x.GetDirectPath(), x.GetMediaKey(), x.GetFileSHA256(), x.GetFileEncSHA256(), x.GetFileLength()}, true
	case m.GetStickerMessage() != nil:
		x := m.GetStickerMessage()
		return mediaInfo{"sticker", "sticker_" + safeID + ".webp", x.GetURL(), x.GetDirectPath(), x.GetMediaKey(), x.GetFileSHA256(), x.GetFileEncSHA256(), x.GetFileLength()}, true
	}
	return mediaInfo{}, false
}

func extFor(mime, def string) string {
	mime, _, _ = strings.Cut(mime, ";")
	switch strings.TrimSpace(mime) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4", "audio/aac":
		return ".m4a"
	}
	return def
}

// sanitizeFilename keeps a filename safe to join onto a directory.
func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ".")
	return name
}

// recordMessage stores a parsed message and its chat. It is shared by live
// messages and history sync.
func (s *Service) recordMessage(ctx context.Context, evt *events.Message, chatName string) {
	info := evt.Info
	msg := evt.Message
	chat := info.Chat.ToNonAD()
	chatJID := chat.String()

	if pm := msg.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if txt := extractText(pm.GetEditedMessage()); txt != "" {
				_ = s.Store.UpdateContent(chatJID, pm.GetKey().GetID(), txt)
			}
		case waE2E.ProtocolMessage_REVOKE:
			_ = s.Store.UpdateContent(chatJID, pm.GetKey().GetID(), "[deleted]")
		}
		return
	}

	if s.recordReaction(ctx, evt) {
		return
	}

	if q, opts, ok := pollCreation(msg); ok {
		_ = s.Store.SavePoll(store.Poll{ID: info.ID, ChatJID: chatJID, Question: q, Options: opts}, info.Timestamp)
	}

	content := extractText(msg)
	media, hasMedia := extractMedia(msg, info.ID)
	if content == "" && !hasMedia {
		return
	}
	if chatName == "" {
		chatName = s.chatNameFor(ctx, chat)
	}
	if err := s.Store.UpsertChat(chatJID, chatName, info.Timestamp); err != nil {
		s.log.Warnf("store chat: %v", err)
	}
	sender := info.Sender.ToNonAD().String()
	if info.Sender.IsEmpty() {
		sender = chatJID
	}
	m := store.Message{
		ID: info.ID, ChatJID: chatJID, Sender: sender, Content: content,
		Timestamp: info.Timestamp, IsFromMe: info.IsFromMe, QuotedID: quotedID(msg),
	}
	if hasMedia {
		m.MediaType, m.Filename = media.Type, media.Filename
		m.URL, m.DirectPath = media.URL, media.DirectPath
		m.MediaKey, m.FileSHA256, m.FileEncSHA256, m.FileLength = media.MediaKey, media.SHA256, media.EncSHA256, media.Length
	}
	if err := s.Store.StoreMessage(m); err != nil {
		s.log.Warnf("store message: %v", err)
	}
}

func (s *Service) handleMessage(ctx context.Context, evt *events.Message) {
	if pu := evt.Message.GetPollUpdateMessage(); pu != nil {
		s.recordPollVote(ctx, evt, pu.GetPollCreationMessageKey().GetID())
		return
	}
	s.recordMessage(ctx, evt, "")
}

func (s *Service) recordPollVote(ctx context.Context, evt *events.Message, pollID string) {
	vote, err := s.Client.DecryptPollVote(ctx, evt)
	if err != nil {
		s.log.Warnf("decrypt poll vote: %v", err)
		return
	}
	poll, err := s.Store.GetPoll(pollID)
	if err != nil {
		s.log.Debugf("vote for untracked poll %s", pollID)
		return
	}
	chosen := MapPollSelection(poll.Options, vote.GetSelectedOptions())
	if err := s.Store.SaveVote(pollID, evt.Info.Chat.String(), evt.Info.Sender.ToNonAD().String(), chosen, evt.Info.Timestamp); err != nil {
		s.log.Warnf("store poll vote: %v", err)
	}
}

// MapPollSelection converts the SHA-256 option hashes in a decrypted poll
// vote back into option names, in poll order.
func MapPollSelection(options []string, selected [][]byte) []string {
	hashes := whatsmeow.HashPollOptions(options)
	chosen := []string{}
	for i, h := range hashes {
		for _, sel := range selected {
			if string(h) == string(sel) {
				chosen = append(chosen, options[i])
				break
			}
		}
	}
	return chosen
}

func (s *Service) handleHistorySync(ctx context.Context, evt *events.HistorySync) {
	stored := 0
	for _, conv := range evt.Data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		name := conv.GetDisplayName()
		if name == "" {
			name = conv.GetName()
		}
		for _, hm := range conv.GetMessages() {
			wm := hm.GetMessage()
			if wm == nil || wm.GetMessage() == nil {
				continue
			}
			parsed, err := s.Client.ParseWebMessage(chat, wm)
			if err != nil {
				continue
			}
			if pu := parsed.Message.GetPollUpdateMessage(); pu != nil {
				continue // votes in history cannot be matched reliably; skip
			}
			s.recordMessage(ctx, parsed, name)
			stored++
		}
	}
	s.log.Infof("History sync (%s): processed %d messages in %d chats",
		evt.Data.GetSyncType().String(), stored, len(evt.Data.GetConversations()))
}
