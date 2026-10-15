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
instance with `--rest` and have the others use the REST API.

## Tools

| Tool | What it does |
|---|---|
| `search_contacts` | Find people by name or number (stored chats + synced address book) |
| `list_chats` / `get_chat` | Chats with last message; filter by name, groups/direct |
| `list_messages` | Filter by chat, sender, text, date range, media; resolves LID senders to phone + name; excludes Status (`status@broadcast`) unless asked |
| `get_message_context` | Messages around a given message |
| `send_message` | Text, optionally as a quoted reply (`reply_to`) |
| `send_file` | Image, video, document or audio with caption |
| `send_audio_message` | Voice note; converts to Ogg/Opus with ffmpeg when needed |
| `download_media` | Save an attachment locally and return its path |
| `edit_message` / `delete_message` | Edit or revoke your messages |
| `react` | Emoji reaction (empty emoji removes it) |
| `create_poll` / `poll_results` | Send polls; votes are decrypted and tallied |
| `list_groups` / `group_info` | Joined groups, members with phone/LID/admin flags |
| `create_group` / `rename_group` / `update_group_participants` | Manage groups (add, remove, promote, demote) |
| `mark_read` | Send read receipts |
| `pin_message` | Pin/unpin for 1, 7 or 30 days |
| `request_history` | Ask the phone for older messages of a chat |
| `connection_status` | Is the session paired and connected? |

Every send returns the new message `id` and `chat_jid`. Recipients can be phone numbers with country
code (`+1 555 0100`) or JIDs (`15550100@s.whatsapp.net`, `123456789@lid`, `123456789-123@g.us`).

## Flags

| Flag | Default | |
|---|---|---|
| `--data-dir` | `~/.local/share/whatsmeow-mcp` | Session (`whatsapp.db`), history (`messages.db`), downloads (`media/`) |
| `--rest ADDR` | off | Also serve the REST API; a bare `:8080` binds to `127.0.0.1` |
| `--no-mcp` | false | Daemon mode (REST only, no stdio) |
| `--no-connect` | false | Offline: read local history, never connect |
| `--pair-phone` | | Pair with a code instead of a QR |
| `--log-level` | INFO | Logs always go to stderr; stdout is the MCP channel |

## REST API (optional)

`--rest :8080` exposes the same endpoints as the original
[lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) bridge, so existing scripts keep
working. All are `POST` with a JSON body:

`/api/send` `{recipient, message, media_path?, reply_to?}` ·
`/api/reply` `{chat_jid, id, message}` · `/api/edit` `{chat_jid, id, message}` ·
`/api/react` `{chat_jid, id, emoji}` · `/api/revoke` `{chat_jid, id}` ·
`/api/send_pinned` `{recipient, message}` · `/api/poll` `{recipient, question, options, multi}` ·
`/api/poll_results` `{id}` · `/api/groups` · `/api/group_info` `{jid}` ·
`/api/group_participants` `{jid, action, participants}` · `/api/create_group` `{name, participants}` ·
`/api/rename_group` `{jid, name}` · `/api/mark_read` `{chat_jid, ids, sender?}` ·
`/api/history` `{chat_jid, count}` (`?latest=1`) · `/api/download` `{message_id, chat_jid}` ·
`GET /api/status`.

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
