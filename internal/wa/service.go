// Package wa holds the WhatsApp linked-device session (whatsmeow), records
// incoming messages into the local store, and implements every action the
// MCP tools and REST API expose.
package wa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"github.com/Kieldro/whatsmeow-mcp/internal/store"
)

// ErrNotConnected is returned by actions that need a live WhatsApp connection.
var ErrNotConnected = errors.New("not connected to WhatsApp (offline mode, not logged in, or connection lost); run `whatsmeow-mcp login` if this device is not paired yet")

// Service is a WhatsApp session plus its local message store.
type Service struct {
	Client   *whatsmeow.Client
	Store    *store.Store
	DataDir  string
	MediaDir string

	container *sqlstore.Container
	log       waLog.Logger
	offline   bool

	loginMu sync.Mutex

	presenceMu sync.Mutex
	presence   map[string]LastSeen
}

// Options configures New.
type Options struct {
	DataDir string
	Log     waLog.Logger
	// Offline opens the databases but never connects to WhatsApp.
	Offline bool
}

// New opens the session and message databases under DataDir.
func New(ctx context.Context, opts Options) (*Service, error) {
	if opts.Log == nil {
		opts.Log = waLog.Noop
	}
	if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	container, err := sqlstore.New(ctx, "sqlite3",
		"file:"+filepath.Join(opts.DataDir, "whatsapp.db")+"?_foreign_keys=on&_busy_timeout=5000",
		opts.Log.Sub("Database"))
	if err != nil {
		return nil, fmt.Errorf("open session db: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		container.Close()
		return nil, fmt.Errorf("load device: %w", err)
	}
	msgs, err := store.Open(filepath.Join(opts.DataDir, "messages.db"))
	if err != nil {
		container.Close()
		return nil, err
	}
	wastore.SetOSInfo("whatsmeow-mcp", [3]uint32{0, 1, 0})
	s := &Service{
		Client:    whatsmeow.NewClient(device, opts.Log.Sub("Client")),
		Store:     msgs,
		DataDir:   opts.DataDir,
		MediaDir:  filepath.Join(opts.DataDir, "media"),
		container: container,
		log:       opts.Log,
		offline:   opts.Offline,
		presence:  map[string]LastSeen{},
	}
	s.Client.AddEventHandler(s.handleEvent)
	return s, nil
}

// Close disconnects and closes the databases.
func (s *Service) Close() {
	s.Client.Disconnect()
	s.Store.Close()
	s.container.Close()
}

// LoggedIn reports whether this device has been paired.
func (s *Service) LoggedIn() bool { return s.Client.Store.ID != nil }

// Ready returns nil if actions that talk to WhatsApp can run.
func (s *Service) Ready() error {
	if s.offline || !s.LoggedIn() || !s.Client.IsConnected() {
		return ErrNotConnected
	}
	return nil
}

// Status summarizes the session state.
func (s *Service) Status() map[string]any {
	out := map[string]any{
		"offline":   s.offline,
		"logged_in": s.LoggedIn(),
		"connected": s.Client.IsConnected(),
		"data_dir":  s.DataDir,
	}
	if id := s.Client.Store.ID; id != nil {
		out["jid"] = id.ToNonAD().String()
	}
	return out
}

// Connect connects an already-paired device. It is a no-op in offline mode.
func (s *Service) Connect(ctx context.Context) error {
	if s.offline {
		return nil
	}
	if !s.LoggedIn() {
		return errors.New("device not paired; run `whatsmeow-mcp login`")
	}
	return s.Client.Connect()
}

// Login pairs a new device, printing a QR code (or a pairing code when
// pairPhone is set) to out, and blocks until pairing succeeds, fails, or ctx
// ends. The connection stays open afterwards.
func (s *Service) Login(ctx context.Context, pairPhone string, out io.Writer) error {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.offline {
		return errors.New("cannot log in while offline")
	}
	if s.LoggedIn() {
		if s.Client.IsConnected() {
			return nil
		}
		return s.Client.Connect()
	}
	qrChan, err := s.Client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("get QR channel: %w", err)
	}
	if err := s.Client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	pairRequested := false
	for evt := range qrChan {
		switch evt.Event {
		case "code":
			if pairPhone != "" {
				if pairRequested {
					continue
				}
				pairRequested = true
				code, err := s.Client.PairPhone(ctx, pairPhone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
				if err != nil {
					return fmt.Errorf("request pairing code: %w", err)
				}
				fmt.Fprintf(out, "\nPairing code: %s\nOn your phone: WhatsApp > Linked devices > Link a device > Link with phone number instead.\n", code)
				_ = os.WriteFile(filepath.Join(s.DataDir, "pair_code.txt"), []byte(code+"\n"), 0o600)
			} else {
				fmt.Fprintln(out, "\nScan this QR code with WhatsApp (Linked devices > Link a device):")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, out)
			}
		case "success":
			fmt.Fprintln(out, "Paired successfully.")
			_ = os.Remove(filepath.Join(s.DataDir, "pair_code.txt"))
			return nil
		case "timeout":
			return errors.New("timed out waiting for the QR code to be scanned")
		default:
			if evt.Error != nil {
				return fmt.Errorf("login failed (%s): %w", evt.Event, evt.Error)
			}
		}
	}
	return errors.New("login channel closed before pairing finished")
}

// WaitConnected waits up to d for the connection to come up.
func (s *Service) WaitConnected(d time.Duration) bool {
	return s.Client.WaitForConnection(d)
}

func (s *Service) handleEvent(evt any) {
	ctx := context.Background()
	switch v := evt.(type) {
	case *events.Message:
		s.handleMessage(ctx, v)
	case *events.HistorySync:
		s.handleHistorySync(ctx, v)
	case *events.Presence:
		s.recordPresence(v)
	case *events.Connected:
		s.log.Infof("Connected to WhatsApp")
	case *events.LoggedOut:
		s.log.Warnf("Device was logged out; run `whatsmeow-mcp login` to pair again")
	}
}
