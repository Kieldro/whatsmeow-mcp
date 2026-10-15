// Package mcpserver exposes the WhatsApp service as MCP tools.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Kieldro/whatsmeow-mcp/internal/store"
	"github.com/Kieldro/whatsmeow-mcp/internal/wa"
)

// result packs v as pretty JSON text content.
func result(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

func ok(extra map[string]any) (*mcp.CallToolResult, any, error) {
	out := map[string]any{"success": true}
	for k, v := range extra {
		out[k] = v
	}
	return result(out)
}

func parseTime(name, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%s: %q is not an ISO-8601 date/time", name, s)
}

// Tool input types. Fields without omitempty are required.

type searchContactsIn struct {
	Query string `json:"query" jsonschema:"Name or phone-number fragment to search for"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum results (default 50)"`
}

type listChatsIn struct {
	Query         string `json:"query,omitempty" jsonschema:"Filter by chat name or JID substring"`
	Kind          string `json:"kind,omitempty" jsonschema:"all (default), groups or direct"`
	SortBy        string `json:"sort_by,omitempty" jsonschema:"last_active (default) or name"`
	IncludeStatus bool   `json:"include_status,omitempty" jsonschema:"Include the WhatsApp Status pseudo-chat (status@broadcast)"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Page size (default 20)"`
	Page          int    `json:"page,omitempty" jsonschema:"Zero-based page number"`
}

type getChatIn struct {
	Chat string `json:"chat" jsonschema:"Chat JID, phone number, or exact chat name"`
}

type listMessagesIn struct {
	Chat          string `json:"chat,omitempty" jsonschema:"Only this chat: JID, phone number or chat name. A contact's phone-number chat and LID chat are both searched"`
	Sender        string `json:"sender,omitempty" jsonschema:"Only messages from this sender (phone number or JID; LIDs are matched too)"`
	Query         string `json:"query,omitempty" jsonschema:"Case-insensitive text search in message content and captions"`
	After         string `json:"after,omitempty" jsonschema:"ISO-8601 date/time; only messages after this (local time if no offset)"`
	Before        string `json:"before,omitempty" jsonschema:"ISO-8601 date/time; only messages before this"`
	OnlyMedia     bool   `json:"only_media,omitempty" jsonschema:"Only messages with an attachment"`
	IncludeStatus bool   `json:"include_status,omitempty" jsonschema:"Include WhatsApp Status/stories (status@broadcast), excluded by default"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Page size (default 20, max 500)"`
	Page          int    `json:"page,omitempty" jsonschema:"Zero-based page number"`
}

type messageContextIn struct {
	MessageID string `json:"message_id" jsonschema:"ID of the message"`
	Chat      string `json:"chat,omitempty" jsonschema:"Chat JID (recommended; IDs are only unique per chat)"`
	Before    int    `json:"before,omitempty" jsonschema:"Messages to include before (default 5)"`
	After     int    `json:"after,omitempty" jsonschema:"Messages to include after (default 5)"`
}

type sendMessageIn struct {
	Recipient string `json:"recipient" jsonschema:"Phone number with country code (e.g. +1 555 0100) or a JID (…@s.whatsapp.net, …@lid, …@g.us)"`
	Message   string `json:"message" jsonschema:"Text to send"`
	ReplyTo   string `json:"reply_to,omitempty" jsonschema:"Optional message ID to quote-reply to (must be in the same chat)"`
}

type sendFileIn struct {
	Recipient string `json:"recipient" jsonschema:"Phone number with country code or a JID"`
	MediaPath string `json:"media_path" jsonschema:"Absolute path of the file to send. Images/videos are sent as media, .ogg/.opus as a voice note, anything else as a document"`
	Caption   string `json:"caption,omitempty" jsonschema:"Optional caption for images, videos and documents"`
}

type sendAudioIn struct {
	Recipient string `json:"recipient" jsonschema:"Phone number with country code or a JID"`
	MediaPath string `json:"media_path" jsonschema:"Absolute path of an audio file; non-Ogg/Opus files are converted with ffmpeg"`
}

type downloadMediaIn struct {
	MessageID string `json:"message_id" jsonschema:"ID of the message with the attachment"`
	ChatJID   string `json:"chat_jid,omitempty" jsonschema:"Chat JID of the message (recommended)"`
}

type editMessageIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"Chat JID"`
	MessageID string `json:"message_id" jsonschema:"ID of a text message you sent"`
	Message   string `json:"message" jsonschema:"Replacement text"`
}

type deleteMessageIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"Chat JID"`
	MessageID string `json:"message_id" jsonschema:"ID of the message to delete for everyone"`
	Sender    string `json:"sender,omitempty" jsonschema:"Only for group admins deleting someone else's message: that sender's JID"`
}

type reactIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"Chat JID"`
	MessageID string `json:"message_id" jsonschema:"ID of the message to react to"`
	Emoji     string `json:"emoji,omitempty" jsonschema:"Emoji to react with; empty removes your reaction"`
}

type createPollIn struct {
	Recipient string   `json:"recipient" jsonschema:"Phone number with country code or a JID (usually a group)"`
	Question  string   `json:"question" jsonschema:"Poll question"`
	Options   []string `json:"options" jsonschema:"2-12 answer options"`
	Multi     bool     `json:"multi,omitempty" jsonschema:"Allow selecting more than one option"`
}

type pollResultsIn struct {
	PollID string `json:"poll_id" jsonschema:"Message ID of the poll"`
}

type groupIn struct {
	GroupJID string `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
}

type updateParticipantsIn struct {
	GroupJID     string   `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
	Action       string   `json:"action" jsonschema:"add, remove, promote or demote"`
	Participants []string `json:"participants" jsonschema:"Phone numbers with country code or JIDs"`
}

type createGroupIn struct {
	Name         string   `json:"name" jsonschema:"Group subject"`
	Participants []string `json:"participants" jsonschema:"Phone numbers with country code or JIDs to add"`
}

type renameGroupIn struct {
	GroupJID string `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
	Name     string `json:"name" jsonschema:"New subject"`
}

type markReadIn struct {
	ChatJID    string   `json:"chat_jid" jsonschema:"Chat JID"`
	MessageIDs []string `json:"message_ids" jsonschema:"IDs of the messages to mark as read"`
	Sender     string   `json:"sender,omitempty" jsonschema:"Groups only: sender of the messages (looked up automatically if omitted)"`
}

type pinIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"Chat JID"`
	MessageID string `json:"message_id" jsonschema:"ID of the message to pin"`
	Days      int    `json:"days,omitempty" jsonschema:"Pin duration: 1, 7 or 30 days (default 7; rounded up)"`
	Unpin     bool   `json:"unpin,omitempty" jsonschema:"Unpin instead of pin"`
}

type requestHistoryIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"Chat JID to backfill"`
	Count   int    `json:"count,omitempty" jsonschema:"Messages to request (default 50, max 500)"`
	Mode    string `json:"mode,omitempty" jsonschema:"older (default): before the oldest stored message; latest: the most recent messages; missing_sender: re-fetch group rows stored without a sender"`
}

type empty struct{}

// New builds the MCP server with every tool registered.
func New(svc *wa.Service, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "whatsmeow-mcp", Title: "WhatsApp (whatsmeow)", Version: version}, &mcp.ServerOptions{
		Instructions: "Personal WhatsApp access through a linked device. Reads come from a local history database; " +
			"sending acts as the user, so confirm message text and recipient with the user before sending. " +
			"Use search_contacts or list_chats to find JIDs; list_messages accepts a phone number, JID or chat name.",
	})
	tool := func(name, title, desc string, readOnly bool) *mcp.Tool {
		t := &mcp.Tool{Name: name, Title: title, Description: desc, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly, Title: title}}
		if !readOnly {
			f := false
			t.Annotations.DestructiveHint = &f
		}
		return t
	}
	destructive := func(t *mcp.Tool) *mcp.Tool {
		yes := true
		t.Annotations.DestructiveHint = &yes
		return t
	}

	mcp.AddTool(srv, tool("search_contacts", "Search contacts",
		"Search WhatsApp contacts by name or phone number across stored chats and the synced address book. Returns name, phone, LID and the chat JID to use with list_messages/send_message.", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, in searchContactsIn) (*mcp.CallToolResult, any, error) {
			c, err := svc.SearchContacts(ctx, in.Query, in.Limit)
			if err != nil {
				return nil, nil, err
			}
			return result(c)
		})

	mcp.AddTool(srv, tool("list_chats", "List chats",
		"List WhatsApp chats with their last message, most recently active first. Status/stories are excluded unless include_status is set.", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, in listChatsIn) (*mcp.CallToolResult, any, error) {
			f := store.ChatFilter{Query: in.Query, IncludeStatus: in.IncludeStatus, SortByName: in.SortBy == "name", Limit: in.Limit}
			switch in.Kind {
			case "groups":
				f.OnlyGroups = true
			case "direct":
				f.OnlyDirect = true
			case "", "all":
			default:
				return nil, nil, fmt.Errorf("kind must be all, groups or direct")
			}
			if f.Limit <= 0 {
				f.Limit = 20
			}
			f.Offset = max(in.Page, 0) * f.Limit
			c, err := svc.ListChats(ctx, f)
			if err != nil {
				return nil, nil, err
			}
			return result(c)
		})

	mcp.AddTool(srv, tool("get_chat", "Get chat",
		"Get one chat's metadata and last message by JID, phone number or exact name.", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, in getChatIn) (*mcp.CallToolResult, any, error) {
			c, err := svc.GetChat(ctx, in.Chat)
			if err != nil {
				return nil, nil, err
			}
			return result(c)
		})

	mcp.AddTool(srv, tool("list_messages", "List messages",
		"List stored WhatsApp messages, newest first, with sender phone and name resolved (LID senders are mapped to phone numbers). "+
			"Filter by chat, sender, text query and date range. WhatsApp Status posts (status@broadcast) are excluded unless include_status is true. "+
			"Media messages show media_type; fetch the file with download_media.", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, in listMessagesIn) (*mcp.CallToolResult, any, error) {
			after, err := parseTime("after", in.After)
			if err != nil {
				return nil, nil, err
			}
			before, err := parseTime("before", in.Before)
			if err != nil {
				return nil, nil, err
			}
			msgs, err := svc.ListMessages(ctx, wa.MessageQuery{Chat: in.Chat, Sender: in.Sender, Query: in.Query,
				After: after, Before: before, Limit: min(in.Limit, 500), Page: in.Page,
				IncludeStatus: in.IncludeStatus, OnlyMedia: in.OnlyMedia})
			if err != nil {
				return nil, nil, err
			}
			return result(msgs)
		})

	mcp.AddTool(srv, tool("get_message_context", "Get message context",
		"Get the messages before and after a given message in the same chat.", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, in messageContextIn) (*mcp.CallToolResult, any, error) {
			b, a := in.Before, in.After
			if b <= 0 {
				b = 5
			}
			if a <= 0 {
				a = 5
			}
			c, err := svc.GetMessageContext(ctx, in.Chat, in.MessageID, min(b, 100), min(a, 100))
			if err != nil {
				return nil, nil, err
			}
			return result(c)
		})

	mcp.AddTool(srv, tool("send_message", "Send message",
		"Send a WhatsApp text message as the user, optionally as a quoted reply. Returns the new message id and chat JID. Confirm with the user before sending.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendMessageIn) (*mcp.CallToolResult, any, error) {
			s, err := svc.SendText(ctx, in.Recipient, in.Message, in.ReplyTo)
			if err != nil {
				return nil, nil, err
			}
			return result(s)
		})

	mcp.AddTool(srv, tool("send_file", "Send file",
		"Send a local file (image, video, audio or document) with an optional caption. Returns the message id and chat JID.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendFileIn) (*mcp.CallToolResult, any, error) {
			s, err := svc.SendFile(ctx, in.Recipient, in.MediaPath, in.Caption)
			if err != nil {
				return nil, nil, err
			}
			return result(s)
		})

	mcp.AddTool(srv, tool("send_audio_message", "Send voice note",
		"Send an audio file as a WhatsApp voice note (push-to-talk). Non-Ogg/Opus input is converted with ffmpeg if it is installed; otherwise use send_file.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in sendAudioIn) (*mcp.CallToolResult, any, error) {
			s, err := svc.SendAudio(ctx, in.Recipient, in.MediaPath)
			if err != nil {
				return nil, nil, err
			}
			return result(s)
		})

	mcp.AddTool(srv, tool("download_media", "Download media",
		"Download the attachment of a stored message (image, video, voice note, document, sticker) and return its local file path.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in downloadMediaIn) (*mcp.CallToolResult, any, error) {
			d, err := svc.DownloadMedia(ctx, in.ChatJID, in.MessageID)
			if err != nil {
				return nil, nil, err
			}
			return result(d)
		})

	mcp.AddTool(srv, tool("edit_message", "Edit message",
		"Edit the text of a message you sent (WhatsApp allows this for about 15 minutes after sending).", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in editMessageIn) (*mcp.CallToolResult, any, error) {
			s, err := svc.Edit(ctx, in.ChatJID, in.MessageID, in.Message)
			if err != nil {
				return nil, nil, err
			}
			return result(s)
		})

	mcp.AddTool(srv, destructive(tool("delete_message", "Delete message",
		"Delete (revoke) a message for everyone. Works on your own messages, or others' messages if you are a group admin.", false)),
		func(ctx context.Context, _ *mcp.CallToolRequest, in deleteMessageIn) (*mcp.CallToolResult, any, error) {
			if err := svc.Revoke(ctx, in.ChatJID, in.MessageID, in.Sender); err != nil {
				return nil, nil, err
			}
			return ok(map[string]any{"id": in.MessageID})
		})

	mcp.AddTool(srv, tool("react", "React",
		"React to a message with an emoji, or remove your reaction with an empty emoji.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in reactIn) (*mcp.CallToolResult, any, error) {
			if err := svc.React(ctx, in.ChatJID, in.MessageID, in.Emoji); err != nil {
				return nil, nil, err
			}
			return ok(nil)
		})

	mcp.AddTool(srv, tool("create_poll", "Create poll",
		"Send a poll. Votes are decrypted as they arrive; read them with poll_results.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in createPollIn) (*mcp.CallToolResult, any, error) {
			s, err := svc.CreatePoll(ctx, in.Recipient, in.Question, in.Options, in.Multi)
			if err != nil {
				return nil, nil, err
			}
			return result(s)
		})

	mcp.AddTool(srv, tool("poll_results", "Poll results",
		"Show votes per option for a poll that was sent or received while this server was running.", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, in pollResultsIn) (*mcp.CallToolResult, any, error) {
			r, err := svc.PollResults(ctx, in.PollID)
			if err != nil {
				return nil, nil, err
			}
			return result(r)
		})

	mcp.AddTool(srv, tool("list_groups", "List groups",
		"List the groups this account belongs to, with member counts (fetched live from WhatsApp).", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			g, err := svc.ListGroups(ctx)
			if err != nil {
				return nil, nil, err
			}
			return result(g)
		})

	mcp.AddTool(srv, tool("group_info", "Group info",
		"Get a group's subject, topic and members (JID, LID, phone, name, admin flags).", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, in groupIn) (*mcp.CallToolResult, any, error) {
			g, err := svc.GroupInfo(ctx, in.GroupJID)
			if err != nil {
				return nil, nil, err
			}
			return result(g)
		})

	mcp.AddTool(srv, destructive(tool("update_group_participants", "Update group members",
		"Add, remove, promote or demote group members (requires admin).", false)),
		func(ctx context.Context, _ *mcp.CallToolRequest, in updateParticipantsIn) (*mcp.CallToolResult, any, error) {
			r, err := svc.UpdateParticipants(ctx, in.GroupJID, in.Action, in.Participants)
			if err != nil {
				return nil, nil, err
			}
			return ok(map[string]any{"results": r})
		})

	mcp.AddTool(srv, tool("create_group", "Create group",
		"Create a new group with the given members. Returns the group JID.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in createGroupIn) (*mcp.CallToolResult, any, error) {
			g, err := svc.CreateGroup(ctx, in.Name, in.Participants)
			if err != nil {
				return nil, nil, err
			}
			return result(g)
		})

	mcp.AddTool(srv, tool("rename_group", "Rename group",
		"Change a group's subject.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in renameGroupIn) (*mcp.CallToolResult, any, error) {
			if err := svc.RenameGroup(ctx, in.GroupJID, in.Name); err != nil {
				return nil, nil, err
			}
			return ok(nil)
		})

	mcp.AddTool(srv, tool("mark_read", "Mark read",
		"Send read receipts (blue ticks) for messages in a chat.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in markReadIn) (*mcp.CallToolResult, any, error) {
			if err := svc.MarkRead(ctx, in.ChatJID, in.MessageIDs, in.Sender); err != nil {
				return nil, nil, err
			}
			return ok(nil)
		})

	mcp.AddTool(srv, tool("pin_message", "Pin message",
		"Pin a message for everyone in the chat for 1, 7 or 30 days, or unpin it.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in pinIn) (*mcp.CallToolResult, any, error) {
			days := in.Days
			if days == 0 {
				days = 7
			}
			if err := svc.Pin(ctx, in.ChatJID, in.MessageID, days, in.Unpin); err != nil {
				return nil, nil, err
			}
			return ok(nil)
		})

	mcp.AddTool(srv, tool("request_history", "Request history",
		"Ask the phone to send older messages of a chat (on-demand history sync). Messages arrive asynchronously and are stored; query them again with list_messages after a few seconds.", false),
		func(ctx context.Context, _ *mcp.CallToolRequest, in requestHistoryIn) (*mcp.CallToolResult, any, error) {
			mode := wa.AnchorOldest
			switch in.Mode {
			case "", "older":
			case "latest":
				mode = wa.AnchorLatest
			case "missing_sender":
				mode = wa.AnchorMissingSender
			default:
				return nil, nil, fmt.Errorf("mode must be older, latest or missing_sender")
			}
			r, err := svc.RequestHistory(ctx, in.ChatJID, in.Count, mode)
			if err != nil {
				return nil, nil, err
			}
			return result(r)
		})

	mcp.AddTool(srv, tool("connection_status", "Connection status",
		"Report whether the WhatsApp session is paired and connected.", true),
		func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, any, error) {
			return result(svc.Status())
		})

	return srv
}
