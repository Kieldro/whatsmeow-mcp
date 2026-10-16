# whatsmeow-mcp

A WhatsApp [MCP](https://modelcontextprotocol.io) server in a **single Go binary**. It links to your
WhatsApp account as a companion device (like WhatsApp Web) using
[whatsmeow](https://github.com/tulir/whatsmeow), keeps a local SQLite history of your chats, and serves
MCP tools over stdio. No Python, no separate bridge process.

> **Unofficial.** This project is not affiliated with WhatsApp or Meta. Automated or bulk use of
> WhatsApp may violate its Terms of Service and can get your number banned. Use it for your own
> account, at human pace, and at your own risk.

## Install

Requires Go 1.26+ and a C compiler (SQLite via cgo). `ffmpeg` is optional (voice-note conversion).

```sh
go install github.com/Kieldro/whatsmeow-mcp/cmd/whatsmeow-mcp@latest
```

## Log in

Pair once from a terminal:

```sh
whatsmeow-mcp login                        # scan the QR code: WhatsApp > Linked devices > Link a device
whatsmeow-mcp login --pair-phone 15550100  # or get an 8-character pairing code instead
```

The session and message history live in `--data-dir` (default `~/.local/share/whatsmeow-mcp`,
or `$WHATSMEOW_MCP_DATA_DIR`). Treat that directory like a password: anyone with it can act as you.
After pairing, your phone pushes recent history; older messages can be pulled with `request_history`.

## Use with Claude Code

```sh
claude mcp add whatsapp -- whatsmeow-mcp
# or with options
claude mcp add whatsapp -- whatsmeow-mcp --data-dir ~/.local/share/whatsmeow-mcp --rest :8080
```

Other MCP clients: run `whatsmeow-mcp` as a stdio server, e.g.

```json
{ "mcpServers": { "whatsapp": { "command": "whatsmeow-mcp", "args": [] } } }
```

Only one process can use a data directory at a time (it is locked), because two clients on the same
linked device fight over the connection. To share one session between several tools, run one
long-lived daemon and have MCP clients connect to it over HTTP (below), and scripts use `--rest`.

### Daemon mode (MCP over HTTP)

Run one instance as a service that holds the session, with no stdio MCP:

```ini
# ~/.config/systemd/user/whatsmeow-mcp.service
[Unit]
Description=WhatsApp MCP server (whatsmeow)
After=network-online.target

[Service]
ExecStart=%h/go/bin/whatsmeow-mcp --no-mcp --rest :8080 --mcp-http :8090
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```

```sh
whatsmeow-mcp login            # pair once, before starting the service
systemctl --user daemon-reload
systemctl --user enable --now whatsmeow-mcp
claude mcp add --transport http whatsapp http://127.0.0.1:8090/mcp
```

The MCP endpoint is `/mcp` (Streamable HTTP). It has no authentication, so keep it on loopback
(a bare `:8090` binds to `127.0.0.1`).

## Tools

| Tool | What it does |
|---|---|
| `search_contacts` | Find people by name or number (stored chats + synced address book) |
| `list_chats` / `get_chat` | Chats with last message; filter by name, groups/direct |
| `list_messages` | Filter by chat, sender, text, date range, media; resolves LID senders to phone + name; shows `reply_to`, reactions and, for your own messages, receipt `status` (delivered/read/played); excludes Status (`status@broadcast`) unless asked |
| `get_message_context` | Messages around a given message |
| `send_message` | Text, optionally as a quoted reply (`reply_to`) and with `@mentions` |
| `send_file` | Image, video, document or audio with caption |
| `send_audio_message` | Voice note; converts to Ogg/Opus with ffmpeg when needed |
| `download_media` | Save an attachment locally and return its path |
| `edit_message` / `delete_message` | Edit or revoke your messages |
| `react` | Emoji reaction (empty emoji removes it) |
| `create_poll` / `poll_results` / `vote_poll` | Send polls, vote in them; votes are decrypted and tallied |
| `list_groups` / `group_info` | Joined groups, members with phone/LID/admin flags |
| `create_group` / `rename_group` / `update_group_participants` | Manage groups (add, remove, promote, demote) |
| `mark_read` | Send read receipts |
| `pin_message` | Pin/unpin for 1, 7 or 30 days |
| `request_history` | Ask the phone for older messages of a chat |
| `connection_status` | Is the session paired and connected? |
| **Receipts, reactions, calls** (recorded from live events) | |
| `get_message_receipts` | Delivered/read/played times per message and per reader (also for messages sent from your phone) |
| `get_message_reactions` | Who reacted with what, per message |
| `list_calls` / `reject_call` | Incoming calls (caller, audio/video, status); decline a ringing call |
| **Groups** | |
| `get_group_invite_link` | Invite link (`reset` revokes the old one) |
| `get_group_info_from_link` / `join_group_with_link` | Preview a group from its link, or join it |
| `leave_group` | Leave a group |
| `set_group_description` / `set_group_photo` | Change description or photo (JPEG; omit path to remove) |
| `set_group_announce` / `set_group_locked` | Admins-only messages / admins-only info edits |
| `list_group_join_requests` / `update_group_join_requests` | Pending join requests; approve or reject |
| `set_group_join_approval` / `set_group_member_add_mode` | Require admin approval to join; who can add members |
| `list_communities` / `list_community_groups` | Communities and their linked groups |
| `create_community` / `link_group_to_community` / `unlink_group_from_community` | Create a community; add or remove groups |
| **Contacts** | |
| `is_on_whatsapp` | Which numbers have WhatsApp (batch, max 50) |
| `get_user_info` | About text, picture ID, LID, business name |
| `get_profile_picture` | Save a user/group photo under `media/profile_pictures/` |
| `block_contact` / `unblock_contact` / `list_blocklist` | Block list management |
| `get_business_profile` | Business address, email, categories, hours |
| `get_contact_qr_link` / `resolve_link` | Your wa.me/qr link; who a wa.me/qr or wa.me/message link belongs to |
| **Messages** | |
| `send_location` / `send_contact_card` / `send_sticker` | Location pin, vCard from name + number, `.webp` sticker |
| `forward_message` | Re-send a stored text or media message, marked as forwarded |
| `star_message` / `delete_message_for_me` | Star/unstar; delete only on your devices |
| **Presence** | |
| `send_presence` / `send_chat_presence` | Online/offline; typing, recording, paused |
| `subscribe_presence` / `get_last_seen` | Subscribe to a user's presence; read the latest update |
| **Chats** (synced to your phone) | |
| `archive_chat` / `pin_chat` / `mute_chat` / `mark_chat_read` | Archive, pin, mute (`8h`, `1w`, `forever`), read/unread |
| `set_disappearing_timer` / `set_default_disappearing_timer` | `off`, `24h`, `7d`, `90d`, per chat or default for new chats |
| **Status** | |
| `post_text_status` / `post_media_status` / `list_statuses` | Post text or image/video stories; list stored ones |
| `get_status_privacy` | Who receives your Status posts |
| **Channels** | |
| `list_newsletters` / `newsletter_info` / `newsletter_messages` | Followed channels, channel details, recent posts |
| `follow_newsletter` / `unfollow_newsletter` / `mute_newsletter` | Follow, unfollow, mute |
| `react_newsletter` / `create_newsletter` | React to a post; create your own channel |
| **Profile** | |
| `set_status_message` / `set_profile_picture` | About text; own photo (JPEG) |
| `get_privacy_settings` / `set_privacy_setting` | Last seen, online, profile, read receipts, group add, … |

Receipts, reactions and calls are only known from the moment this server started recording them;
WhatsApp delivers receipts to every linked device, so messages sent from your phone are covered too.

Tools that change things others can see (join/leave, block, status posts, group and privacy
settings) are annotated as destructive and say so in their descriptions, so clients can ask first.

Every send returns the new message `id` and `chat_jid`. Recipients can be phone numbers with country
code (`+1 555 0100`) or JIDs (`15550100@s.whatsapp.net`, `123456789@lid`, `123456789-123@g.us`).

## Flags

| Flag | Default | |
|---|---|---|
| `--data-dir` | `~/.local/share/whatsmeow-mcp` | Session (`whatsapp.db`), history (`messages.db`), downloads (`media/`) |
| `--rest ADDR` | off | Also serve the REST API; a bare `:8080` binds to `127.0.0.1` |
| `--mcp-http ADDR` | off | Also serve MCP over Streamable HTTP at `/mcp`; a bare `:8090` binds to `127.0.0.1` |
| `--no-mcp` | false | Daemon mode: no stdio MCP (use with `--rest` and/or `--mcp-http`) |
| `--no-connect` | false | Offline: read local history, never connect |
| `--pair-phone` | | Pair with a code instead of a QR |
| `--log-level` | INFO | Logs always go to stderr; stdout is the MCP channel |

## REST API (optional)

`--rest :8080` exposes the same endpoints as the original
[lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) bridge, so existing scripts keep
working. All are `POST` with a JSON body:

`/api/send` `{recipient, message, media_path?, reply_to?, mentions?}` ·
`/api/reply` `{chat_jid, id, message}` · `/api/edit` `{chat_jid, id, message}` ·
`/api/react` `{chat_jid, id, emoji}` · `/api/revoke` `{chat_jid, id}` ·
`/api/send_pinned` `{recipient, message}` · `/api/poll` `{recipient, question, options, multi}` ·
`/api/poll_results` `{id}` · `/api/groups` · `/api/group_info` `{jid}` ·
`/api/group_participants` `{jid, action, participants}` · `/api/create_group` `{name, participants}` ·
`/api/rename_group` `{jid, name}` · `/api/mark_read` `{chat_jid, ids, sender?}` ·
`/api/history` `{chat_jid, count}` (`?latest=1`) · `/api/download` `{message_id, chat_jid}` ·
`GET /api/status`.

Every tool in the Receipts, Groups, Contacts, Messages (new), Presence, Chats, Status, Channels and
Profile sections above (plus `vote_poll`) is also `POST /api/<tool name>` with the same JSON arguments as the MCP tool, e.g.
`/api/send_location` `{recipient, latitude, longitude, name?}`. It answers
`{"success": true, "result": …}`, or `{"success": false, "error": …}` with 400 for bad input and 500
for WhatsApp errors.

The REST API has no authentication. Keep it on localhost.

An existing bridge `store/` directory (`whatsapp.db` + `messages.db`) can be reused by copying it
into a data dir; stop the old bridge first.

## Development

```sh
go build ./... && go vet ./... && go test ./...
```

The test suite includes an MCP stdio smoke test that runs the binary with `--no-connect` against an
empty temporary data dir; it never contacts WhatsApp.

## Credits

- [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) (MIT), which this project started
  from and ports to a single Go binary.
- [tulir/whatsmeow](https://github.com/tulir/whatsmeow), the WhatsApp Web multidevice library.
- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk), the official Go MCP SDK.

## License

MIT, see [LICENSE](LICENSE).
