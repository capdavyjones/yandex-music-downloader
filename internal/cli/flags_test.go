package cli

import (
	"bytes"
	"strings"
	"testing"
	"ya-music/ya"
)

func TestParseTUIOptionsRejectsPositionalArgs(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseTUIOptions([]string{"download"}, &stderr)
	if parsed.proceed || parsed.exitCode != 2 {
		t.Fatalf("proceed = %v, exit code = %d, want 2, stderr: %s", parsed.proceed, parsed.exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unexpected command; use 'yamdl download --help' for batch downloads") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestParseTUIOptionsAcceptsTimeoutAndSkipCover(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseTUIOptions([]string{"--timeout", "30", "--skip-cover"}, &stderr)
	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	if parsed.options.timeoutSeconds != 30 || !parsed.options.skipCover {
		t.Fatalf("unexpected options: %#v", parsed.options)
	}
}

func TestParseTUIOptionsRejectsNegativeTimeout(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseTUIOptions([]string{"--timeout", "-1"}, &stderr)
	if parsed.proceed || parsed.exitCode != 2 {
		t.Fatalf("proceed = %v, exit code = %d, stderr: %s", parsed.proceed, parsed.exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "timeout must be >= 0 seconds") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestParseDownloadOptionsRejectsNegativeTimeout(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "abc123",
		"--link", "https://music.yandex.ru/album/123",
		"--timeout", "-1",
	}, &stderr)
	if parsed.proceed || parsed.exitCode != 2 {
		t.Fatalf("proceed = %v, exit code = %d, stderr: %s", parsed.proceed, parsed.exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "timeout must be >= 0 seconds") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestParseDownloadOptionsAcceptsCurrentDownloadFlags(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "  abc123  ",
		"--link", "  https://music.yandex.ru/album/123  ",
		"--format", "FLAC",
		"--output", "./music",
		"--timeout", "180",
		"--skip-cover",
	}, &stderr)

	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	options := parsed.options
	if options.token != "abc123" || options.link != "https://music.yandex.ru/album/123" {
		t.Fatalf("unexpected required options: %#v", options)
	}
	if options.format != ya.AudioFormatFLAC || options.output != "./music" || options.timeoutSeconds != 180 || !options.skipCover {
		t.Fatalf("unexpected parsed options: %#v", options)
	}
}

func TestParseDownloadOptionsAcceptsConfigAndArtistLink(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "abc123",
		"--link", "https://music.yandex.ru/artist/170660",
		"--config", "./yamdl.yaml",
	}, &stderr)
	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	if parsed.options.configPath != "./yamdl.yaml" {
		t.Fatalf("configPath = %q", parsed.options.configPath)
	}
	if parsed.options.link != "https://music.yandex.ru/artist/170660" {
		t.Fatalf("link = %q", parsed.options.link)
	}
	if parsed.options.format != "" {
		t.Fatalf("format = %q, want empty so config can apply", parsed.options.format)
	}
	if parsed.options.output != "" {
		t.Fatalf("output = %q, want empty so config can apply", parsed.options.output)
	}
}

func TestParseDownloadOptionsOmitsFormatWhenUnset(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "abc123",
		"--link", "https://music.yandex.ru/album/123",
	}, &stderr)
	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	if parsed.options.format != "" {
		t.Fatalf("format = %q, want empty", parsed.options.format)
	}
	if parsed.options.output != "" {
		t.Fatalf("output = %q, want empty", parsed.options.output)
	}
}

func TestResolveAudioFormatPrefersFlagOverConfig(t *testing.T) {
	if got := resolveAudioFormat(ya.AudioFormatMP3, "flac"); got != ya.AudioFormatMP3 {
		t.Fatalf("got %q, want mp3 flag to override config", got)
	}
	if got := resolveAudioFormat("", "flac"); got != ya.AudioFormatFLAC {
		t.Fatalf("got %q, want flac from config", got)
	}
	if got := resolveAudioFormat("", ""); got != ya.AudioFormatMP3 {
		t.Fatalf("got %q, want mp3 default", got)
	}
}

func TestResolveOutputPrefersFlagOverConfig(t *testing.T) {
	t.Setenv("YAMDL_DOWNLOAD_DIR", "")
	if got := resolveOutput("./music", "./from-config"); got != "./music" {
		t.Fatalf("got %q, want flag to override config", got)
	}
	if got := resolveOutput("", "./from-config"); got != "./from-config" {
		t.Fatalf("got %q, want path from config", got)
	}
	if got := resolveOutput("", ""); got != "./downloads" {
		t.Fatalf("got %q, want default downloads dir", got)
	}
}

func TestParseTUIOptionsAcceptsConfigPath(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseTUIOptions([]string{"--config", "custom.yaml"}, &stderr)
	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	if parsed.options.configPath != "custom.yaml" {
		t.Fatalf("configPath = %q", parsed.options.configPath)
	}
}

func TestParseDownloadOptionsRejectsInvalidInput(t *testing.T) {
	tests := [][]string{
		{"--link", "https://music.yandex.ru/album/123"},
		{"--token", "abc123"},
		{"--token", "abc123", "--link", "https://music.yandex.ru/album/123", "--format", "aac"},
		{"--token", "abc123", "--link", "https://music.yandex.ru/album/123", "--timeout", "-1"},
	}

	for _, args := range tests {
		var stderr bytes.Buffer
		parsed := parseDownloadOptions(args, &stderr)
		if parsed.proceed || parsed.exitCode != 2 {
			t.Fatalf("args %q: proceed = %v, exit code = %d, stderr: %s", args, parsed.proceed, parsed.exitCode, stderr.String())
		}
	}
}

func TestParseDownloadOptionsRejectsPositionalArgs(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "abc",
		"--link", "https://music.yandex.ru/album/1",
		"extra",
	}, &stderr)
	if parsed.proceed || parsed.exitCode != 2 {
		t.Fatalf("proceed = %v, exit code = %d, stderr: %s", parsed.proceed, parsed.exitCode, stderr.String())
	}
}

func TestResolveOutputUsesEnvVariableOverConfig(t *testing.T) {
	t.Setenv("YAMDL_DOWNLOAD_DIR", "/env/download/path")

	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "abc123",
		"--link", "https://music.yandex.ru/album/123",
	}, &stderr)

	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	if got := resolveOutput(parsed.options.output, "./from-config"); got != "/env/download/path" {
		t.Fatalf("expected output = %q, got %q", "/env/download/path", got)
	}
}

func TestParseDownloadOptionsFlagOverridesEnv(t *testing.T) {
	t.Setenv("YAMDL_DOWNLOAD_DIR", "/env/download/path")

	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "abc123",
		"--link", "https://music.yandex.ru/album/123",
		"--output", "/flag/download/path",
	}, &stderr)

	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	if got := resolveOutput(parsed.options.output, "./from-config"); got != "/flag/download/path" {
		t.Fatalf("expected output = %q, got %q", "/flag/download/path", got)
	}
}

func TestParseDownloadOptionsAcceptsArtistLink(t *testing.T) {
	var stderr bytes.Buffer
	parsed := parseDownloadOptions([]string{
		"--token", "abc123",
		"--link", "https://music.yandex.ru/artist/170660",
	}, &stderr)
	if !parsed.proceed {
		t.Fatalf("expected proceed, exit code = %d, stderr: %s", parsed.exitCode, stderr.String())
	}
	if parsed.options.link != "https://music.yandex.ru/artist/170660" {
		t.Fatalf("link = %q", parsed.options.link)
	}
}
