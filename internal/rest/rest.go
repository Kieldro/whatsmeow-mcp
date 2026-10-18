// Package rest serves the optional HTTP API that mirrors the original
// whatsapp-mcp bridge endpoints, so existing scripts keep working.
package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Kieldro/whatsmeow-mcp/internal/ops"
	"github.com/Kieldro/whatsmeow-mcp/internal/wa"
)

// Handler returns an http.Handler with every /api endpoint.
func Handler(svc *wa.Service) http.Handler {
	mux := http.NewServeMux()

	post := func(path string, newReq func() any, fn func(ctx context.Context, r *http.Request, req any) (map[string]any, int)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method != http.MethodPost {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "POST only"})
				return
			}
			req := newReq()
			if req != nil && r.ContentLength != 0 {
				if err := json.NewDecoder(r.Body).Decode(req); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "invalid JSON: " + err.Error()})
					return
				}
			}
			out, code := fn(r.Context(), r, req)
			writeJSON(w, code, out)
		})
	}
	fail := func(err error) (map[string]any, int) {
		return map[string]any{"success": false, "message": err.Error(), "error": err.Error()}, http.StatusInternalServerError
	}
	bad := func(msg string) (map[string]any, int) {
		return map[string]any{"success": false, "message": msg, "error": msg}, http.StatusBadRequest
	}
	okResp := func(extra map[string]any) (map[string]any, int) {
		out := map[string]any{"success": true}
		for k, v := range extra {
			out[k] = v
		}
		return out, http.StatusOK
	}

	type sendReq struct {
		Recipient string   `json:"recipient"`
		Message   string   `json:"message"`
		MediaPath string   `json:"media_path"`
		ReplyTo   string   `json:"reply_to"`
		Mentions  []string `json:"mentions"`
	}
	post("/api/send", func() any { return &sendReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*sendReq)
		if req.Recipient == "" || (req.Message == "" && req.MediaPath == "") {
			return bad("recipient and message or media_path are required")
		}
		var s wa.Sent
		var err error
		if req.MediaPath != "" {
			s, err = svc.SendFile(ctx, req.Recipient, req.MediaPath, req.Message)
		} else {
			s, err = svc.SendText(ctx, req.Recipient, req.Message, req.ReplyTo, req.Mentions...)
		}
		if err != nil {
			return fail(err)
		}
		return okResp(sentResp(s, map[string]any{"message": "Message sent to " + req.Recipient}))
	})

	type pinnedReq struct {
		Recipient string `json:"recipient"`
		Message   string `json:"message"`
		Days      int    `json:"days"`
	}
	post("/api/send_pinned", func() any { return &pinnedReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*pinnedReq)
		if req.Recipient == "" || req.Message == "" {
			return bad("recipient and message are required")
		}
		s, err := svc.SendText(ctx, req.Recipient, req.Message, "")
		if err != nil {
			return fail(err)
		}
		days := req.Days
		if days == 0 {
			days = 30
		}
		if err := svc.Pin(ctx, s.ChatJID, s.ID, days, false); err != nil {
			return fail(fmt.Errorf("sent %s but pin failed: %w", s.ID, err))
		}
		return okResp(sentResp(s, nil))
	})

	type replyReq struct {
		ChatJID string `json:"chat_jid"`
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	post("/api/reply", func() any { return &replyReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*replyReq)
		if req.ChatJID == "" || req.ID == "" || req.Message == "" {
			return bad("POST {chat_jid, id, message}")
		}
		s, err := svc.SendText(ctx, req.ChatJID, req.Message, req.ID)
		if err != nil {
			return fail(err)
		}
		return okResp(sentResp(s, nil))
	})

	post("/api/edit", func() any { return &replyReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*replyReq)
		if req.ChatJID == "" || req.ID == "" || req.Message == "" {
			return bad("POST {chat_jid, id, message}")
		}
		if _, err := svc.Edit(ctx, req.ChatJID, req.ID, req.Message); err != nil {
			return fail(err)
		}
		return okResp(nil)
	})

	type reactReq struct {
		ChatJID string `json:"chat_jid"`
		ID      string `json:"id"`
		Emoji   string `json:"emoji"`
	}
	post("/api/react", func() any { return &reactReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*reactReq)
		if req.ChatJID == "" || req.ID == "" {
			return bad("POST {chat_jid, id, emoji}")
		}
		if err := svc.React(ctx, req.ChatJID, req.ID, req.Emoji); err != nil {
			return fail(err)
		}
		return okResp(nil)
	})

	type revokeReq struct {
		ChatJID string `json:"chat_jid"`
		ID      string `json:"id"`
		Sender  string `json:"sender"`
	}
	post("/api/revoke", func() any { return &revokeReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*revokeReq)
		if req.ChatJID == "" || req.ID == "" {
			return bad("POST {chat_jid, id}")
		}
		if err := svc.Revoke(ctx, req.ChatJID, req.ID, req.Sender); err != nil {
			return fail(err)
		}
		return okResp(nil)
	})

	type pollReq struct {
		Recipient string   `json:"recipient"`
		Question  string   `json:"question"`
		Options   []string `json:"options"`
		Multi     bool     `json:"multi"`
	}
	post("/api/poll", func() any { return &pollReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*pollReq)
		if req.Recipient == "" || req.Question == "" || len(req.Options) < 2 {
			return bad("POST {recipient, question, options, multi}")
		}
		s, err := svc.CreatePoll(ctx, req.Recipient, req.Question, req.Options, req.Multi)
		if err != nil {
			return fail(err)
		}
		return okResp(sentResp(s, nil))
	})

	type idReq struct {
		ID string `json:"id"`
	}
	post("/api/poll_results", func() any { return &idReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*idReq)
		if req.ID == "" {
			return bad("POST {id}")
		}
		res, err := svc.PollResults(ctx, req.ID)
		if err != nil {
			return fail(err)
		}
		return okResp(map[string]any{"question": res.Question, "votes": res.Votes, "voter_names": res.Voters})
	})

	mux.HandleFunc("/api/groups", func(w http.ResponseWriter, r *http.Request) {
		g, err := svc.ListGroups(r.Context())
		if err != nil {
			out, code := fail(err)
			writeJSON(w, code, out)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "groups": g})
	})

	type jidReq struct {
		JID  string `json:"jid"`
		Name string `json:"name"`
	}
	post("/api/group_info", func() any { return &jidReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*jidReq)
		if req.JID == "" {
			return bad("POST {jid}")
		}
		g, err := svc.GroupInfo(ctx, req.JID)
		if err != nil {
			return fail(err)
		}
		return okResp(map[string]any{"name": g.Name, "count": g.Count, "participants": g.Participants})
	})

	post("/api/rename_group", func() any { return &jidReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*jidReq)
		if req.JID == "" || req.Name == "" {
			return bad("jid and name are required")
		}
		if err := svc.RenameGroup(ctx, req.JID, req.Name); err != nil {
			return fail(err)
		}
		return okResp(nil)
	})

	type participantsReq struct {
		JID          string   `json:"jid"`
		Action       string   `json:"action"`
		Participants []string `json:"participants"`
	}
	post("/api/group_participants", func() any { return &participantsReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*participantsReq)
		if req.JID == "" || len(req.Participants) == 0 {
			return bad("POST {jid, action, participants}")
		}
		res, err := svc.UpdateParticipants(ctx, req.JID, req.Action, req.Participants)
		if err != nil {
			return fail(err)
		}
		return okResp(map[string]any{"results": res})
	})

	type createGroupReq struct {
		Name         string   `json:"name"`
		Participants []string `json:"participants"`
	}
	post("/api/create_group", func() any { return &createGroupReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*createGroupReq)
		if req.Name == "" || len(req.Participants) == 0 {
			return bad("name and participants are required")
		}
		g, err := svc.CreateGroup(ctx, req.Name, req.Participants)
		if err != nil {
			return fail(err)
		}
		return okResp(map[string]any{"jid": g.JID, "name": g.Name, "participants": g.Members})
	})

	type markReadReq struct {
		ChatJID string   `json:"chat_jid"`
		IDs     []string `json:"ids"`
		Sender  string   `json:"sender"`
	}
	post("/api/mark_read", func() any { return &markReadReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*markReadReq)
		if req.ChatJID == "" || len(req.IDs) == 0 {
			return bad("POST {chat_jid, ids, sender}")
		}
		if err := svc.MarkRead(ctx, req.ChatJID, req.IDs, req.Sender); err != nil {
			return fail(err)
		}
		return okResp(nil)
	})

	type historyReq struct {
		ChatJID string `json:"chat_jid"`
		Count   int    `json:"count"`
		Mode    string `json:"mode"`
	}
	post("/api/history", func() any { return &historyReq{} }, func(ctx context.Context, hr *http.Request, r any) (map[string]any, int) {
		req := r.(*historyReq)
		if req.ChatJID == "" {
			return bad("POST {chat_jid, count}")
		}
		// Matches the original bridge: by default re-fetch rows stored without
		// a sender; ?latest=1 asks for the newest batch.
		mode := wa.AnchorMissingSender
		switch {
		case hr.URL.Query().Get("latest") == "1" || req.Mode == "latest":
			mode = wa.AnchorLatest
		case req.Mode == "older":
			mode = wa.AnchorOldest
		}
		if req.Count <= 0 || req.Count > 500 {
			req.Count = 100
		}
		res, err := svc.RequestHistory(ctx, req.ChatJID, req.Count, mode)
		if err != nil {
			return fail(err)
		}
		return okResp(map[string]any{"anchor": res.Anchor, "chat_jid": res.ChatJID})
	})

	type downloadReq struct {
		MessageID string `json:"message_id"`
		ChatJID   string `json:"chat_jid"`
	}
	post("/api/download", func() any { return &downloadReq{} }, func(ctx context.Context, _ *http.Request, r any) (map[string]any, int) {
		req := r.(*downloadReq)
		if req.MessageID == "" || req.ChatJID == "" {
			return bad("Message ID and Chat JID are required")
		}
		d, err := svc.DownloadMedia(ctx, req.ChatJID, req.MessageID)
		if err != nil {
			return fail(fmt.Errorf("Failed to download media: %w", err))
		}
		return okResp(map[string]any{"message": "Successfully downloaded " + d.MediaType + " media", "filename": d.Filename, "path": d.Path})
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, svc.Status())
	})

	// Every shared operation is also POST /api/<tool name>, taking the same
	// JSON arguments as the MCP tool and answering {"success": true, "result": …}.
	for _, op := range ops.All() {
		mux.HandleFunc("/api/"+op.Name, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "POST only"})
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": err.Error(), "error": err.Error()})
				return
			}
			out, err := op.Call(r.Context(), svc, body)
			if err != nil {
				code := http.StatusInternalServerError
				if ops.IsInvalidInput(err) {
					code = http.StatusBadRequest
				}
				writeJSON(w, code, map[string]any{"success": false, "message": err.Error(), "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true, "result": out})
		})
	}
	return mux
}

// sentResp builds the response fields for a sent message: id, chat_jid and
// the recipient identity-change fields, plus extra.
func sentResp(s wa.Sent, extra map[string]any) map[string]any {
	out := s.Fields()
	out["id"], out["chat_jid"] = s.ID, s.ChatJID
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
