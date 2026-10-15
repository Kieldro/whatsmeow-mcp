// Package jidutil parses and normalizes the many ways a user can name a
// WhatsApp chat: bare phone numbers, formatted numbers and full JIDs.
package jidutil

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// StatusBroadcast is the pseudo-chat that holds WhatsApp Status (stories).
const StatusBroadcast = "status@broadcast"

// DigitsOnly strips everything except ASCII digits ("+1 (555) 010-0100" -> "15550100100").
func DigitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Parse turns a phone number or JID string into a JID.
//
// Phone numbers may contain spaces, dashes, parentheses and a leading "+";
// they must include the country code. JIDs ("…@s.whatsapp.net", "…@lid",
// "…@g.us") are parsed as-is, with device suffixes dropped.
func Parse(s string) (types.JID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.EmptyJID, fmt.Errorf("empty recipient")
	}
	if strings.Contains(s, "@") {
		j, err := types.ParseJID(s)
		if err != nil {
			return types.EmptyJID, fmt.Errorf("invalid JID %q: %w", s, err)
		}
		if j.User == "" && j.Server != types.BroadcastServer {
			return types.EmptyJID, fmt.Errorf("invalid JID %q: missing user part", s)
		}
		return j.ToNonAD(), nil
	}
	digits := DigitsOnly(s)
	if len(digits) < 5 {
		return types.EmptyJID, fmt.Errorf("%q is not a phone number (include the country code) or a JID", s)
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

// ParseList parses each entry with Parse.
func ParseList(list []string) ([]types.JID, error) {
	out := make([]types.JID, 0, len(list))
	for _, s := range list {
		j, err := Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

// FromStoredSender converts a sender column value back into a JID.
//
// Older rows store only the user part of the sender; in that case a long
// numeric user (more than 13 digits) is assumed to be a LID, since phone
// numbers are at most 15 digits but in practice 13 or fewer.
func FromStoredSender(sender string) (types.JID, bool) {
	if sender == "" {
		return types.EmptyJID, false
	}
	if strings.Contains(sender, "@") {
		j, err := types.ParseJID(sender)
		if err != nil {
			return types.EmptyJID, false
		}
		return j.ToNonAD(), true
	}
	if len(sender) > 13 {
		return types.NewJID(sender, types.HiddenUserServer), true
	}
	return types.NewJID(sender, types.DefaultUserServer), true
}

// IsGroup reports whether the JID string names a group chat.
func IsGroup(jid string) bool { return strings.HasSuffix(jid, "@"+types.GroupServer) }

// UserPart returns the part of a JID string before "@" and any device suffix.
func UserPart(jid string) string {
	u, _, _ := strings.Cut(jid, "@")
	u, _, _ = strings.Cut(u, ":")
	return u
}
