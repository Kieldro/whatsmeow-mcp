package wa

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// SendLocation sends a location pin.
func (s *Service) SendLocation(ctx context.Context, recipient string, lat, lng float64, name, address string) (Sent, error) {
	chat, err := SendTarget(recipient)
	if err != nil {
		return Sent{}, invalidf("%v", err)
	}
	if err := ValidateLatLng(lat, lng); err != nil {
		return Sent{}, err
	}
	loc := &waE2E.LocationMessage{DegreesLatitude: proto.Float64(lat), DegreesLongitude: proto.Float64(lng)}
	if name != "" {
		loc.Name = proto.String(name)
	}
	if address != "" {
		loc.Address = proto.String(address)
	}
	return s.send(ctx, chat, &waE2E.Message{LocationMessage: loc})
}

// SendContactCard sends a contact card (vCard) for name and phone.
func (s *Service) SendContactCard(ctx context.Context, recipient, name, phone string) (Sent, error) {
	chat, err := SendTarget(recipient)
	if err != nil {
		return Sent{}, invalidf("%v", err)
	}
	vcard, err := BuildVCard(name, phone)
	if err != nil {
		return Sent{}, err
	}
	return s.send(ctx, chat, &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String(strings.TrimSpace(name)), Vcard: proto.String(vcard),
	}})
}

// SendSticker uploads a .webp file and sends it as a sticker.
func (s *Service) SendSticker(ctx context.Context, recipient, path string) (Sent, error) {
	chat, err := SendTarget(recipient)
	if err != nil {
		return Sent{}, invalidf("%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Sent{}, invalidf("read sticker: %v", err)
	}
	if !IsWebP(data) {
		return Sent{}, invalidf("%s is not a WebP image; stickers must be .webp (ideally 512x512)", path)
	}
	if err := s.Ready(); err != nil {
		return Sent{}, err
	}
	up, err := s.Client.Upload(ctx, data, whatsmeow.MediaImage)
	if err != nil {
		return Sent{}, fmt.Errorf("upload: %w", err)
	}
	return s.send(ctx, chat, &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		Mimetype: proto.String("image/webp"), IsAnimated: proto.Bool(isAnimatedWebP(data)),
		URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256,
		FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
	}})
}

// forwardContent rebuilds a stored message for forwarding. Media is re-sent
// by reference (same encrypted upload), as WhatsApp clients do.
func forwardContent(m store.Message) (*waE2E.Message, error) {
	ci := &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(1)}
	if m.MediaType == "" {
		if m.Content == "" || m.Content == "[deleted]" {
			return nil, invalidf("message has no forwardable content")
		}
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(m.Content), ContextInfo: ci}}, nil
	}
	dp := m.DirectPath
	if dp == "" {
		dp = ExtractDirectPathFromURL(m.URL)
	}
	if dp == "" || len(m.MediaKey) == 0 || len(m.FileEncSHA256) == 0 {
		return nil, invalidf("stored media is incomplete; use download_media then send_file instead")
	}
	var url *string
	if m.URL != "" {
		url = proto.String(m.URL)
	}
	_, mime := mediaKind(m.Filename)
	var caption *string
	if m.Content != "" {
		caption = proto.String(m.Content)
	}
	length := proto.Uint64(m.FileLength)
	switch m.MediaType {
	case "image":
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: caption, Mimetype: proto.String(mime), URL: url,
			DirectPath: proto.String(dp), MediaKey: m.MediaKey, FileEncSHA256: m.FileEncSHA256, FileSHA256: m.FileSHA256,
			FileLength: length, ContextInfo: ci}}, nil
	case "video":
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: caption, Mimetype: proto.String(mime), URL: url,
			DirectPath: proto.String(dp), MediaKey: m.MediaKey, FileEncSHA256: m.FileEncSHA256, FileSHA256: m.FileSHA256,
			FileLength: length, ContextInfo: ci}}, nil
	case "audio":
		ptt := strings.HasSuffix(strings.ToLower(m.Filename), ".ogg")
		if ptt {
			mime = "audio/ogg; codecs=opus"
		}
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String(mime), PTT: proto.Bool(ptt), URL: url,
			DirectPath: proto.String(dp), MediaKey: m.MediaKey, FileEncSHA256: m.FileEncSHA256, FileSHA256: m.FileSHA256,
			FileLength: length, ContextInfo: ci}}, nil
	case "sticker":
		return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{Mimetype: proto.String("image/webp"), URL: url,
			DirectPath: proto.String(dp), MediaKey: m.MediaKey, FileEncSHA256: m.FileEncSHA256, FileSHA256: m.FileSHA256,
			FileLength: length, ContextInfo: ci}}, nil
	case "document":
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: caption, Mimetype: proto.String(mime), URL: url,
			Title: proto.String(m.Filename), FileName: proto.String(m.Filename),
			DirectPath: proto.String(dp), MediaKey: m.MediaKey, FileEncSHA256: m.FileEncSHA256, FileSHA256: m.FileSHA256,
			FileLength: length, ContextInfo: ci}}, nil
	}
	return nil, invalidf("cannot forward media type %q", m.MediaType)
}

// Forward re-sends a stored message (text or media) to another chat, marked as forwarded.
func (s *Service) Forward(ctx context.Context, chatJID, id, to string) (Sent, error) {
	src, err := jidutil.Parse(chatJID)
	if err != nil {
		return Sent{}, invalidf("%v", err)
	}
	dst, err := SendTarget(to)
	if err != nil {
		return Sent{}, invalidf("%v", err)
	}
	m, err := s.Store.GetMessage(src.String(), id)
	if err != nil {
		return Sent{}, invalidf("message %s not found in local history of %s", id, src)
	}
	msg, err := forwardContent(m)
	if err != nil {
		return Sent{}, err
	}
	return s.send(ctx, dst, msg)
}

// messageKeyInfo returns the sender and from-me flag of a message for app-state indexes.
func (s *Service) messageKeyInfo(chat types.JID, id string) (sender types.JID, fromMe bool, ts time.Time) {
	sender, m := s.targetSender(chat, id)
	if m != nil {
		fromMe, ts = m.IsFromMe, m.Timestamp
	}
	return sender, fromMe, ts
}

// Star stars or unstars a message (synced to your other devices).
func (s *Service) Star(ctx context.Context, chatJID, id string, starred bool) error {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return invalidf("%v", err)
	}
	if id == "" {
		return invalidf("message_id is required")
	}
	if err := s.Ready(); err != nil {
		return err
	}
	sender, fromMe, _ := s.messageKeyInfo(chat, id)
	if keyParticipant(chat, sender, fromMe) == "0" {
		sender = chat // BuildStar writes "0" when sender and chat share a user
	}
	return s.Client.SendAppState(ctx, appstate.BuildStar(chat, sender, id, fromMe, starred))
}

// buildDeleteForMe builds the app-state patch that hides one message on your devices.
func buildDeleteForMe(chat, sender types.JID, id string, fromMe, deleteMedia bool, ts time.Time) appstate.PatchInfo {
	me := "0"
	if fromMe {
		me = "1"
	}
	action := &waSyncAction.DeleteMessageForMeAction{DeleteMedia: proto.Bool(deleteMedia)}
	if !ts.IsZero() {
		action.MessageTimestamp = proto.Int64(ts.Unix())
	}
	return appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexDeleteMessageForMe, chat.String(), id, me, keyParticipant(chat, sender, fromMe)},
			Version: 3,
			Value:   &waSyncAction.SyncActionValue{DeleteMessageForMeAction: action},
		}},
	}
}

// DeleteForMe hides a message on this account's devices only (others still see it).
func (s *Service) DeleteForMe(ctx context.Context, chatJID, id string, deleteMedia bool) error {
	chat, err := jidutil.Parse(chatJID)
	if err != nil {
		return invalidf("%v", err)
	}
	if id == "" {
		return invalidf("message_id is required")
	}
	if err := s.Ready(); err != nil {
		return err
	}
	sender, fromMe, ts := s.messageKeyInfo(chat, id)
	if err := s.Client.SendAppState(ctx, buildDeleteForMe(chat, sender, id, fromMe, deleteMedia, ts)); err != nil {
		return err
	}
	_ = s.Store.DeleteMessage(chat.String(), id)
	return nil
}

// ParseARGB parses "#RRGGBB" or "#AARRGGBB" into an ARGB value (opaque if no alpha).
func ParseARGB(s string) (uint32, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) != 6 && len(h) != 8 {
		return 0, invalidf("color %q must be #RRGGBB or #AARRGGBB", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, invalidf("color %q must be hex", s)
	}
	if len(h) == 6 {
		v |= 0xFF000000
	}
	return uint32(v), nil
}

// PostTextStatus posts a text Status (story) to your contacts, per your status privacy settings.
func (s *Service) PostTextStatus(ctx context.Context, text, background string) (Sent, error) {
	if strings.TrimSpace(text) == "" {
		return Sent{}, invalidf("status text is empty")
	}
	bg := uint32(0xFF128C7E)
	if background != "" {
		var err error
		if bg, err = ParseARGB(background); err != nil {
			return Sent{}, err
		}
	}
	return s.send(ctx, types.StatusBroadcastJID, &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(text), BackgroundArgb: proto.Uint32(bg), TextArgb: proto.Uint32(0xFFFFFFFF),
		Font: waE2E.ExtendedTextMessage_SYSTEM.Enum(),
	}})
}

// PostMediaStatus posts an image or video Status with an optional caption.
func (s *Service) PostMediaStatus(ctx context.Context, path, caption string) (Sent, error) {
	if kind, _ := mediaKind(path); kind != whatsmeow.MediaImage && kind != whatsmeow.MediaVideo {
		return Sent{}, invalidf("status media must be an image (.jpg/.png) or video (.mp4/.mov)")
	}
	if _, err := os.Stat(path); err != nil {
		return Sent{}, invalidf("%v", err)
	}
	return s.SendFile(ctx, jidutil.StatusBroadcast, path, caption)
}

// ListStatuses returns recent Status posts stored locally, newest first.
func (s *Service) ListStatuses(ctx context.Context, sender string, limit int) ([]MessageView, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.ListMessages(ctx, MessageQuery{Chat: jidutil.StatusBroadcast, Sender: sender, IncludeStatus: true, Limit: min(limit, 200)})
}
