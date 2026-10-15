package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// httpRPC is a minimal Streamable HTTP MCP client.
type httpRPC struct {
	t       *testing.T
	url     string
	session string
	id      int
}

func (c *httpRPC) post(body map[string]any) *http.Response {
	c.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
		req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" {
		c.session = s
	}
	return resp
}

func (c *httpRPC) notify(method string, params any) {
	c.t.Helper()
	resp := c.post(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		c.t.Fatalf("%s: HTTP %d", method, resp.StatusCode)
	}
}

func (c *httpRPC) call(method string, params any) json.RawMessage {
	c.t.Helper()
	c.id++
	resp := c.post(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		c.t.Fatalf("%s: HTTP %d: %s", method, resp.StatusCode, b)
	}
	// The body is either one JSON object or an SSE stream of "data:" lines.
	var payloads [][]byte
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
				payloads = append(payloads, []byte(strings.TrimSpace(d)))
			}
		}
	} else {
		b, _ := io.ReadAll(resp.Body)
		payloads = append(payloads, b)
	}
	for _, p := range payloads {
		var r struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(p, &r) != nil || r.ID != c.id {
			continue
		}
		if r.Error != nil {
			c.t.Fatalf("%s: error %s", method, r.Error)
		}
		return r.Result
	}
	c.t.Fatalf("%s: no response in %q", method, payloads)
	return nil
}

// TestMCPHTTPSmoke starts the binary as an offline daemon (no stdio MCP) and
// speaks MCP over Streamable HTTP.
func TestMCPHTTPSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "whatsmeow-mcp")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	cmd := exec.Command(bin, "--no-connect", "--no-mcp", "--mcp-http", addr, "--data-dir", filepath.Join(tmp, "data"))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.Process.Signal(os.Interrupt) != nil {
			cmd.Process.Kill() // no SIGINT on this platform
		}
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			t.Error("server did not exit on interrupt")
		}
	})

	// Wait for the listener.
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			break
		}
		select {
		case err := <-exited:
			t.Fatalf("server exited early: %v\n%s", err, stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not listen on %s\n%s", addr, stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	c := &httpRPC{t: t, url: "http://" + addr + "/mcp"}
	var init struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	json.Unmarshal(c.call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "http-smoke-test", "version": "0"},
	}), &init)
	if init.ServerInfo.Name != "whatsmeow-mcp" {
		t.Fatalf("serverInfo = %+v", init)
	}
	if c.session == "" {
		t.Fatal("no Mcp-Session-Id returned by initialize")
	}
	c.notify("notifications/initialized", map[string]any{})

	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	json.Unmarshal(c.call("tools/list", map[string]any{}), &list)
	var names []string
	for _, tl := range list.Tools {
		names = append(names, tl.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(expectedTools, ",") {
		t.Fatalf("tools mismatch\n got: %v\nwant: %v", names, expectedTools)
	}
	t.Logf("tools/list over HTTP returned %d tools", len(names))

	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(c.call("tools/call", map[string]any{"name": "connection_status", "arguments": map[string]any{}}), &r)
	if r.IsError || len(r.Content) == 0 || !strings.Contains(r.Content[0].Text, `"offline": true`) {
		t.Fatalf("connection_status over HTTP: %+v", r)
	}
}
