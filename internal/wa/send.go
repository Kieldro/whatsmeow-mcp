package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/Kieldro/whatsmeow-mcp/internal/audio"
	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// Sent identifies a message this client sent.
type Sent struct {
	ID        string    `json:"id"`
	ChatJID   string    `json:"chat_jid"`
	Timestamp time.Time `json:"timestamp"`
}

// send sends msg and records it in the local store so it shows up in
// list_messages and can be replied to, edited or reacted to later.
func (s *Service) send(ctx context.Context, chat types.JID, msg *waE2E.Message) (Sent, error) {
	if err := s.Ready(); err != nil {
		return Sent{}, err
	}
	resp, err := s.Client.SendMessage(ctx, chat, msg)
	if err != nil {
		return Sent{}, err
	}
	out := Sent{ID: resp.ID, ChatJID: chat.String(), Timestamp: resp.Timestamp}
	content := extractText(msg)
	media, hasMedia := extractMedia(msg, resp.ID)
	if content != "" || hasMedia {
		_ = s.Store.UpsertChat(out.ChatJID, s.chatNameFor(ctx, chat), resp.Timestamp)
		m := store.Message{ID: resp.ID, ChatJID: out.ChatJID, Sender: s.Client.Store.ID.ToNonAD().String(),
			Content: content, Timestamp: resp.Timestamp, IsFromMe: true}
		if hasMedia {
			m.MediaType, m.Filename = media.Type, media.Filename
			m.URL, m.DirectPath = media.URL, media.DirectPath
			m.MediaKey, m.FileSHA256, m.FileEncSHA256, m.FileLength = media.MediaKey, media.SHA256, media.EncSHA256, media.Length
		}
		_ = s.Store.StoreMessage(m)
	}
	return out, nil
}

// targetSender works out who sent message id in chat (needed for reactions,
// quoted replies, pins and mark-read) and returns the stored message if known.
func (s *Service) targetSender(chat types.JID, id string) (types.JID, *store.Message) {
	m, err := s.Store.GetMessage(chat.String(), id)
	if err != nil {
		// Unknown message: in a DM assume the other party sent it.
		if chat.Server != types.GroupServer {
			return chat, nil
		}
		return types.EmptyJID, nil
	}
	if m.IsFromMe {
		return s.Client.Store.ID.ToNonAD(), &m
	}
	if chat.Server != types.GroupServer {
		return chat, &m
	}
	if j, ok := jidutil.FromStoredSender(m.Sender); ok {
		return j, &m
	}
	return types.EmptyJID, &m
}

// SendText sends a text message, optionally as a quoted reply to replyTo.
func (s *Service) SendText(ctx context.Context, recipient, text, replyTo string) (Sent, error) {
	chat, err := SendTarget(recipient)
	if err != nil {
		return Sent{}, err
	}
	if text == "" {
		return Sent{}, errors.New("message text is empty")
	}
	if replyTo == "" {
		return s.send(ctx, chat, &waE2E.Message{Conversation: proto.String(text)})
	}
	sender, quoted := s.targetSender(chat, replyTo)
	ci := &waE2E.ContextInfo{StanzaID: proto.String(replyTo)}
	if !sender.IsEmpty() {
		ci.Participant = proto.String(sender.String())
	}
	if quoted != nil {
		ci.QuotedMessage = &waE2E.Message{Conversation: proto.String(quoted.Content)}
	} else {
		ci.QuotedMessage = &waE2E.Message{Conversation: proto.String("")}
	}
	return s.send(ctx, chat, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(text), ContextInfo: ci,
	}})
}

// mediaKind maps a file extension to a WhatsApp media type and MIME type.
func mediaKind(path string) (whatsmeow.MediaType, string) {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case "jpg", "jpeg":
		return whatsmeow.MediaImage, "image/jpeg"
	case "png":
		return whatsmeow.MediaImage, "image/png"
	case "gif":
		return whatsmeow.MediaImage, "image/gif"
	case "webp":
		return whatsmeow.MediaImage, "image/webp"
	case "ogg", "opus":
		return whatsmeow.MediaAudio, "audio/ogg; codecs=opus"
	case "mp3":
		return whatsmeow.MediaAudio, "audio/mpeg"
	case "m4a":
		return whatsmeow.MediaAudio, "audio/mp4"
	case "mp4":
		return whatsmeow.MediaVideo, "video/mp4"
	case "mov":
		return whatsmeow.MediaVideo, "video/quicktime"
	case "avi":
		return whatsmeow.MediaVideo, "video/avi"
	case "pdf":
		return whatsmeow.MediaDocument, "application/pdf"
	case "txt":
		return whatsmeow.MediaDocument, "text/plain"
	}
	return whatsmeow.MediaDocument, "application/octet-stream"
}

// SendFile uploads and sends a file. Images and videos are sent as media with
// an optional caption; .ogg/.opus as a voice note; anything else as a document.
func (s *Service) SendFile(ctx context.Context, recipient, path, caption string) (Sent, error) {
	chat, err := SendTarget(recipient)
	if err != nil {
		return Sent{}, err
	}
	if err := s.Ready(); err != nil {
		return Sent{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Sent{}, fmt.Errorf("read file: %w", err)
	}
	kind, mime := mediaKind(path)
	if kind == whatsmeow.MediaAudio && !audio.IsOgg(data) {
		// Non-Opus audio is sent as a plain audio attachment.
		mime = strings.TrimSpace(strings.Split(mime, ";")[0])
	}
	up, err := s.Client.Upload(ctx, data, kind)
	if err != nil {
		return Sent{}, fmt.Errorf("upload: %w", err)
	}
	msg := &waE2E.Message{}
	switch kind {
	case whatsmeow.MediaImage:
		msg.ImageMessage = &waE2E.ImageMessage{Caption: proto.String(caption), Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256,
			FileSHA256: up.FileSHA256, FileLength: &up.FileLength}
	case whatsmeow.MediaVideo:
		msg.VideoMessage = &waE2E.VideoMessage{Caption: proto.String(caption), Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256,
			FileSHA256: up.FileSHA256, FileLength: &up.FileLength}
	case whatsmeow.MediaAudio:
		am := &waE2E.AudioMessage{Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256,
			FileSHA256: up.FileSHA256, FileLength: &up.FileLength}
		if audio.IsOgg(data) {
			secs, wave, err := audio.AnalyzeOggOpus(data)
			if err != nil {
				return Sent{}, err
			}
			am.Seconds, am.PTT, am.Waveform = proto.Uint32(secs), proto.Bool(true), wave
		}
		msg.AudioMessage = am
	default:
		name := filepath.Base(path)
		msg.DocumentMessage = &waE2E.DocumentMessage{Title: proto.String(name), FileName: proto.String(name),
			Caption: proto.String(caption), Mimetype: proto.String(mime),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256,
			FileSHA256: up.FileSHA256, FileLength: &up.FileLength}
	}
	return s.send(ctx, chat, msg)
}

// SendAudio sends any audio file as a voice note, converting to Ogg/Opus
// with ffmpeg when needed.
func (s *Service) SendAudio(ctx context.Context, recipient, path string) (Sent, error) {
	if err := s.Ready(); err != nil {
		return Sent{}, err
	}
	if _, err := os.Stat(path); err != nil {
		return Sent{}, err
	}
	if audio.NeedsConversion(path) {
		conv, err := audio.ConvertToOpus(ctx, path)
		if err != nil {
			return Sent{}, err
		}
		defer os.Remove(conv)
		path = conv
	}
	return s.SendFile(ctx, recipient, path, "")
}

// Edit replaces the text of a message this account sent (WhatsApp allows
// edits for about 15-20 minutes).
func (s *Service) Edit(ctx context.Context, chatJID, id, text string) (Sent, error) {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return Sent{}, err
	}
	if err := s.Ready(); err != nil {
		return Sent{}, err
	}
	resp, err := s.Client.SendMessage(ctx, chat, s.Client.BuildEdit(chat, id, &waE2E.Message{Conversation: proto.String(text)}))
	if err != nil {
		return Sent{}, err
	}
	_ = s.Store.UpdateContent(chat.String(), id, text)
	return Sent{ID: id, ChatJID: chat.String(), Timestamp: resp.Timestamp}, nil
}

// Revoke deletes a message for everyone. sender may be empty for your own
// messages; group admins can pass another member's JID.
func (s *Service) Revoke(ctx context.Context, chatJID, id, sender string) error {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return err
	}
	if err := s.Ready(); err != nil {
		return err
	}
	var sj types.JID
	if sender != "" {
		if sj, err = jidutil.Parse(sender); err != nil {
			return err
		}
	}
	if _, err := s.Client.SendMessage(ctx, chat, s.Client.BuildRevoke(chat, sj, id)); err != nil {
		return err
	}
	_ = s.Store.UpdateContent(chat.String(), id, "[deleted]")
	return nil
}

// React adds (or with an empty emoji, removes) a reaction.
func (s *Service) React(ctx context.Context, chatJID, id, emoji string) error {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return err
	}
	if err := s.Ready(); err != nil {
		return err
	}
	sender, _ := s.targetSender(chat, id)
	_, err = s.Client.SendMessage(ctx, chat, s.Client.BuildReaction(chat, sender, id, emoji))
	return err
}

// Pin pins (or unpins) a message for everyone in the chat.
func (s *Service) Pin(ctx context.Context, chatJID, id string, days int, unpin bool) error {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return err
	}
	if err := s.Ready(); err != nil {
		return err
	}
	sender, _ := s.targetSender(chat, id)
	typ := waE2E.PinInChatMessage_PIN_FOR_ALL
	if unpin {
		typ = waE2E.PinInChatMessage_UNPIN_FOR_ALL
	}
	msg := &waE2E.Message{PinInChatMessage: &waE2E.PinInChatMessage{
		Key:               s.Client.BuildMessageKey(chat, sender, id),
		Type:              typ.Enum(),
		SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
	}}
	if !unpin {
		switch {
		case days <= 1:
			days = 1
		case days <= 7:
			days = 7
		default:
			days = 30
		}
		msg.MessageContextInfo = &waE2E.MessageContextInfo{MessageAddOnDurationInSecs: proto.Uint32(uint32(days * 24 * 3600))}
	}
	_, err = s.Client.SendMessage(ctx, chat, msg)
	return err
}

// MarkRead sends read receipts. In groups the sender of the messages is
// needed; when omitted it is looked up from the first message.
func (s *Service) MarkRead(ctx context.Context, chatJID string, ids []string, sender string) error {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("no message ids")
	}
	if err := s.Ready(); err != nil {
		return err
	}
	sj := chat
	if sender != "" {
		if sj, err = jidutil.Parse(sender); err != nil {
			return err
		}
	} else if chat.Server == types.GroupServer {
		sj, _ = s.targetSender(chat, ids[0])
	}
	mids := make([]types.MessageID, len(ids))
	copy(mids, ids)
	return s.Client.MarkRead(ctx, mids, time.Now(), chat, sj)
}

// CreatePoll sends a poll and tracks it so votes can be tallied.
func (s *Service) CreatePoll(ctx context.Context, recipient, question string, options []string, multi bool) (Sent, error) {
	chat, err := SendTarget(recipient)
	if err != nil {
		return Sent{}, err
	}
	if question == "" || len(options) < 2 {
		return Sent{}, errors.New("a poll needs a question and at least 2 options")
	}
	if err := s.Ready(); err != nil {
		return Sent{}, err
	}
	selectable := 1
	if multi {
		selectable = 0
	}
	sent, err := s.send(ctx, chat, s.Client.BuildPollCreation(question, options, selectable))
	if err != nil {
		return sent, err
	}
	_ = s.Store.SavePoll(store.Poll{ID: sent.ID, ChatJID: sent.ChatJID, Question: question, Options: options}, time.Now())
	return sent, nil
}

// PollResults is the tally of a tracked poll.
type PollResults struct {
	ID       string              `json:"id"`
	ChatJID  string              `json:"chat_jid"`
	Question string              `json:"question"`
	Options  []string            `json:"options"`
	Votes    map[string][]string `json:"votes"`
	Voters   map[string]string   `json:"voter_names,omitempty"`
}

// PollResults tallies votes for a poll seen or sent by this client.
func (s *Service) PollResults(ctx context.Context, id string) (PollResults, error) {
	p, err := s.Store.GetPoll(id)
	if err != nil {
		return PollResults{}, fmt.Errorf("unknown poll %q (only polls received or sent while this client was running are tracked)", id)
	}
	votes, err := s.Store.Votes(id)
	if err != nil {
		return PollResults{}, err
	}
	r := s.NewResolver()
	names := map[string]string{}
	for voter := range votes {
		if n := r.Resolve(ctx, voter).Name; n != "" {
			names[voter] = n
		}
	}
	return PollResults{ID: p.ID, ChatJID: p.ChatJID, Question: p.Question, Options: p.Options,
		Votes: store.TallyVotes(p.Options, votes), Voters: names}, nil
}
