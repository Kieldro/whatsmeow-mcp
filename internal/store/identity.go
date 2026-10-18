package store

import (
	"database/sql"
	"time"
)

// identitySchema records contacts whose WhatsApp identity changed: a new
// primary device (identity-change notification) or a new LID for the same
// phone number (seen when resolving a recipient before sending).
const identitySchema = `
CREATE TABLE IF NOT EXISTS identity_changes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	jid TEXT,
	phone TEXT,
	old_lid TEXT,
	new_lid TEXT,
	source TEXT,
	implicit BOOLEAN,
	timestamp TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_identity_changes_ts ON identity_changes(timestamp);
`

// Identity change sources.
const (
	// IdentitySourceNotification is a server identity-change notification.
	IdentitySourceNotification = "notification"
	// IdentitySourceSend is a LID mismatch found while resolving a recipient.
	IdentitySourceSend = "send"
)

// IdentityChange is one recorded identity change.
type IdentityChange struct {
	ID        int64     `json:"id"`
	JID       string    `json:"jid"`
	Phone     string    `json:"phone,omitempty"`
	OldLID    string    `json:"old_lid,omitempty"`
	NewLID    string    `json:"new_lid,omitempty"`
	Source    string    `json:"source"`
	Implicit  bool      `json:"implicit,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// SaveIdentityChange records an identity change.
func (s *Store) SaveIdentityChange(c IdentityChange) error {
	if c.Timestamp.IsZero() {
		c.Timestamp = time.Now()
	}
	_, err := s.db.Exec(`INSERT INTO identity_changes (jid, phone, old_lid, new_lid, source, implicit, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, c.JID, c.Phone, c.OldLID, c.NewLID, c.Source, c.Implicit, c.Timestamp.UTC())
	return err
}

// IdentityChanges returns recorded identity changes, newest first.
func (s *Store) IdentityChanges(limit int) ([]IdentityChange, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, jid, phone, old_lid, new_lid, source, implicit, timestamp
		FROM identity_changes ORDER BY timestamp DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdentityChange{}
	for rows.Next() {
		var c IdentityChange
		var phone, oldLID, newLID sql.NullString
		var ts sql.NullTime
		if err := rows.Scan(&c.ID, &c.JID, &phone, &oldLID, &newLID, &c.Source, &c.Implicit, &ts); err != nil {
			return nil, err
		}
		c.Phone, c.OldLID, c.NewLID, c.Timestamp = phone.String, oldLID.String, newLID.String, ts.Time
		out = append(out, c)
	}
	return out, rows.Err()
}
