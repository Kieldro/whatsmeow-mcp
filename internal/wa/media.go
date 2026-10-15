package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"

	"github.com/Kieldro/whatsmeow-mcp/internal/jidutil"
)

// ExtractDirectPathFromURL derives a media direct path from a full media URL.
//
// The query string is kept on purpose: the oh/oe signature parameters are
// required, and stripping them makes the CDN answer 403.
func ExtractDirectPathFromURL(url string) string {
	_, path, ok := strings.Cut(url, ".net/")
	if !ok {
		return url
	}
	return "/" + path
}

// downloadable implements whatsmeow.DownloadableMessage for stored media.
type downloadable struct {
	directPath           string
	mediaKey, sha, encSH []byte
	length               uint64
	mediaType            whatsmeow.MediaType
}

func (d downloadable) GetDirectPath() string             { return d.directPath }
func (d downloadable) GetMediaKey() []byte               { return d.mediaKey }
func (d downloadable) GetFileSHA256() []byte             { return d.sha }
func (d downloadable) GetFileEncSHA256() []byte          { return d.encSH }
func (d downloadable) GetFileLength() uint64             { return d.length }
func (d downloadable) GetMediaType() whatsmeow.MediaType { return d.mediaType }

// Downloaded describes a media file saved to disk.
type Downloaded struct {
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
	Cached    bool   `json:"cached,omitempty"`
}

// DownloadMedia saves the attachment of a stored message under the media
// directory and returns its absolute path. chatJID may be empty.
func (s *Service) DownloadMedia(ctx context.Context, chatJID, id string) (Downloaded, error) {
	if chatJID != "" {
		if j, err := jidutil.Parse(chatJID); err == nil {
			chatJID = j.String()
		}
	}
	m, err := s.Store.GetMessage(chatJID, id)
	if err != nil {
		return Downloaded{}, fmt.Errorf("message %s not found in local history", id)
	}
	if m.MediaType == "" {
		return Downloaded{}, errors.New("message has no media")
	}
	dir := filepath.Join(s.MediaDir, sanitizeFilename(m.ChatJID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Downloaded{}, err
	}
	name := sanitizeFilename(m.Filename)
	if name == "" {
		name = m.MediaType + "_" + sanitizeFilename(id)
	}
	path := filepath.Join(dir, name)
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		return Downloaded{Path: path, Filename: name, MediaType: m.MediaType, Bytes: st.Size(), Cached: true}, nil
	}
	if err := s.Ready(); err != nil {
		return Downloaded{}, err
	}
	var mt whatsmeow.MediaType
	switch m.MediaType {
	case "image", "sticker":
		mt = whatsmeow.MediaImage
	case "video":
		mt = whatsmeow.MediaVideo
	case "audio":
		mt = whatsmeow.MediaAudio
	case "document":
		mt = whatsmeow.MediaDocument
	default:
		return Downloaded{}, fmt.Errorf("unsupported media type %q", m.MediaType)
	}
	dp := m.DirectPath
	if dp == "" {
		dp = ExtractDirectPathFromURL(m.URL)
	}
	if dp == "" || len(m.MediaKey) == 0 || len(m.FileEncSHA256) == 0 {
		return Downloaded{}, errors.New("incomplete media information for download")
	}
	data, err := s.Client.Download(ctx, downloadable{dp, m.MediaKey, m.FileSHA256, m.FileEncSHA256, m.FileLength, mt})
	if err != nil {
		return Downloaded{}, fmt.Errorf("download: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Downloaded{}, err
	}
	return Downloaded{Path: path, Filename: name, MediaType: m.MediaType, Bytes: int64(len(data))}, nil
}
