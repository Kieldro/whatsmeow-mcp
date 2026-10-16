package wa

import (
	"time"
)

// Reconnecting after the stream was replaced by another client using the same
// session must not turn into a fight where each client keeps kicking the
// other off, so the delay doubles on every replacement in a row.
const (
	replacedBaseDelay = time.Minute
	replacedMaxDelay  = 30 * time.Minute
	// replacedResetAfter is how long without a replacement before the
	// backoff starts over.
	replacedResetAfter = time.Hour
)

// ReplacedBackoff returns the reconnect delay after the n-th consecutive
// stream replacement (n >= 1): 1m, 2m, 4m, … capped at 30m.
func ReplacedBackoff(n int) time.Duration {
	d := replacedBaseDelay
	for i := 1; i < n && d < replacedMaxDelay; i++ {
		d *= 2
	}
	return min(d, replacedMaxDelay)
}

// handleStreamReplaced runs when WhatsApp disconnects this client because
// another client connected with the same session keys. whatsmeow does not
// reconnect by itself in that case.
func (s *Service) handleStreamReplaced() {
	s.replacedMu.Lock()
	now := time.Now()
	if now.Sub(s.lastReplaced) > replacedResetAfter {
		s.replacedCount = 0
	}
	s.replacedCount++
	s.lastReplaced = now
	delay := ReplacedBackoff(s.replacedCount)
	if s.reconnectTimer != nil {
		s.reconnectTimer.Stop()
	}
	if s.closed {
		s.replacedMu.Unlock()
		return
	}
	s.reconnectTimer = time.AfterFunc(delay, s.reconnectAfterReplaced)
	n := s.replacedCount
	s.replacedMu.Unlock()
	s.log.Errorf("STREAM REPLACED: another client connected with this same WhatsApp session (a second whatsmeow-mcp, "+
		"or an old bridge using the same keys) and WhatsApp disconnected this one. Stop the other client. "+
		"Reconnecting in %s (replacement #%d in a row).", delay, n)
}

func (s *Service) reconnectAfterReplaced() {
	s.replacedMu.Lock()
	closed := s.closed
	s.replacedMu.Unlock()
	if closed || s.Client.IsConnected() {
		return
	}
	s.log.Warnf("Reconnecting to WhatsApp after stream replacement")
	if err := s.Client.Connect(); err != nil {
		s.log.Errorf("Reconnect after stream replacement failed: %v", err)
	}
}
