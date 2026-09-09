package ya

import (
	"os"
	"path/filepath"
	"testing"
	"ya-music/utils"
	"ya-music/ya/model"

	"github.com/bogem/id3v2/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneTaggedTrackRewritesAlbumTags(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "album-one")
	destDir := filepath.Join(root, "album-two")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.MkdirAll(destDir, 0755))

	src := filepath.Join(srcDir, "Artist - Song.mp3")
	require.NoError(t, os.WriteFile(src, []byte("audio payload"), 0644))
	require.NoError(t, writeID3Tags(src, model.Track{
		ID:      model.FlexibleID("123"),
		Title:   "Song",
		Artists: []model.Artist{{Name: "Artist"}},
		Albums:  []model.Album{{Title: "Album One", Year: 1975, TrackPosition: model.TrackPosition{Index: 1}}},
	}, ""))

	client := NewClient(utils.NewHttpClient())
	destTrack := model.Track{
		ID:      model.FlexibleID("123"),
		Title:   "Song",
		Artists: []model.Artist{{Name: "Artist"}},
		Albums:  []model.Album{{Title: "Album Two", Year: 1992, TrackPosition: model.TrackPosition{Index: 3}}},
	}

	dest, err := client.CloneTaggedTrack(src, destDir, destTrack, DownloadOptions{SkipCover: true})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(destDir, "Artist - Song.mp3"), dest)

	tag, err := id3v2.Open(dest, id3v2.Options{Parse: true})
	require.NoError(t, err)
	defer tag.Close()
	assert.Equal(t, "Song", tag.Title())
	assert.Equal(t, "Album Two", tag.Album())
	assert.Equal(t, "1992", tag.Year())
	assert.Equal(t, "3", tag.GetTextFrame(tag.CommonID("Track number/Position in set")).Text)
}

func TestCloneTaggedTrackSameDestinationIsDuplicate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Artist - Song.mp3")
	require.NoError(t, os.WriteFile(src, []byte("audio payload"), 0644))

	client := NewClient(utils.NewHttpClient())
	_, err := client.CloneTaggedTrack(src, dir, model.Track{
		Title:   "Song",
		Artists: []model.Artist{{Name: "Artist"}},
	}, DownloadOptions{SkipCover: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateDestination)
}

func TestCloneTaggedTrackExistingDestinationIsAlreadyExists(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "one")
	destDir := filepath.Join(root, "two")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.MkdirAll(destDir, 0755))

	src := filepath.Join(srcDir, "Artist - Song.mp3")
	dest := filepath.Join(destDir, "Artist - Song.mp3")
	require.NoError(t, os.WriteFile(src, []byte("source"), 0644))
	require.NoError(t, os.WriteFile(dest, []byte("existing"), 0644))

	client := NewClient(utils.NewHttpClient())
	got, err := client.CloneTaggedTrack(src, destDir, model.Track{
		Title:   "Song",
		Artists: []model.Artist{{Name: "Artist"}},
	}, DownloadOptions{SkipCover: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTrackAlreadyExists)
	assert.Equal(t, dest, got)
}

func TestCloneTaggedTrackRejectsUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "track.wav")
	require.NoError(t, os.WriteFile(src, []byte("audio"), 0644))

	client := NewClient(utils.NewHttpClient())
	_, err := client.CloneTaggedTrack(src, t.TempDir(), model.Track{Title: "Song"}, DownloadOptions{SkipCover: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported audio extension")
}
