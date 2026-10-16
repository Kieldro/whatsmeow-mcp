package store

import (
	"database/sql"
	"strings"
	"time"
)

// activitySchema holds the tables for things that happen to messages after
// they are sent (receipts, reactions) and for calls.
const activitySchema = `
CREATE TABLE IF NOT EXISTS receipts (
	msg_id TEXT,
	chat_jid TEXT,
	reader TEXT,
	type TEXT,
	timestamp TIMESTAMP,
	PRIMARY KEY (msg_id, chat_jid, reader, type)
);
CREATE INDEX IF NOT EXISTS idx_receipts_ts ON receipts(timestamp);
CREATE TABLE IF NOT EXISTS reactions (
	msg_id TEXT,
	chat_jid TEXT,
	sender TEXT,
	emoji TEXT,
	timestamp TIMESTAMP,
	PRIMARY KEY (msg_id, chat_jid, sender)
);
CREATE TABLE IF NOT EXISTS calls (
	call_id TEXT PRIMARY KEY,
	caller TEXT,
	group_jid TEXT,
	media TEXT,
	is_group BOOLEAN,
	status TEXT,
	reason TEXT,
	started TIMESTAMP,
	updated TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_calls_started ON calls(started);
`

// Receipt types stored in the receipts table.
const (
	ReceiptDelivered = "delivered"
	ReceiptRead      = "read"
	ReceiptPlayed    = "played"
)

// ReceiptRank orders receipt types: played > read > delivered > unknown.
func ReceiptRank(typ string) int {
	switch typ {
	case ReceiptPlayed:
		return 3
	case ReceiptRead:
		return 2
	case ReceiptDelivered:
		return 1
	}
	return 0
}

// Receipt is one delivery/read/played receipt from another user.
type Receipt struct {
	MessageID string
	ChatJID   string
	Reader    string
	Type      string
	Timestamp time.Time
}

// SaveReceipt records a receipt. Repeats are ignored, so the first time a
// reader reached each state is kept.
func (s *Store) SaveReceipt(r Receipt) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO receipts (msg_id, chat_jid, reader, type, timestamp) VALUES (?, ?, ?, ?, ?)`,
		r.MessageID, r.ChatJID, r.Reader, r.Type, r.Timestamp.UTC())
	return err
}

// placeholders returns "?, ?, ?" for n values and appends them to args.
func placeholders(vals []string, args []any) (string, []any) {
	ph := make([]string, len(vals))
	for i, v := range vals {
		ph[i] = "?"
		args = append(args, v)
	}
	return strings.Join(ph, ", "), args
}

// Receipts returns the receipts for the given message ids, oldest first.
// Message ids are matched in any chat, because the same conversation can be
// addressed by phone number or LID.
func (s *Store) Receipts(ids []string) ([]Receipt, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph, args := placeholders(ids, nil)
	rows, err := s.db.Query(`SELECT msg_id, chat_jid, reader, type, timestamp FROM receipts
		WHERE msg_id IN (`+ph+`) ORDER BY timestamp ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Receipt
	for rows.Next() {
		var r Receipt
		var ts sql.NullTime
		if err := rows.Scan(&r.MessageID, &r.ChatJID, &r.Reader, &r.Type, &ts); err != nil {
			return nil, err
		}
		r.Timestamp = ts.Time
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReceiptStatus returns the furthest receipt state (delivered, read or
// played) reached by any reader, per message id. Messages without receipts
// are absent from the map.
func (s *Store) ReceiptStatus(ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	ph, args := placeholders(ids, nil)
	rows, err := s.db.Query(`SELECT msg_id, MAX(CASE type WHEN 'played' THEN 3 WHEN 'read' THEN 2 WHEN 'delivered' THEN 1 ELSE 0 END)
		FROM receipts WHERE msg_id IN (`+ph+`) GROUP BY msg_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{"", ReceiptDelivered, ReceiptRead, ReceiptPlayed}
	for rows.Next() {
		var id string
		var rank int
		if err := rows.Scan(&id, &rank); err != nil {
			return nil, err
		}
		if rank > 0 && rank < len(names) {
			out[id] = names[rank]
		}
	}
	return out, rows.Err()
}

// CountReceipts returns the number of stored receipts (for diagnostics).
func (s *Store) CountReceipts() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM receipts`).Scan(&n)
	return n, err
}

// Reaction is one user's current reaction to a message.
type Reaction struct {
	MessageID string
	ChatJID   string
	Sender    string
	Emoji     string
	Timestamp time.Time
}

// SaveReaction records a user's reaction to a message, replacing their
// earlier one unless it is older. An empty emoji records a removal.
func (s *Store) SaveReaction(r Reaction) error {
	_, err := s.db.Exec(`INSERT INTO reactions (msg_id, chat_jid, sender, emoji, timestamp) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(msg_id, chat_jid, sender) DO UPDATE SET emoji = excluded.emoji, timestamp = excluded.timestamp
		WHERE excluded.timestamp >= reactions.timestamp`,
		r.MessageID, r.ChatJID, r.Sender, r.Emoji, r.Timestamp.UTC())
	return err
}

// Reactions returns the current (non-removed) reactions to the given
// message ids, oldest first.
func (s *Store) Reactions(ids []string) ([]Reaction, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph, args := placeholders(ids, nil)
	rows, err := s.db.Query(`SELECT msg_id, chat_jid, sender, emoji, timestamp FROM reactions
		WHERE msg_id IN (`+ph+`) AND emoji != '' ORDER BY timestamp ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reaction
	for rows.Next() {
		var r Reaction
		var ts sql.NullTime
		if err := rows.Scan(&r.MessageID, &r.ChatJID, &r.Sender, &r.Emoji, &ts); err != nil {
			return nil, err
		}
		r.Timestamp = ts.Time
		out = append(out, r)
	}
	return out, rows.Err()
}

// Call is an incoming (or other-device) call seen by this client.
type Call struct {
	ID       string
	Caller   string
	GroupJID string
	Media    string // audio or video, when known
	IsGroup  bool
	Status   string // ringing, accepted, rejected, ended
	Reason   string // termination reason reported by WhatsApp, if any
	Started  time.Time
	Updated  time.Time
}

// SaveCall inserts a call or updates its status. Empty fields never
// overwrite known values, and the start time is kept from the first event.
func (s *Store) SaveCall(c Call) error {
	if c.Updated.IsZero() {
		c.Updated = c.Started
	}
	_, err := s.db.Exec(`INSERT INTO calls (call_id, caller, group_jid, media, is_group, status, reason, started, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(call_id) DO UPDATE SET
			caller = COALESCE(NULLIF(excluded.caller, ''), calls.caller),
			group_jid = COALESCE(NULLIF(excluded.group_jid, ''), calls.group_jid),
			media = COALESCE(NULLIF(excluded.media, ''), calls.media),
			is_group = calls.is_group OR excluded.is_group,
			status = COALESCE(NULLIF(excluded.status, ''), calls.status),
			reason = COALESCE(NULLIF(excluded.reason, ''), calls.reason),
			updated = excluded.updated`,
		c.ID, c.Caller, c.GroupJID, c.Media, c.IsGroup, c.Status, c.Reason, c.Started.UTC(), c.Updated.UTC())
	return err
}

// GetCall returns one call by id.
func (s *Store) GetCall(id string) (Call, error) {
	calls, err := s.queryCalls(`WHERE call_id = ?`, id)
	if err != nil {
		return Call{}, err
	}
	if len(calls) == 0 {
		return Call{}, ErrNotFound
	}
	return calls[0], nil
}

// ListCalls returns recent calls, newest first.
func (s *Store) ListCalls(limit int) ([]Call, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.queryCalls(`ORDER BY started DESC LIMIT ?`, limit)
}

func (s *Store) queryCalls(tail string, args ...any) ([]Call, error) {
	rows, err := s.db.Query(`SELECT call_id, COALESCE(caller, ''), COALESCE(group_jid, ''), COALESCE(media, ''),
		COALESCE(is_group, 0), COALESCE(status, ''), COALESCE(reason, ''), started, updated FROM calls `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Call
	for rows.Next() {
		var c Call
		var started, updated sql.NullTime
		if err := rows.Scan(&c.ID, &c.Caller, &c.GroupJID, &c.Media, &c.IsGroup, &c.Status, &c.Reason, &started, &updated); err != nil {
			return nil, err
		}
		c.Started, c.Updated = started.Time, updated.Time
		out = append(out, c)
	}
	return out, rows.Err()
}
