package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPath                  = "yamdl.yaml"
	DefaultAlbumMask             = "{year} - {title}"
	DefaultTrackMask             = "{artist} - {title}"
	DefaultFormat                = "mp3"
	DefaultDuplicates            = DuplicatesID
	DefaultDownloadRetries       = 0
	DefaultDownloadRetryInterval = 10
)

// Duplicates задаёт, как обрабатывать повтор одного и того же ID трека.
type Duplicates string

const (
	DuplicatesID   Duplicates = "id"
	DuplicatesNone Duplicates = "none"
	DuplicatesCopy Duplicates = "copy"
)

var placeholderPattern = regexp.MustCompile(`\{[^{}]+\}`)

var albumPlaceholders = map[string]struct{}{
	"{year}":  {},
	"{title}": {},
}

var trackPlaceholders = map[string]struct{}{
	"{n}":      {},
	"{nn}":     {},
	"{year}":   {},
	"{album}":  {},
	"{title}":  {},
	"{artist}": {},
}

// Config описывает настройки приложения из YAML.
type Config struct {
	Format                string     `yaml:"format"`
	DownloadDir           string     `yaml:"download_dir"`
	Duplicates            Duplicates `yaml:"duplicates"`
	DownloadRetries       int        `yaml:"download_retries"`
	DownloadRetryInterval int        `yaml:"download_retry_interval"`
	AlbumArtistFirst      bool       `yaml:"album_artist_first"`
	Layout                Layout     `yaml:"layout"`
}

// Layout задаёт раскладку скачанных файлов по исполнителям и релизам.
type Layout struct {
	ByArtist      bool   `yaml:"by_artist"`
	ByAlbum       bool   `yaml:"by_album"`
	ByPlaylist    bool   `yaml:"by_playlist"`
	AlbumDirMask  string `yaml:"album_dir_mask"`
	TrackFileMask string `yaml:"track_file_mask"`
}

func Default() Config {
	return Config{
		Format:                DefaultFormat,
		Duplicates:            DefaultDuplicates,
		DownloadRetries:       DefaultDownloadRetries,
		DownloadRetryInterval: DefaultDownloadRetryInterval,
		Layout: Layout{
			AlbumDirMask:  DefaultAlbumMask,
			TrackFileMask: DefaultTrackMask,
		},
	}
}

// Load читает YAML. Пустой path означает DefaultPath; отсутствие этого файла
// не считается ошибкой и возвращает значения по умолчанию.
func Load(path string) (Config, error) {
	explicit := strings.TrimSpace(path) != ""
	if !explicit {
		path = DefaultPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("failed to read config %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to parse config %s: %w", path, err)
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) normalize() error {
	if err := c.normalizeFormat(); err != nil {
		return err
	}
	c.DownloadDir = strings.TrimSpace(c.DownloadDir)
	if err := c.normalizeDuplicates(); err != nil {
		return err
	}
	if err := c.normalizeDownloadRetries(); err != nil {
		return err
	}
	return c.Layout.normalize()
}

func (c *Config) normalizeDownloadRetries() error {
	if c.DownloadRetries < 0 {
		return fmt.Errorf("download_retries must be >= 0")
	}
	// 0 после Unmarshal означает явный ноль в YAML — интервал должен быть положительным.
	if c.DownloadRetryInterval <= 0 {
		return fmt.Errorf("download_retry_interval must be > 0")
	}
	return nil
}

func (c *Config) normalizeFormat() error {
	c.Format = strings.ToLower(strings.TrimSpace(c.Format))
	if c.Format == "" {
		c.Format = DefaultFormat
		return nil
	}
	if c.Format != "mp3" && c.Format != "flac" {
		return fmt.Errorf("format must be mp3 or flac")
	}
	return nil
}

func (c *Config) normalizeDuplicates() error {
	value := Duplicates(strings.ToLower(strings.TrimSpace(string(c.Duplicates))))
	if value == "" {
		c.Duplicates = DefaultDuplicates
		return nil
	}
	switch value {
	case DuplicatesID, DuplicatesNone, DuplicatesCopy:
		c.Duplicates = value
		return nil
	default:
		return fmt.Errorf("duplicates must be id, none or copy")
	}
}

// OrDefault возвращает id, если политика не задана.
func (d Duplicates) OrDefault() Duplicates {
	if d == "" {
		return DefaultDuplicates
	}
	return d
}

// MarksDuplicates сообщает, нужно ли заранее помечать повторы ID как дубликаты.
// Для id повторные вхождения пропускаются; для copy они остаются в очереди на копирование.
func (d Duplicates) MarksDuplicates() bool {
	switch d.OrDefault() {
	case DuplicatesID, DuplicatesCopy:
		return true
	default:
		return false
	}
}

// CopiesSharedID сообщает, нужно ли копировать уже скачанный файл с тем же ID.
func (d Duplicates) CopiesSharedID() bool {
	return d.OrDefault() == DuplicatesCopy
}

func (l *Layout) normalize() error {
	l.AlbumDirMask = strings.TrimSpace(l.AlbumDirMask)
	if l.AlbumDirMask == "" {
		if l.ByAlbum {
			l.AlbumDirMask = "{title}"
		} else {
			l.AlbumDirMask = DefaultAlbumMask
		}
	}
	l.TrackFileMask = strings.TrimSpace(l.TrackFileMask)
	if l.TrackFileMask == "" {
		l.TrackFileMask = DefaultTrackMask
	}
	if err := validateMask("album_dir_mask", l.AlbumDirMask, albumPlaceholders); err != nil {
		return err
	}
	return validateMask("track_file_mask", l.TrackFileMask, trackPlaceholders)
}

func validateMask(field, mask string, allowed map[string]struct{}) error {
	for _, match := range placeholderPattern.FindAllString(mask, -1) {
		if _, ok := allowed[match]; !ok {
			return fmt.Errorf("unknown %s placeholder %s", field, match)
		}
	}
	return nil
}
