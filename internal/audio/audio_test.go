package audio

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPlaceholderWaveform(t *testing.T) {
	w := PlaceholderWaveform(12)
	if len(w) != 64 {
		t.Fatalf("len = %d", len(w))
	}
	for _, v := range w {
		if v > 100 {
			t.Fatalf("value out of range: %d", v)
		}
	}
	if string(PlaceholderWaveform(12)) != string(w) {
		t.Fatal("waveform should be deterministic")
	}
}

func TestAnalyzeRejectsNonOgg(t *testing.T) {
	if _, _, err := AnalyzeOggOpus([]byte("RIFF....")); err == nil {
		t.Fatal("expected error")
	}
}

func TestNeedsConversion(t *testing.T) {
	if NeedsConversion("a.ogg") || NeedsConversion("b.OPUS") || !NeedsConversion("c.mp3") {
		t.Fatal("NeedsConversion")
	}
}

func TestConvertAndAnalyze(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	wav := filepath.Join(dir, "tone.wav")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=3", "-y", wav).CombinedOutput(); err != nil {
		t.Fatalf("make wav: %v %s", err, out)
	}
	ogg, err := ConvertToOpus(context.Background(), wav)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(ogg)
	data, err := os.ReadFile(ogg)
	if err != nil {
		t.Fatal(err)
	}
	secs, wave, err := AnalyzeOggOpus(data)
	if err != nil {
		t.Fatal(err)
	}
	if secs != 3 || len(wave) != 64 {
		t.Fatalf("duration = %d (want 3), waveform %d bytes", secs, len(wave))
	}
}
