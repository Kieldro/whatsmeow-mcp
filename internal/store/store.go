// Package store keeps the local message history (chats, messages, polls) in
// SQLite. The schema is compatible with the lharries/whatsapp-mcp bridge's
// messages.db, plus a direct_path column for reliable media downloads.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("not found")

const schema = `
CREATE TABLE IF NOT EXISTS chats (
	jid TEXT PRIMARY KEY,
	name TEXT,
	last_message_time TIMESTAMP
);
CREATE TABLE IF NOT EXISTS messages (
	id TEXT,
	chat_jid TEXT,
	sender TEXT,
	content TEXT,
	timestamp TIMESTAMP,
	is_from_me BOOLEAN,
	media_type TEXT,
	filename TEXT,
	url TEXT,
	media_key BLOB,
	file_sha256 BLOB,
	file_enc_sha256 BLOB,
	file_length INTEGER,
	PRIMARY KEY (id, chat_jid),
	FOREIGN KEY (chat_jid) REFERENCES chats(jid)
);
CREATE TABLE IF NOT EXISTS polls (
	id TEXT PRIMARY KEY,
	chat_jid TEXT,
	question TEXT,
	options TEXT,
	created TIMESTAMP
);
CREATE TABLE IF NOT EXISTS poll_votes (
	poll_id TEXT,
	chat_jid TEXT,
	voter TEXT,
	options TEXT,
	timestamp TIMESTAMP,
	PRIMARY KEY (poll_id, voter)
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_ts ON messages(chat_jid, timestamp);
CREATE INDEX IF NOT EXISTS idx_messages_ts ON messages(timestamp);
`

// Store wraps the messages database.
type Store struct {
	db *sql.DB
}

// Chat is one row of the chats table, optionally joined with its last message.
type Chat struct {
	JID             string    `json:"jid"`
	Name            string    `json:"name"`
	LastMessageTime time.Time `json:"last_message_time"`
	LastMessage     string    `json:"last_message,omitempty"`
	LastSender      string    `json:"last_sender,omitempty"`
	LastIsFromMe    bool      `json:"last_is_from_me,omitempty"`
}

// IsGroup reports whether the chat is a group.
func (c Chat) IsGroup() bool { return strings.HasSuffix(c.JID, "@g.us") }

// Message is one stored message. Media fields are only set for media messages.
type Message struct {
	ID        string
	ChatJID   string
	ChatName  string
	Sender    string
	Content   string
	Timestamp time.Time
	IsFromMe  bool
	MediaType string
	Filename  string

	URL           string
	DirectPath    string
	MediaKey      []byte
	FileSHA256    []byte
	FileEncSHA256 []byte
	FileLength    uint64
}

// Open opens (creating if needed) the messages database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open message db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}
	// Migration for databases created by the original bridge.
	if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN direct_path TEXT`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, fmt.Errorf("migrate messages: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// UpsertChat records a chat. An empty name never overwrites an existing one,
// and last_message_time only moves forward.
func (s *Store) UpsertChat(jid, name string, last time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			name = COALESCE(NULLIF(excluded.name, ''), chats.name),
			last_message_time = CASE
				WHEN chats.last_message_time IS NULL OR excluded.last_message_time > chats.last_message_time
				THEN excluded.last_message_time ELSE chats.last_message_time END`,
		jid, name, last.UTC())
	return err
}

// SetChatName overwrites a chat's name.
func (s *Store) SetChatName(jid, name string) error {
	_, err := s.db.Exec(`UPDATE chats SET name = ? WHERE jid = ?`, name, jid)
	return err
}

// ChatName returns the stored name of a chat, or "" if unknown.
func (s *Store) ChatName(jid string) string {
	var name sql.NullString
	_ = s.db.QueryRow(`SELECT name FROM chats WHERE jid = ?`, jid).Scan(&name)
	return name.String
}

// UnnamedDirectChats returns DM chats whose name is empty or just the number.
func (s *Store) UnnamedDirectChats() ([]string, error) {
	rows, err := s.db.Query(`
		SELECT jid FROM chats
		WHERE jid NOT LIKE '%@g.us' AND jid != 'status@broadcast'
		  AND (name IS NULL OR name = '' OR name = substr(jid, 1, instr(jid, '@') - 1))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var j string
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// StoreMessage inserts or replaces a message. Messages with neither text nor
// media are skipped.
func (s *Store) StoreMessage(m Message) error {
	if m.Content == "" && m.MediaType == "" {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO messages
		(id, chat_jid, sender, content, timestamp, is_from_me, media_type, filename,
		 url, direct_path, media_key, file_sha256, file_enc_sha256, file_length)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ChatJID, m.Sender, m.Content, m.Timestamp.UTC(), m.IsFromMe, m.MediaType, m.Filename,
		m.URL, m.DirectPath, m.MediaKey, m.FileSHA256, m.FileEncSHA256, m.FileLength)
	return err
}

// UpdateContent replaces the text of a stored message (used for edits and revokes).
func (s *Store) UpdateContent(chatJID, id, content string) error {
	_, err := s.db.Exec(`UPDATE messages SET content = ? WHERE id = ? AND chat_jid = ?`, content, id, chatJID)
	return err
}

// DeleteMessage removes a stored message (used for delete-for-me).
func (s *Store) DeleteMessage(chatJID, id string) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE id = ? AND chat_jid = ?`, id, chatJID)
	return err
}

const msgColumns = `m.id, m.chat_jid, COALESCE(c.name, ''), COALESCE(m.sender, ''), COALESCE(m.content, ''),
	m.timestamp, COALESCE(m.is_from_me, 0), COALESCE(m.media_type, ''), COALESCE(m.filename, ''),
	COALESCE(m.url, ''), COALESCE(m.direct_path, ''), m.media_key, m.file_sha256, m.file_enc_sha256,
	COALESCE(m.file_length, 0)`

func scanMessages(rows *sql.Rows) ([]Message, error) {
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var ts sql.NullTime
		if err := rows.Scan(&m.ID, &m.ChatJID, &m.ChatName, &m.Sender, &m.Content, &ts, &m.IsFromMe,
			&m.MediaType, &m.Filename, &m.URL, &m.DirectPath, &m.MediaKey, &m.FileSHA256,
			&m.FileEncSHA256, &m.FileLength); err != nil {
			return nil, err
		}
		m.Timestamp = ts.Time
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMessage returns one message. If chatJID is empty, the most recent
// message with that id in any chat is returned.
func (s *Store) GetMessage(chatJID, id string) (Message, error) {
	q := `SELECT ` + msgColumns + ` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid WHERE m.id = ?`
	args := []any{id}
	if chatJID != "" {
		q += ` AND m.chat_jid = ?`
		args = append(args, chatJID)
	}
	rows, err := s.db.Query(q+` ORDER BY m.timestamp DESC LIMIT 1`, args...)
	if err != nil {
		return Message{}, err
	}
	msgs, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, ErrNotFound
	}
	return msgs[0], nil
}

// MessageFilter selects messages for ListMessages. Empty fields do not filter.
type MessageFilter struct {
	// ChatJIDs matches any of these chats (a contact may have a phone-number
	// chat and a LID chat).
	ChatJIDs []string
	// Senders matches any of these stored sender values.
	Senders []string
	// Query is a case-insensitive substring match on the message text.
	Query  string
	After  time.Time
	Before time.Time
	// IncludeStatus includes WhatsApp Status (status@broadcast) posts.
	IncludeStatus bool
	// OnlyMedia restricts to messages with attachments.
	OnlyMedia bool
	Limit     int
	Offset    int
}

// BuildMessageQuery turns a filter into SQL plus arguments. Results are newest first.
func BuildMessageQuery(f MessageFilter) (string, []any) {
	var where []string
	var args []any
	in := func(col string, vals []string) {
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = "?"
			args = append(args, v)
		}
		where = append(where, col+" IN ("+strings.Join(ph, ", ")+")")
	}
	if len(f.ChatJIDs) > 0 {
		in("m.chat_jid", f.ChatJIDs)
	}
	if len(f.Senders) > 0 {
		in("m.sender", f.Senders)
	}
	if !f.IncludeStatus {
		where = append(where, "m.chat_jid != 'status@broadcast'")
	}
	if f.Query != "" {
		where = append(where, "LOWER(m.content) LIKE LOWER(?)")
		args = append(args, "%"+f.Query+"%")
	}
	if !f.After.IsZero() {
		where = append(where, "m.timestamp > ?")
		args = append(args, f.After.UTC())
	}
	if !f.Before.IsZero() {
		where = append(where, "m.timestamp < ?")
		args = append(args, f.Before.UTC())
	}
	if f.OnlyMedia {
		where = append(where, "COALESCE(m.media_type, '') != ''")
	}
	q := `SELECT ` + msgColumns + ` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	q += " ORDER BY m.timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, limit, max(f.Offset, 0))
	return q, args
}

// ListMessages returns messages matching the filter, newest first.
func (s *Store) ListMessages(f MessageFilter) ([]Message, error) {
	q, args := BuildMessageQuery(f)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

// Context returns up to before/after messages around a message, in chronological order.
func (s *Store) Context(target Message, before, after int) (prev, next []Message, err error) {
	base := `SELECT ` + msgColumns + ` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid WHERE m.chat_jid = ? `
	rows, err := s.db.Query(base+`AND m.timestamp < ? ORDER BY m.timestamp DESC LIMIT ?`, target.ChatJID, target.Timestamp.UTC(), before)
	if err != nil {
		return nil, nil, err
	}
	if prev, err = scanMessages(rows); err != nil {
		return nil, nil, err
	}
	for i, j := 0, len(prev)-1; i < j; i, j = i+1, j-1 {
		prev[i], prev[j] = prev[j], prev[i]
	}
	rows, err = s.db.Query(base+`AND m.timestamp > ? ORDER BY m.timestamp ASC LIMIT ?`, target.ChatJID, target.Timestamp.UTC(), after)
	if err != nil {
		return nil, nil, err
	}
	next, err = scanMessages(rows)
	return prev, next, err
}

// ChatFilter selects chats for ListChats.
type ChatFilter struct {
	JID           string // exact match
	Query         string // substring of name or JID
	OnlyGroups    bool
	OnlyDirect    bool
	IncludeStatus bool
	SortByName    bool
	Limit         int
	Offset        int
}

// ListChats returns chats with their last message.
func (s *Store) ListChats(f ChatFilter) ([]Chat, error) {
	q := `SELECT c.jid, COALESCE(c.name, ''), c.last_message_time,
		COALESCE((SELECT content FROM messages WHERE chat_jid = c.jid ORDER BY timestamp DESC LIMIT 1), ''),
		COALESCE((SELECT sender FROM messages WHERE chat_jid = c.jid ORDER BY timestamp DESC LIMIT 1), ''),
		COALESCE((SELECT is_from_me FROM messages WHERE chat_jid = c.jid ORDER BY timestamp DESC LIMIT 1), 0)
		FROM chats c`
	var where []string
	var args []any
	if f.JID != "" {
		where = append(where, "c.jid = ?")
		args = append(args, f.JID)
	}
	if f.Query != "" {
		where = append(where, "(LOWER(c.name) LIKE LOWER(?) OR c.jid LIKE ?)")
		args = append(args, "%"+f.Query+"%", "%"+f.Query+"%")
	}
	if f.OnlyGroups {
		where = append(where, "c.jid LIKE '%@g.us'")
	}
	if f.OnlyDirect {
		where = append(where, "c.jid NOT LIKE '%@g.us'")
	}
	if !f.IncludeStatus {
		where = append(where, "c.jid != 'status@broadcast'")
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if f.SortByName {
		q += " ORDER BY LOWER(c.name)"
	} else {
		q += " ORDER BY c.last_message_time DESC"
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	q += " LIMIT ? OFFSET ?"
	args = append(args, limit, max(f.Offset, 0))
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var ts sql.NullTime
		if err := rows.Scan(&c.JID, &c.Name, &ts, &c.LastMessage, &c.LastSender, &c.LastIsFromMe); err != nil {
			return nil, err
		}
		c.LastMessageTime = ts.Time
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetChat returns one chat.
func (s *Store) GetChat(jid string) (Chat, error) {
	rows, err := s.ListChats(ChatFilter{JID: jid, Limit: 1, IncludeStatus: true})
	if err != nil {
		return Chat{}, err
	}
	if len(rows) == 0 {
		return Chat{}, ErrNotFound
	}
	return rows[0], nil
}

// ChatExists reports whether a chat row exists.
func (s *Store) ChatExists(jid string) bool {
	var one int
	return s.db.QueryRow(`SELECT 1 FROM chats WHERE jid = ?`, jid).Scan(&one) == nil
}

// OldestMessage returns the oldest stored message of a chat.
func (s *Store) OldestMessage(chatJID string) (Message, error) {
	rows, err := s.db.Query(`SELECT `+msgColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid
		WHERE m.chat_jid = ? ORDER BY m.timestamp ASC LIMIT 1`, chatJID)
	if err != nil {
		return Message{}, err
	}
	msgs, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, ErrNotFound
	}
	return msgs[0], nil
}

// AnchorAfterMissingSender finds the message just after the newest message
// whose sender equals one of the given placeholder values (rows stored without
// a real sender). It is the anchor for re-requesting those rows from the phone.
func (s *Store) AnchorAfterMissingSender(chatJID string, placeholders []string) (Message, error) {
	ph := strings.TrimSuffix(strings.Repeat("?, ", len(placeholders)), ", ")
	args := []any{chatJID, chatJID}
	for _, p := range placeholders {
		args = append(args, p)
	}
	rows, err := s.db.Query(`SELECT `+msgColumns+` FROM messages m LEFT JOIN chats c ON c.jid = m.chat_jid
		WHERE m.chat_jid = ? AND m.timestamp > (
			SELECT MAX(timestamp) FROM messages WHERE chat_jid = ? AND sender IN (`+ph+`))
		ORDER BY m.timestamp ASC LIMIT 1`, args...)
	if err != nil {
		return Message{}, err
	}
	msgs, err := scanMessages(rows)
	if err != nil {
		return Message{}, err
	}
	if len(msgs) == 0 {
		return Message{}, ErrNotFound
	}
	return msgs[0], nil
}

// Poll is a tracked poll.
type Poll struct {
	ID       string
	ChatJID  string
	Question string
	Options  []string
}

// SavePoll records a poll so votes can be decoded later.
func (s *Store) SavePoll(p Poll, created time.Time) error {
	b, _ := json.Marshal(p.Options)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO polls (id, chat_jid, question, options, created) VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.ChatJID, p.Question, string(b), created.UTC())
	return err
}

// GetPoll loads a poll by message id.
func (s *Store) GetPoll(id string) (Poll, error) {
	var p Poll
	var opts string
	err := s.db.QueryRow(`SELECT id, COALESCE(chat_jid, ''), question, options FROM polls WHERE id = ?`, id).
		Scan(&p.ID, &p.ChatJID, &p.Question, &opts)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(opts), &p.Options); err != nil {
		return p, fmt.Errorf("decode poll options: %w", err)
	}
	return p, nil
}

// SaveVote records a voter's current selection (replacing any earlier vote).
func (s *Store) SaveVote(pollID, chatJID, voter string, chosen []string, ts time.Time) error {
	if chosen == nil {
		chosen = []string{}
	}
	b, _ := json.Marshal(chosen)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO poll_votes (poll_id, chat_jid, voter, options, timestamp) VALUES (?, ?, ?, ?, ?)`,
		pollID, chatJID, voter, string(b), ts.UTC())
	return err
}

// Votes returns voter -> chosen options for a poll.
func (s *Store) Votes(pollID string) (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT voter, options FROM poll_votes WHERE poll_id = ?`, pollID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var voter, opts string
		if err := rows.Scan(&voter, &opts); err != nil {
			return nil, err
		}
		var chosen []string
		if err := json.Unmarshal([]byte(opts), &chosen); err != nil {
			return nil, err
		}
		out[voter] = chosen
	}
	return out, rows.Err()
}

// TallyVotes groups votes by option, keeping every option (even with no votes).
func TallyVotes(options []string, votes map[string][]string) map[string][]string {
	out := make(map[string][]string, len(options))
	for _, o := range options {
		out[o] = []string{}
	}
	for voter, chosen := range votes {
		for _, o := range chosen {
			out[o] = append(out[o], voter)
		}
	}
	return out
}
