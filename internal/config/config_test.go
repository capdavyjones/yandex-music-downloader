package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMissingDefaultFileReturnsDefaults(t *testing.T) {
	t.Chdir(t.TempDir())

	cfg, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, Default(), cfg)
	assert.Equal(t, DefaultFormat, cfg.Format)
	assert.Empty(t, cfg.DownloadDir)
	assert.Equal(t, DefaultDuplicates, cfg.Duplicates)
	assert.False(t, cfg.Layout.ByArtist)
	assert.False(t, cfg.Layout.ByAlbum)
	assert.False(t, cfg.Layout.ByPlaylist)
	assert.Equal(t, DefaultAlbumMask, cfg.Layout.AlbumDirMask)
	assert.Equal(t, DefaultTrackMask, cfg.Layout.TrackFileMask)
}

func TestLoadMissingExplicitFileReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config")
}

func TestLoadParsesLayoutFlagsAndMask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
layout:
  by_artist: true
  by_album: true
  by_playlist: true
  album_dir_mask: "{year} — {title}"
  track_file_mask: "{nn} - {title}"
`), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultFormat, cfg.Format)
	assert.Empty(t, cfg.DownloadDir)
	assert.True(t, cfg.Layout.ByArtist)
	assert.True(t, cfg.Layout.ByAlbum)
	assert.True(t, cfg.Layout.ByPlaylist)
	assert.Equal(t, "{year} — {title}", cfg.Layout.AlbumDirMask)
	assert.Equal(t, "{nn} - {title}", cfg.Layout.TrackFileMask)
}

func TestLoadEmptyMaskWithByAlbumDefaultsToTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
layout:
  by_album: true
  album_dir_mask: ""
`), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "{title}", cfg.Layout.AlbumDirMask)
	assert.Equal(t, DefaultTrackMask, cfg.Layout.TrackFileMask)
}

func TestLoadEmptyTrackMaskDefaultsToArtistTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
layout:
  track_file_mask: ""
`), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultTrackMask, cfg.Layout.TrackFileMask)
}

func TestLoadRejectsUnknownPlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
layout:
  album_dir_mask: "{year} - {name}"
`), 0644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown album_dir_mask placeholder {name}")
}

func TestLoadRejectsUnknownTrackPlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
layout:
  track_file_mask: "{nn} - {name}"
`), 0644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown track_file_mask placeholder {name}")
}

func TestLoadParsesFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("format: FLAC\n"), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "flac", cfg.Format)
}

func TestLoadEmptyFormatDefaultsToMP3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("format: \"\"\n"), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DefaultFormat, cfg.Format)
}

func TestLoadParsesDownloadDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("download_dir: \" ./music \"\n"), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "./music", cfg.DownloadDir)
}

func TestLoadLeavesDownloadDirEmptyWhenUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("download_dir: \"\"\n"), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.DownloadDir)
}

func TestLoadRejectsUnknownFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("format: aac\n"), 0644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "format must be mp3 or flac")
}

func TestLoadEmptyDuplicatesDefaultsToID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("duplicates: \"\"\n"), 0644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, DuplicatesID, cfg.Duplicates)
}

func TestLoadParsesDuplicates(t *testing.T) {
	tests := []struct {
		raw  string
		want Duplicates
	}{
		{raw: "id", want: DuplicatesID},
		{raw: "NONE", want: DuplicatesNone},
		{raw: " Copy ", want: DuplicatesCopy},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "yamdl.yaml")
			require.NoError(t, os.WriteFile(path, []byte("duplicates: "+tt.raw+"\n"), 0644))

			cfg, err := Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Duplicates)
		})
	}
}

func TestLoadRejectsUnknownDuplicates(t *testing.T) {
	tests := []string{"auto", "title", "skip"}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "yamdl.yaml")
			require.NoError(t, os.WriteFile(path, []byte("duplicates: "+raw+"\n"), 0644))

			_, err := Load(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "duplicates must be id, none or copy")
		})
	}
}

func TestDuplicatesHelpers(t *testing.T) {
	assert.Equal(t, DuplicatesID, Duplicates("").OrDefault())
	assert.True(t, DuplicatesID.MarksDuplicates())
	assert.False(t, DuplicatesNone.MarksDuplicates())
	assert.True(t, DuplicatesCopy.MarksDuplicates())
	assert.True(t, DuplicatesCopy.CopiesSharedID())
	assert.False(t, DuplicatesID.CopiesSharedID())
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yamdl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("layout: [broken"), 0644))

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config")
}
