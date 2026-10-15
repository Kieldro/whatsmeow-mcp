package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/Kieldro/whatsmeow-mcp/internal/wa"
)

func TestCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, o := range All() {
		if seen[o.Name] {
			t.Errorf("duplicate op %s", o.Name)
		}
		seen[o.Name] = true
		if o.Title == "" || len(o.Description) < 20 {
			t.Errorf("%s needs a title and a real description", o.Name)
		}
		if o.Kind == Destructive && !strings.Contains(o.Description, "Confirm") && !strings.Contains(o.Description, "admin") &&
			!strings.Contains(o.Description, "DESTRUCTIVE") && !strings.Contains(o.Description, "notified") {
			t.Errorf("destructive op %s should warn in its description", o.Name)
		}
		tl := o.Tool()
		if tl.Annotations.ReadOnlyHint != (o.Kind == Read) {
			t.Errorf("%s: wrong read-only hint", o.Name)
		}
		if o.Kind != Read && (tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint != (o.Kind == Destructive)) {
			t.Errorf("%s: wrong destructive hint", o.Name)
		}
		if err := o.Validate(nil); err != nil && !IsInvalidInput(err) {
			t.Errorf("%s: schema does not resolve: %v", o.Name, err)
		}
	}
	for _, name := range []string{"leave_group", "block_contact", "join_group_with_link", "set_privacy_setting", "delete_message_for_me"} {
		found := false
		for _, o := range All() {
			if o.Name == name {
				found = o.Kind == Destructive
			}
		}
		if !found {
			t.Errorf("%s must exist and be marked destructive", name)
		}
	}
}

func find(t *testing.T, name string) Op {
	for _, o := range All() {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("no op %s", name)
	return Op{}
}

func TestValidate(t *testing.T) {
	loc := find(t, "send_location")
	if err := loc.Validate([]byte(`{"recipient":"+1 555 0100","latitude":1.5,"longitude":2}`)); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	for _, body := range []string{`{}`, `{"recipient":"+1 555 0100","latitude":"north","longitude":2}`, `not json`} {
		if err := loc.Validate([]byte(body)); !IsInvalidInput(err) {
			t.Errorf("Validate(%s) should be invalid input, got %v", body, err)
		}
	}
	if err := find(t, "list_blocklist").Validate(nil); err != nil {
		t.Errorf("no-input op with empty body: %v", err)
	}
}

func TestCallOffline(t *testing.T) {
	svc, err := wa.New(context.Background(), wa.Options{DataDir: t.TempDir(), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	// Input errors are reported before connectivity.
	_, err = find(t, "mute_chat").Call(context.Background(), svc, []byte(`{"chat_jid":"15550100","duration":"soon"}`))
	if !IsInvalidInput(err) {
		t.Fatalf("bad duration: %v", err)
	}
	_, err = find(t, "send_contact_card").Call(context.Background(), svc, []byte(`{"recipient":"15550100","name":"A","phone":"+1 555 0100"}`))
	if err == nil || IsInvalidInput(err) || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("offline send should fail with not connected: %v", err)
	}
	// Local-store reads work offline.
	out, err := find(t, "list_statuses").Call(context.Background(), svc, nil)
	if err != nil {
		t.Fatalf("list_statuses offline: %v", err)
	}
	if v, ok := out.([]wa.MessageView); !ok || len(v) != 0 {
		t.Fatalf("list_statuses on empty store = %#v", out)
	}
	ls, err := find(t, "get_last_seen").Call(context.Background(), svc, []byte(`{"jid":"+1 555 0100"}`))
	if err != nil || ls.(wa.LastSeen).Known {
		t.Fatalf("get_last_seen without updates: %+v %v", ls, err)
	}
}
