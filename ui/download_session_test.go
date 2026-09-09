package ui

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"ya-music/internal/config"
	"ya-music/utils"
	"ya-music/ya"
	"ya-music/ya/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingDownloadClient struct {
	dir     string
	options ya.DownloadOptions
}

type fakeDownloadClient struct {
	filename string
	err      error
}

func (f *fakeDownloadClient) DownloadTrackWithOptions(
	model.Track,
	string,
	ya.DownloadOptions,
) (string, error) {
	return f.filename, f.err
}

func (c *recordingDownloadClient) DownloadTrackWithOptions(
	_ model.Track,
	outputDir string,
	options ya.DownloadOptions,
) (string, error) {
	c.dir = outputDir
	c.options = options
	return filepath.Join(outputDir, "Song.mp3"), nil
}

func TestDownloadSessionUsesLayoutDirectory(t *testing.T) {
	client := &recordingDownloadClient{}
	outputDir := t.TempDir()
	session := NewDownloadSession(
		client,
		utils.NewDiscardDownloadLogger(),
		ya.DownloadOptions{},
		outputDir,
		SessionConfig{
			Settings: config.Config{
				Layout: config.Layout{
					ByArtist:     true,
					ByAlbum:      true,
					AlbumDirMask: "{year} - {title}",
				},
			},
			FolderArtist: "ABBA",
		},
	)

	var final DownloadSessionEvent
	for event := range session.Run([]TrackProgress{{
		uid: "track-1",
		track: &model.Track{
			ID:        model.FlexibleID("1"),
			Title:     "Mamma Mia",
			Artists:   []model.Artist{{Name: "ABBA"}},
			Available: true,
			Albums:    []model.Album{{Title: "ABBA", Year: 1975}},
		},
		status: TrackStatusReady,
	}}) {
		if event.Completed {
			final = event
		}
	}

	wantDir := filepath.Join(outputDir, "ABBA", "1975 - ABBA")
	assert.Equal(t, TrackStatusDownloaded, final.Progress.status)
	assert.Equal(t, wantDir, client.dir)
	assert.Equal(t, filepath.Join(wantDir, "Song.mp3"), final.Progress.filename)
}

func TestDownloadSessionUsesFlatPlaylistDirectory(t *testing.T) {
	client := &recordingDownloadClient{}
	outputDir := t.TempDir()
	session := NewDownloadSession(
		client,
		utils.NewDiscardDownloadLogger(),
		ya.DownloadOptions{},
		outputDir,
		SessionConfig{
			Settings: config.Config{
				Layout: config.Layout{
					ByArtist:     true,
					ByAlbum:      true,
					ByPlaylist:   true,
					AlbumDirMask: "{year} - {title}",
				},
			},
			FolderArtist:   "ABBA",
			FolderPlaylist: "Favorites",
		},
	)

	var final DownloadSessionEvent
	for event := range session.Run([]TrackProgress{{
		uid: "track-1",
		track: &model.Track{
			ID:        model.FlexibleID("1"),
			Title:     "Mamma Mia",
			Artists:   []model.Artist{{Name: "ABBA"}},
			Available: true,
			Albums:    []model.Album{{Title: "ABBA", Year: 1975}},
		},
		status: TrackStatusReady,
	}}) {
		if event.Completed {
			final = event
		}
	}

	wantDir := filepath.Join(outputDir, "Favorites")
	assert.Equal(t, TrackStatusDownloaded, final.Progress.status)
	assert.Equal(t, wantDir, client.dir)
	assert.Equal(t, filepath.Join(wantDir, "Song.mp3"), final.Progress.filename)
}

func TestDownloadSessionPassesMaskedFilenameBase(t *testing.T) {
	client := &recordingDownloadClient{}
	outputDir := t.TempDir()
	session := NewDownloadSession(
		client,
		utils.NewDiscardDownloadLogger(),
		ya.DownloadOptions{},
		outputDir,
		SessionConfig{
			Settings: config.Config{
				Layout: config.Layout{TrackFileMask: "{nn} - {title}"},
			},
		},
	)

	track := &model.Track{
		ID:        model.FlexibleID("1"),
		Title:     "Mamma Mia",
		Artists:   []model.Artist{{Name: "ABBA"}},
		Available: true,
		Albums:    []model.Album{{Title: "Arrival", Year: 1975, TrackPosition: model.TrackPosition{Index: 1}}},
	}
	for event := range session.Run([]TrackProgress{{
		uid:    "track-1",
		track:  track,
		status: TrackStatusReady,
	}}) {
		if event.Completed {
			break
		}
	}

	assert.Equal(t, "01 - Mamma Mia", client.options.FilenameBase)
}

func TestDownloadSessionEmitsSnapshotsWithoutMutatingInput(t *testing.T) {
	input := []TrackProgress{{
		uid: "track-1", track: &model.Track{ID: model.FlexibleID("1"), Title: "Song"}, status: TrackStatusReady,
	}}
	session := NewDownloadSession(
		&fakeDownloadClient{filename: "downloads/Song.mp3"},
		utils.NewDownloadLoggerForWriter(io.Discard),
		ya.DownloadOptions{},
		t.TempDir(),
	)

	var events []DownloadSessionEvent
	for event := range session.Run(input) {
		events = append(events, event)
	}

	require.Len(t, events, 2)
	assert.Equal(t, TrackStatusReady, input[0].status)
	assert.Equal(t, TrackStatusDownloading, events[0].Progress.status)
	assert.Equal(t, TrackStatusDownloaded, events[1].Progress.status)
	assert.True(t, events[1].Completed)
}

func TestDownloadSessionTurnsClientErrorIntoCompletedErrorEvent(t *testing.T) {
	session := NewDownloadSession(
		&fakeDownloadClient{err: context.Canceled},
		utils.NewDownloadLoggerForWriter(io.Discard),
		ya.DownloadOptions{},
		t.TempDir(),
	)

	var final DownloadSessionEvent
	for event := range session.Run([]TrackProgress{{
		uid: "track-1", track: &model.Track{ID: model.FlexibleID("1"), Title: "Song"}, status: TrackStatusReady,
	}}) {
		if event.Completed {
			final = event
		}
	}

	assert.Equal(t, TrackStatusError, final.Progress.status)
	assert.Contains(t, final.Progress.errMsg, "context canceled")
}

type blockingDownloadClient struct {
	started chan struct{}
	release chan struct{}
	path    string
}

func (c *blockingDownloadClient) DownloadTrackWithOptions(
	model.Track,
	string,
	ya.DownloadOptions,
) (string, error) {
	close(c.started)
	<-c.release
	return c.path, nil
}

func TestDownloadSessionClosesOnlyAfterWorkersFinish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := &blockingDownloadClient{started: started, release: release, path: "song.mp3"}
	session := NewDownloadSession(client, utils.NewDiscardDownloadLogger(), ya.DownloadOptions{}, t.TempDir())
	events := session.Run([]TrackProgress{{
		uid: "track-1", track: &model.Track{ID: model.FlexibleID("1"), Title: "Song"}, status: TrackStatusReady,
	}})

	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("session closed before worker started")
		}
		if event.Progress.status != TrackStatusDownloading {
			t.Fatalf("first event status = %v, want Downloading", event.Progress.status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Downloading event")
	}

	<-started
	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("session closed before worker release")
		}
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	for range events {
	}
}

func TestDownloadSessionLogsSkippedTracks(t *testing.T) {
	var logs bytes.Buffer
	session := NewDownloadSession(
		&fakeDownloadClient{},
		utils.NewDownloadLoggerForWriter(&logs),
		ya.DownloadOptions{},
		t.TempDir(),
	)

	for range session.Run([]TrackProgress{
		{uid: "duplicate", track: &model.Track{ID: model.FlexibleID("1"), Title: "Duplicate"}, status: TrackStatusDuplicate},
		{uid: "unavailable", track: &model.Track{ID: model.FlexibleID("2"), Title: "Unavailable"}, status: TrackStatusNotAvailable},
	}) {
	}

	assert.Contains(t, logs.String(), "reason=duplicate")
	assert.Contains(t, logs.String(), "reason=not_available")
	assert.Contains(t, logs.String(), "download session finished")
}

type panicDownloadClient struct{}

func (panicDownloadClient) DownloadTrackWithOptions(
	model.Track,
	string,
	ya.DownloadOptions,
) (string, error) {
	panic("download failure")
}

func TestDownloadSessionConvertsWorkerPanicToCompletedErrorEvent(t *testing.T) {
	session := NewDownloadSession(
		panicDownloadClient{},
		utils.NewDiscardDownloadLogger(),
		ya.DownloadOptions{},
		t.TempDir(),
	)

	var events []DownloadSessionEvent
	for event := range session.Run([]TrackProgress{{
		uid: "track-1", track: &model.Track{ID: model.FlexibleID("1"), Title: "Song"}, status: TrackStatusReady,
	}}) {
		events = append(events, event)
	}

	require.Len(t, events, 2)
	assert.Equal(t, TrackStatusError, events[1].Progress.status)
	assert.True(t, events[1].Completed)
	assert.Contains(t, events[1].Progress.errMsg, "download failure")
}

type cloneRecordingDownloadClient struct {
	mu          sync.Mutex
	downloads   int
	clones      int
	downloadDir string
	cloneDir    string
}

func (c *cloneRecordingDownloadClient) DownloadTrackWithOptions(
	track model.Track,
	outputDir string,
	_ ya.DownloadOptions,
) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.downloads++
	c.downloadDir = outputDir
	return filepath.Join(outputDir, track.ID.String()+".mp3"), nil
}

func (c *cloneRecordingDownloadClient) CloneTaggedTrack(
	src string,
	destDir string,
	track model.Track,
	_ ya.DownloadOptions,
) (string, error) {
	dest := filepath.Join(destDir, track.ID.String()+".mp3")
	if filepath.Clean(src) == filepath.Clean(dest) {
		return dest, ya.ErrDuplicateDestination
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clones++
	c.cloneDir = destDir
	return dest, nil
}

func TestDownloadSessionCopyClonesIntoOtherAlbumDirectory(t *testing.T) {
	client := &cloneRecordingDownloadClient{}
	outputDir := t.TempDir()
	session := NewDownloadSession(
		client,
		utils.NewDiscardDownloadLogger(),
		ya.DownloadOptions{},
		outputDir,
		SessionConfig{
			Settings: config.Config{
				Layout:     config.Layout{ByAlbum: true, AlbumDirMask: "{title}"},
				Duplicates: config.DuplicatesCopy,
			},
		},
	)

	var completed []TrackProgress
	for event := range session.Run([]TrackProgress{
		{
			uid: "one",
			track: &model.Track{
				ID: model.FlexibleID("1"), Title: "Song", Available: true,
				Albums: []model.Album{{Title: "Album One"}},
			},
			status: TrackStatusReady,
		},
		{
			uid: "two",
			track: &model.Track{
				ID: model.FlexibleID("1"), Title: "Song", Available: true,
				Albums: []model.Album{{Title: "Album Two"}},
			},
			status: TrackStatusReady,
		},
	}) {
		if event.Completed {
			completed = append(completed, event.Progress)
		}
	}

	require.Len(t, completed, 2)
	assert.Equal(t, 1, client.downloads)
	assert.Equal(t, 1, client.clones)
	assert.NotEqual(t, client.downloadDir, client.cloneDir)
	assert.ElementsMatch(t, []string{
		filepath.Join(outputDir, "Album One"),
		filepath.Join(outputDir, "Album Two"),
	}, []string{client.downloadDir, client.cloneDir})
	assert.Equal(t, TrackStatusDownloaded, completed[0].status)
	assert.Equal(t, TrackStatusDownloaded, completed[1].status)
}

func TestDownloadSessionCopyMarksSameDestinationDuplicate(t *testing.T) {
	client := &cloneRecordingDownloadClient{}
	session := NewDownloadSession(
		client,
		utils.NewDiscardDownloadLogger(),
		ya.DownloadOptions{},
		t.TempDir(),
		SessionConfig{Settings: config.Config{Duplicates: config.DuplicatesCopy}},
	)

	var completed []TrackProgress
	track := &model.Track{ID: model.FlexibleID("1"), Title: "Song", Available: true}
	for event := range session.Run([]TrackProgress{
		{uid: "one", track: track, status: TrackStatusReady},
		{uid: "two", track: track, status: TrackStatusReady},
	}) {
		if event.Completed {
			completed = append(completed, event.Progress)
		}
	}

	require.Len(t, completed, 2)
	assert.Equal(t, 1, client.downloads)
	assert.Equal(t, 0, client.clones)
	statuses := []TrackStatus{completed[0].status, completed[1].status}
	assert.Contains(t, statuses, TrackStatusDownloaded)
	assert.Contains(t, statuses, TrackStatusDuplicate)
}
