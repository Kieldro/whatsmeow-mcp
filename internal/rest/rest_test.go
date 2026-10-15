package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kieldro/whatsmeow-mcp/internal/ops"
	"github.com/Kieldro/whatsmeow-mcp/internal/wa"
)

func TestOfflineEndpoints(t *testing.T) {
	svc, err := wa.New(context.Background(), wa.Options{DataDir: t.TempDir(), Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	srv := httptest.NewServer(Handler(svc))
	defer srv.Close()

	post := func(path, body string) (int, map[string]any) {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, out := post("/api/send", `{"recipient":"15550100","message":"hi"}`); code != 500 || out["success"] != false ||
		!strings.Contains(out["message"].(string), "not connected") {
		t.Fatalf("send offline: %d %v", code, out)
	}
	if code, _ := post("/api/send", `{"recipient":""}`); code != 400 {
		t.Fatalf("missing recipient should be 400, got %d", code)
	}
	if code, _ := post("/api/react", `not json`); code != 400 {
		t.Fatalf("bad JSON should be 400, got %d", code)
	}
	if code, out := post("/api/poll_results", `{"id":"nope"}`); code != 500 || !strings.Contains(out["message"].(string), "unknown poll") {
		t.Fatalf("poll_results: %d %v", code, out)
	}
	resp, err := http.Get(srv.URL + "/api/send")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/send = %d", resp.StatusCode)
	}
	// Shared ops are served at /api/<tool name>.
	if code, out := post("/api/send_location", `{"recipient":"15550100","latitude":40.7,"longitude":-74}`); code != 500 ||
		!strings.Contains(out["message"].(string), "not connected") {
		t.Fatalf("send_location offline: %d %v", code, out)
	}
	if code, out := post("/api/send_location", `{"recipient":"15550100"}`); code != 400 || out["success"] != false {
		t.Fatalf("missing required fields should be 400: %d %v", code, out)
	}
	if code, _ := post("/api/mute_chat", `{"chat_jid":"15550100","duration":"soon"}`); code != 400 {
		t.Fatalf("bad duration should be 400, got %d", code)
	}
	if code, out := post("/api/list_statuses", ``); code != 200 || out["success"] != true {
		t.Fatalf("list_statuses: %d %v", code, out)
	}
	for _, op := range ops.All() {
		resp, err := http.Get(srv.URL + "/api/" + op.Name)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("/api/%s: GET = %d, want 405 (registered, POST only)", op.Name, resp.StatusCode)
		}
	}
	for _, p := range []string{"/api/edit", "/api/reply", "/api/poll", "/api/groups", "/api/group_info", "/api/group_participants",
		"/api/mark_read", "/api/revoke", "/api/send_pinned", "/api/create_group", "/api/rename_group", "/api/history", "/api/download"} {
		resp, err := http.Post(srv.URL+p, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("%s not registered", p)
		}
	}
}
