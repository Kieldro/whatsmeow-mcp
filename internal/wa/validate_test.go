package wa

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

func TestBuildVCard(t *testing.T) {
	got, err := BuildVCard(" Test User ", "+1 (555) 010-0100")
	if err != nil {
		t.Fatal(err)
	}
	want := "BEGIN:VCARD\nVERSION:3.0\nN:;Test User;;;\nFN:Test User\nTEL;type=CELL;type=VOICE;waid=15550100100:+15550100100\nEND:VCARD"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	esc, err := BuildVCard("Doe, Jane; Esq\nJr", "15550100")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(esc, `FN:Doe\, Jane\; Esq\nJr`+"\n") {
		t.Fatalf("special characters not escaped: %q", esc)
	}
	for _, c := range [][2]string{{"", "15550100"}, {"Name", "12"}, {"Name", "1234567890123456"}} {
		if _, err := BuildVCard(c[0], c[1]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("BuildVCard(%q, %q) should be invalid input, got %v", c[0], c[1], err)
		}
	}
}

func TestParseMuteDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"": 0, "forever": 0, "Always": 0, "8h": 8 * time.Hour, "90m": 90 * time.Minute,
		"1d": 24 * time.Hour, "1.5d": 36 * time.Hour, "2w": 14 * 24 * time.Hour, "1 w": 7 * 24 * time.Hour,
	}
	for in, want := range ok {
		got, err := ParseMuteDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseMuteDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"soon", "-1h", "0h", "xd", "0d"} {
		if _, err := ParseMuteDuration(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ParseMuteDuration(%q) should fail, got %v", in, err)
		}
	}
}

func TestParseDisappearingTimer(t *testing.T) {
	ok := map[string]time.Duration{"off": 0, "0": 0, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "90d": 90 * 24 * time.Hour, " 7D ": 7 * 24 * time.Hour}
	for in, want := range ok {
		got, err := ParseDisappearingTimer(in)
		if err != nil || got != want {
			t.Errorf("ParseDisappearingTimer(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "1h", "30d", "forever"} {
		if _, err := ParseDisappearingTimer(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ParseDisappearingTimer(%q) should fail, got %v", in, err)
		}
	}
}

func TestParsePrivacySetting(t *testing.T) {
	typ, val, err := ParsePrivacySetting("last_seen", "Contacts")
	if err != nil || typ != types.PrivacySettingTypeLastSeen || val != types.PrivacySettingContacts {
		t.Fatalf("got %v %v %v", typ, val, err)
	}
	if typ, _, err := ParsePrivacySetting("readreceipts", "none"); err != nil || typ != types.PrivacySettingTypeReadReceipts {
		t.Fatalf("raw name not accepted: %v %v", typ, err)
	}
	for _, c := range [][2]string{{"online", "contacts"}, {"read_receipts", "contacts"}, {"nope", "all"}, {"defense", "on"}} {
		if _, _, err := ParsePrivacySetting(c[0], c[1]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ParsePrivacySetting(%q, %q) should fail, got %v", c[0], c[1], err)
		}
	}
}

func TestParsePresence(t *testing.T) {
	cases := map[string][2]string{"typing": {"composing", ""}, "recording": {"composing", "audio"}, "PAUSED": {"paused", ""}}
	for in, want := range cases {
		cp, media, err := ParseChatPresence(in)
		if err != nil || string(cp) != want[0] || string(media) != want[1] {
			t.Errorf("ParseChatPresence(%q) = %q %q %v", in, cp, media, err)
		}
	}
	if _, _, err := ParseChatPresence("dancing"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown chat presence should fail")
	}
	if p, err := ParsePresence("online"); err != nil || p != types.PresenceAvailable {
		t.Errorf("ParsePresence(online) = %v %v", p, err)
	}
	if _, err := ParsePresence("busy"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown presence should fail")
	}
}

func TestValidateLatLng(t *testing.T) {
	if err := ValidateLatLng(40.7, -74.0); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]float64{{91, 0}, {0, 181}, {-90.1, 10}, {0, 0}} {
		if err := ValidateLatLng(c[0], c[1]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ValidateLatLng(%v) should fail", c)
		}
	}
}

func TestImageSniffing(t *testing.T) {
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 20)...)
	if !IsWebP(webp) || isAnimatedWebP(webp) {
		t.Fatal("static webp misdetected")
	}
	anim := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X"), []byte("\x00\x00\x00\x00ANIM")...)
	if !isAnimatedWebP(anim) {
		t.Fatal("animated webp not detected")
	}
	if IsWebP([]byte("\x89PNG\r\n\x1a\n0000")) || IsWebP(nil) {
		t.Fatal("non-webp accepted")
	}
	if !IsJPEG([]byte{0xFF, 0xD8, 0xFF, 0xE0}) || IsJPEG([]byte("RIFF")) {
		t.Fatal("jpeg sniffing wrong")
	}
}

func TestParseARGB(t *testing.T) {
	cases := map[string]uint32{"#128C7E": 0xFF128C7E, "80000000": 0x80000000, " #ffffff ": 0xFFFFFFFF}
	for in, want := range cases {
		if got, err := ParseARGB(in); err != nil || got != want {
			t.Errorf("ParseARGB(%q) = %#x %v, want %#x", in, got, err, want)
		}
	}
	for _, in := range []string{"red", "#12345", "#GGGGGG"} {
		if _, err := ParseARGB(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("ParseARGB(%q) should fail", in)
		}
	}
}

func TestInviteCode(t *testing.T) {
	for _, in := range []string{"https://chat.whatsapp.com/AbCdEf123", "chat.whatsapp.com/AbCdEf123", "AbCdEf123", " https://chat.whatsapp.com/AbCdEf123?mode=r_c "} {
		if got, err := inviteCode(in); err != nil || got != "AbCdEf123" {
			t.Errorf("inviteCode(%q) = %q %v", in, got, err)
		}
	}
	for _, in := range []string{"", "https://example.com/a/b"} {
		if _, err := inviteCode(in); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("inviteCode(%q) should fail", in)
		}
	}
}

func TestKeyParticipantAndDeleteForMe(t *testing.T) {
	group := types.NewJID("123456789-123", types.GroupServer)
	dm := types.NewJID("15550100", types.DefaultUserServer)
	member := types.NewJID("15550101", types.DefaultUserServer)
	if got := keyParticipant(group, member, false); got != member.String() {
		t.Errorf("group, other's message: %q", got)
	}
	if got := keyParticipant(group, member, true); got != "0" {
		t.Errorf("group, own message: %q", got)
	}
	if got := keyParticipant(dm, dm, false); got != "0" {
		t.Errorf("dm: %q", got)
	}
	ts := time.Unix(1700000000, 0)
	p := buildDeleteForMe(group, member, "MSGID", false, true, ts)
	if p.Type != appstate.WAPatchRegularHigh || len(p.Mutations) != 1 {
		t.Fatalf("patch = %+v", p)
	}
	m := p.Mutations[0]
	if want := []string{"deleteMessageForMe", group.String(), "MSGID", "0", member.String()}; !reflect.DeepEqual(m.Index, want) {
		t.Errorf("index = %v, want %v", m.Index, want)
	}
	a := m.Value.GetDeleteMessageForMeAction()
	if m.Version != 3 || !a.GetDeleteMedia() || a.GetMessageTimestamp() != 1700000000 {
		t.Errorf("mutation = %+v", m)
	}
}

func TestForwardContent(t *testing.T) {
	txt, err := forwardContent(store.Message{Content: "hello"})
	if err != nil || txt.GetExtendedTextMessage().GetText() != "hello" || !txt.GetExtendedTextMessage().GetContextInfo().GetIsForwarded() {
		t.Fatalf("text forward: %v %v", txt, err)
	}
	img, err := forwardContent(store.Message{Content: "cap", MediaType: "image", Filename: "image_X.png",
		URL: "https://mmg.whatsapp.net/v/t62/abc.enc?oh=1", MediaKey: []byte{1}, FileEncSHA256: []byte{2}, FileSHA256: []byte{3}, FileLength: 9})
	if err != nil {
		t.Fatal(err)
	}
	im := img.GetImageMessage()
	if im.GetCaption() != "cap" || im.GetMimetype() != "image/png" || im.GetDirectPath() != "/v/t62/abc.enc?oh=1" ||
		im.GetFileLength() != 9 || !im.GetContextInfo().GetIsForwarded() {
		t.Fatalf("image forward: %v", im)
	}
	voice, _ := forwardContent(store.Message{MediaType: "audio", Filename: "audio_X.ogg", DirectPath: "/p", MediaKey: []byte{1}, FileEncSHA256: []byte{2}})
	if !voice.GetAudioMessage().GetPTT() {
		t.Fatal("ogg audio should forward as a voice note")
	}
	for _, m := range []store.Message{{}, {Content: "[deleted]"}, {MediaType: "image", Filename: "x.jpg"}} {
		if _, err := forwardContent(m); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("forwardContent(%+v) should fail, got %v", m, err)
		}
	}
}
