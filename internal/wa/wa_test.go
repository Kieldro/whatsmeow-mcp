package wa

import (
	"reflect"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractDirectPathFromURLKeepsQuery(t *testing.T) {
	url := "https://mmg.whatsapp.net/v/t62.7117-24/11111111_222222222_3333333333_n.enc?ccb=11-4&oh=01_SIG&oe=ABCDEF01&_nc_sid=5e03e0"
	want := "/v/t62.7117-24/11111111_222222222_3333333333_n.enc?ccb=11-4&oh=01_SIG&oe=ABCDEF01&_nc_sid=5e03e0"
	if got := ExtractDirectPathFromURL(url); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := ExtractDirectPathFromURL("not-a-url"); got != "not-a-url" {
		t.Fatalf("unparseable URL should be returned unchanged, got %q", got)
	}
}

func TestMapPollSelection(t *testing.T) {
	opts := []string{"Thursday", "Friday", "Saturday"}
	hashes := whatsmeow.HashPollOptions(opts)
	// Selection order differs from poll order; output follows poll order.
	got := MapPollSelection(opts, [][]byte{hashes[2], hashes[0]})
	if want := []string{"Thursday", "Saturday"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := MapPollSelection(opts, nil); len(got) != 0 || got == nil {
		t.Fatalf("empty vote should map to empty non-nil slice, got %#v", got)
	}
	if got := MapPollSelection(opts, [][]byte{[]byte("unknown-hash")}); len(got) != 0 {
		t.Fatalf("unknown hash should be ignored, got %v", got)
	}
}

func TestExtractText(t *testing.T) {
	cases := []struct {
		msg  *waE2E.Message
		want string
	}{
		{&waE2E.Message{Conversation: proto.String("hi")}, "hi"},
		{&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("reply")}}, "reply"},
		{&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("cap")}}, "cap"},
		{&waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("Q?"),
			Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("a")}, {OptionName: proto.String("b")}}}}, "[poll] Q? (a / b)"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := extractText(c.msg); got != c.want {
			t.Errorf("extractText = %q, want %q", got, c.want)
		}
	}
}

func TestExtractMediaFilename(t *testing.T) {
	m := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{FileName: proto.String("../../etc/passwd")}}
	mi, ok := extractMedia(m, "ABC")
	if !ok || mi.Type != "document" {
		t.Fatalf("got %+v %v", mi, ok)
	}
	if mi.Filename != "_.._etc_passwd" {
		t.Fatalf("filename not sanitized: %q", mi.Filename)
	}
	img, _ := extractMedia(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/png")}}, "ID1")
	if img.Filename != "image_ID1.png" {
		t.Fatalf("got %q", img.Filename)
	}
}

func TestMediaKind(t *testing.T) {
	cases := map[string]whatsmeow.MediaType{
		"a.JPG": whatsmeow.MediaImage, "b.ogg": whatsmeow.MediaAudio, "c.mp4": whatsmeow.MediaVideo, "d.zip": whatsmeow.MediaDocument,
	}
	for f, want := range cases {
		if got, _ := mediaKind(f); got != want {
			t.Errorf("mediaKind(%s) = %v, want %v", f, got, want)
		}
	}
}
