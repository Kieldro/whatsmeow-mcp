// Package audio prepares voice notes: Ogg/Opus duration analysis, a
// placeholder waveform, and conversion of other formats with ffmpeg.
package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsOgg reports whether data starts with an Ogg page signature.
func IsOgg(data []byte) bool { return len(data) >= 4 && string(data[:4]) == "OggS" }

// AnalyzeOggOpus returns the duration (seconds, clamped to 1..3600) and a
// 64-byte placeholder waveform for an Ogg Opus file.
func AnalyzeOggOpus(data []byte) (seconds uint32, waveform []byte, err error) {
	if !IsOgg(data) {
		return 0, nil, fmt.Errorf("not an Ogg file (missing OggS signature)")
	}
	var lastGranule uint64
	const sampleRate = 48000
	var preSkip uint16
	foundHead := false
	for i := 0; i+27 < len(data); {
		if string(data[i:i+4]) != "OggS" {
			i++
			continue
		}
		granule := binary.LittleEndian.Uint64(data[i+6 : i+14])
		seq := binary.LittleEndian.Uint32(data[i+18 : i+22])
		nseg := int(data[i+26])
		if i+27+nseg > len(data) {
			break
		}
		size := 27 + nseg
		for _, l := range data[i+27 : i+27+nseg] {
			size += int(l)
		}
		end := min(i+size, len(data))
		if !foundHead && seq <= 1 {
			page := data[i:end]
			if p := bytes.Index(page, []byte("OpusHead")); p >= 0 && p+8+16 <= len(page) {
				h := page[p+8:]
				// OpusHead: version(1) channels(1) preSkip(2) inputSampleRate(4).
				// Granule positions are always 48 kHz units, whatever the input rate.
				preSkip = binary.LittleEndian.Uint16(h[2:4])
				foundHead = true
			}
		}
		if granule != 0 && granule != math.MaxUint64 {
			lastGranule = granule
		}
		i += size
	}
	var dur float64
	if lastGranule > uint64(preSkip) {
		dur = float64(lastGranule-uint64(preSkip)) / float64(sampleRate)
	} else {
		dur = float64(len(data)) / 2000.0 // rough estimate at ~16 kbit/s
	}
	seconds = uint32(math.Round(dur))
	seconds = max(1, min(seconds, 3600))
	return seconds, PlaceholderWaveform(seconds), nil
}

// PlaceholderWaveform builds a deterministic, natural-looking 64-sample
// waveform (values 0..100) for a voice note of the given length.
func PlaceholderWaveform(seconds uint32) []byte {
	const n = 64
	w := make([]byte, n)
	r := rand.New(rand.NewSource(int64(seconds)))
	freq := float64(min(int(seconds), 120)) / 30.0
	for i := range w {
		pos := float64(i) / n
		v := 35*math.Sin(pos*math.Pi*freq*8) + 17.5*math.Sin(pos*math.Pi*freq*16)
		v += (r.Float64() - 0.5) * 15
		v = v*(0.7+0.3*math.Sin(pos*math.Pi)) + 50
		w[i] = byte(max(0, min(v, 100)))
	}
	return w
}

// ConvertToOpus converts any audio file to a temporary Ogg/Opus voice-note
// file with ffmpeg. The caller must remove the returned file.
func ConvertToOpus(ctx context.Context, input string) (string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found in PATH; install ffmpeg or send the file with send_file")
	}
	f, err := os.CreateTemp("", "whatsmeow-mcp-*.ogg")
	if err != nil {
		return "", err
	}
	out := f.Name()
	f.Close()
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-i", input,
		"-vn", "-c:a", "libopus", "-b:a", "32k", "-ar", "24000", "-ac", "1",
		"-application", "voip", "-vbr", "on", "-compression_level", "10", "-frame_duration", "60",
		"-y", out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("ffmpeg failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// NeedsConversion reports whether a file must be converted before it can be
// sent as a voice note.
func NeedsConversion(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext != ".ogg" && ext != ".opus"
}
