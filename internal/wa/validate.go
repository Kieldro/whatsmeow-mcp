package wa

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// ErrInvalidInput marks errors caused by bad caller input (HTTP 400 in REST).
var ErrInvalidInput = errors.New("invalid input")

// invalidf returns an error wrapping ErrInvalidInput.
func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

// vcardEscape escapes a vCard 3.0 text value.
func vcardEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ",", `\,`, ";", `\;`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(s)
}

// BuildVCard builds a minimal vCard 3.0 for a WhatsApp contact card. phone
// must include the country code; the waid parameter lets WhatsApp offer
// "Message" / "Add contact" buttons.
func BuildVCard(name, phone string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalidf("contact name is required")
	}
	digits := jidutil.DigitsOnly(phone)
	if len(digits) < 5 || len(digits) > 15 {
		return "", invalidf("%q is not a phone number with country code", phone)
	}
	n := vcardEscape(name)
	return "BEGIN:VCARD\n" +
		"VERSION:3.0\n" +
		"N:;" + n + ";;;\n" +
		"FN:" + n + "\n" +
		"TEL;type=CELL;type=VOICE;waid=" + digits + ":+" + digits + "\n" +
		"END:VCARD", nil
}

// ValidateLatLng checks that a coordinate pair is on Earth.
func ValidateLatLng(lat, lng float64) error {
	if math.IsNaN(lat) || math.IsNaN(lng) || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return invalidf("latitude must be within [-90, 90] and longitude within [-180, 180]")
	}
	if lat == 0 && lng == 0 {
		return invalidf("latitude and longitude are both 0; pass real coordinates")
	}
	return nil
}

// IsWebP reports whether data is a RIFF/WEBP image.
func IsWebP(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
}

// isAnimatedWebP reports whether a WebP has an animation chunk.
func isAnimatedWebP(data []byte) bool {
	return IsWebP(data) && bytes.Contains(data[12:min(len(data), 4096)], []byte("ANIM"))
}

// IsJPEG reports whether data starts with a JPEG SOI marker.
func IsJPEG(data []byte) bool {
	return len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
}

// ParseMuteDuration parses how long to mute a chat. "", "forever", "always"
// and "permanent" mean indefinitely (returned as 0). Otherwise it accepts Go
// durations ("8h", "90m") plus day/week units ("1d", "2w").
func ParseMuteDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	switch s {
	case "", "forever", "always", "permanent", "indefinitely":
		return 0, nil
	}
	var d time.Duration
	var err error
	switch {
	case strings.HasSuffix(s, "d") || strings.HasSuffix(s, "w"):
		unit := 24 * time.Hour
		if strings.HasSuffix(s, "w") {
			unit *= 7
		}
		var n float64
		n, err = strconv.ParseFloat(s[:len(s)-1], 64)
		d = time.Duration(n * float64(unit))
	default:
		d, err = time.ParseDuration(s)
	}
	if err != nil || d <= 0 {
		return 0, invalidf("mute duration %q not understood (use e.g. 8h, 1d, 1w or forever)", s)
	}
	return d, nil
}

// ParseDisappearingTimer parses off/24h/7d/90d (and whatsmeow's aliases).
func ParseDisappearingTimer(s string) (time.Duration, error) {
	d, ok := whatsmeow.ParseDisappearingTimerString(strings.TrimSpace(s))
	if !ok {
		return 0, invalidf("disappearing timer must be off, 24h, 7d or 90d (got %q)", s)
	}
	return d, nil
}

// privacyNames maps friendly names to whatsmeow privacy setting types, with
// the values WhatsApp accepts for each.
var privacyNames = map[string]struct {
	typ    types.PrivacySettingType
	values []types.PrivacySetting
}{
	"group_add":     {types.PrivacySettingTypeGroupAdd, []types.PrivacySetting{"all", "contacts", "contact_blacklist", "none"}},
	"last_seen":     {types.PrivacySettingTypeLastSeen, []types.PrivacySetting{"all", "contacts", "contact_blacklist", "none"}},
	"status":        {types.PrivacySettingTypeStatus, []types.PrivacySetting{"all", "contacts", "contact_blacklist", "none"}},
	"profile":       {types.PrivacySettingTypeProfile, []types.PrivacySetting{"all", "contacts", "contact_blacklist", "none"}},
	"read_receipts": {types.PrivacySettingTypeReadReceipts, []types.PrivacySetting{"all", "none"}},
	"online":        {types.PrivacySettingTypeOnline, []types.PrivacySetting{"all", "match_last_seen"}},
	"call_add":      {types.PrivacySettingTypeCallAdd, []types.PrivacySetting{"all", "known"}},
	"messages":      {types.PrivacySettingTypeMessages, []types.PrivacySetting{"all", "contacts"}},
	"defense":       {types.PrivacySettingTypeDefense, []types.PrivacySetting{"on_standard", "off"}},
	"stickers":      {types.PrivacySettingTypeStickers, []types.PrivacySetting{"contacts", "contact_allowlist", "none"}},
}

// ParsePrivacySetting validates a privacy setting name and value. Both the
// friendly names (last_seen) and WhatsApp's raw names (last) are accepted.
func ParsePrivacySetting(name, value string) (types.PrivacySettingType, types.PrivacySetting, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.ToLower(strings.TrimSpace(value))
	for friendly, p := range privacyNames {
		if name != friendly && name != string(p.typ) {
			continue
		}
		for _, v := range p.values {
			if string(v) == value {
				return p.typ, v, nil
			}
		}
		return "", "", invalidf("%s accepts %v (got %q)", friendly, p.values, value)
	}
	return "", "", invalidf("unknown privacy setting %q; one of group_add, last_seen, status, profile, read_receipts, online, call_add, messages, defense, stickers", name)
}

// ParseChatPresence maps typing/recording/paused to whatsmeow values.
func ParseChatPresence(state string) (types.ChatPresence, types.ChatPresenceMedia, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "typing", "composing":
		return types.ChatPresenceComposing, types.ChatPresenceMediaText, nil
	case "recording", "recording_audio":
		return types.ChatPresenceComposing, types.ChatPresenceMediaAudio, nil
	case "paused", "stop", "stopped":
		return types.ChatPresencePaused, types.ChatPresenceMediaText, nil
	}
	return "", "", invalidf("state must be typing, recording or paused (got %q)", state)
}

// ParsePresence maps available/unavailable to whatsmeow values.
func ParsePresence(state string) (types.Presence, error) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "available", "online":
		return types.PresenceAvailable, nil
	case "unavailable", "offline":
		return types.PresenceUnavailable, nil
	}
	return "", invalidf("state must be available or unavailable (got %q)", state)
}

// keyParticipant returns the participant part of an app-state message index:
// the sender for others' messages in groups, otherwise "0".
func keyParticipant(chat, sender types.JID, fromMe bool) string {
	if fromMe || chat.Server != types.GroupServer || sender.IsEmpty() {
		return "0"
	}
	return sender.String()
}
