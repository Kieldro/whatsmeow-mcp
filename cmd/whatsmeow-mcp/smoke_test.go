package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// expectedTools is every MCP tool, sorted (68).
var expectedTools = []string{
	"archive_chat", "block_contact", "connection_status", "create_group", "create_poll", "delete_message",
	"delete_message_for_me", "download_media", "edit_message", "follow_newsletter", "forward_message",
	"get_chat", "get_group_info_from_link", "get_group_invite_link", "get_last_seen", "get_message_context",
	"get_privacy_settings", "get_profile_picture", "get_user_info", "group_info", "is_on_whatsapp",
	"join_group_with_link", "leave_group", "list_blocklist", "list_chats", "list_communities",
	"list_community_groups", "list_group_join_requests", "list_groups", "list_messages", "list_newsletters",
	"list_statuses", "mark_chat_read", "mark_read", "mute_chat", "newsletter_info", "newsletter_messages",
	"pin_chat", "pin_message", "poll_results", "post_media_status", "post_text_status", "react", "rename_group",
	"request_history", "search_contacts", "send_audio_message", "send_chat_presence", "send_contact_card",
	"send_file", "send_location", "send_message", "send_presence", "send_sticker", "set_disappearing_timer",
	"set_group_announce", "set_group_description", "set_group_locked", "set_group_photo", "set_privacy_setting",
	"set_profile_picture", "set_status_message", "star_message", "subscribe_presence", "unblock_contact",
	"unfollow_newsletter", "update_group_join_requests", "update_group_participants",
}

type rpcClient struct {
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Scanner
	id  int
}

func (c *rpcClient) notify(method string, params any) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

func (c *rpcClient) call(method string, params any) json.RawMessage {
	c.t.Helper()
	c.id++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
	for c.out.Scan() {
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		line := c.out.Bytes()
		if err := json.Unmarshal(line, &resp); err != nil {
			c.t.Fatalf("stdout is not JSON-RPC (logs must go to stderr): %q", line)
		}
		if resp.ID != c.id {
			continue // notification
		}
		if resp.Error != nil {
			c.t.Fatalf("%s: error %s", method, resp.Error)
		}
		return resp.Result
	}
	c.t.Fatalf("%s: no response (%v)", method, c.out.Err())
	return nil
}

// TestMCPStdioSmoke builds the binary, starts it offline with an empty data
// dir, and speaks MCP over stdio.
func TestMCPStdioSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "whatsmeow-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dataDir := filepath.Join(tmp, "data")
	cmd := exec.Command(bin, "--no-connect", "--data-dir", dataDir)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			t.Error("server did not exit after stdin closed")
		}
	})
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	c := &rpcClient{t: t, in: stdin, out: sc}

	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	json.Unmarshal(c.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "smoke-test", "version": "0"},
	}), &init)
	if init.ServerInfo.Name != "whatsmeow-mcp" {
		t.Fatalf("serverInfo = %+v", init)
	}
	c.notify("notifications/initialized", map[string]any{})

	var list struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	json.Unmarshal(c.call("tools/list", map[string]any{}), &list)
	var names []string
	for _, tl := range list.Tools {
		names = append(names, tl.Name)
		if tl.Description == "" || !strings.Contains(string(tl.InputSchema), `"type":"object"`) {
			t.Errorf("tool %s lacks a description or object schema", tl.Name)
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(expectedTools, ",") {
		t.Fatalf("tools mismatch\n got: %v\nwant: %v", names, expectedTools)
	}
	t.Logf("tools/list returned %d tools", len(names))

	type callResult struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	callTool := func(name string, args map[string]any) callResult {
		var r callResult
		json.Unmarshal(c.call("tools/call", map[string]any{"name": name, "arguments": args}), &r)
		return r
	}
	if r := callTool("list_chats", map[string]any{}); r.IsError || len(r.Content) == 0 {
		t.Fatalf("list_chats on empty store: %+v", r)
	}
	if r := callTool("list_messages", map[string]any{"query": "x", "after": "2026-01-01"}); r.IsError {
		t.Fatalf("list_messages: %+v", r)
	}
	if r := callTool("send_message", map[string]any{"recipient": "+1 555 0100", "message": "hi"}); !r.IsError ||
		!strings.Contains(r.Content[0].Text, "not connected") {
		t.Fatalf("send_message offline should fail cleanly: %+v", r)
	}
	if r := callTool("send_message", map[string]any{"recipient": "+1 555 0100"}); !r.IsError {
		t.Fatalf("missing required arg should be rejected: %+v", r)
	}
	if r := callTool("send_location", map[string]any{"recipient": "+1 555 0100", "latitude": 40.7, "longitude": -74.0}); !r.IsError ||
		!strings.Contains(r.Content[0].Text, "not connected") {
		t.Fatalf("send_location offline should fail cleanly: %+v", r)
	}
	if r := callTool("set_disappearing_timer", map[string]any{"chat_jid": "+1 555 0100", "timer": "3 days"}); !r.IsError ||
		!strings.Contains(r.Content[0].Text, "off, 24h, 7d or 90d") {
		t.Fatalf("bad timer should be rejected before connecting: %+v", r)
	}
	if r := callTool("list_statuses", map[string]any{}); r.IsError {
		t.Fatalf("list_statuses offline: %+v", r)
	}
	if r := callTool("connection_status", map[string]any{}); r.IsError || !strings.Contains(r.Content[0].Text, `"offline": true`) {
		t.Fatalf("connection_status: %+v", r)
	}
	for _, f := range []string{"whatsapp.db", "messages.db"} {
		if _, err := os.Stat(filepath.Join(dataDir, f)); err != nil {
			t.Errorf("expected %s in data dir: %v", f, err)
		}
	}
}
