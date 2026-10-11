package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
	"ya-music/internal/batch"
	"ya-music/internal/config"
	"ya-music/internal/layout"
	"ya-music/utils"
	"ya-music/ya"
	"ya-music/ya/model"
)

// TrackProgress represents the download progress and state of a track.
type TrackProgress struct {
	uid      string
	track    *model.Track
	status   TrackStatus
	errMsg   string
	filename string
	format   string
}

type downloadTrackClient interface {
	DownloadTrackWithOptions(model.Track, string, ya.DownloadOptions) (string, error)
}

type trackCloner interface {
	CloneTaggedTrack(string, string, model.Track, ya.DownloadOptions) (string, error)
}

// DownloadRetryWait описывает паузу перед следующей попыткой скачивания.
type DownloadRetryWait struct {
	Remaining int
	Until     time.Time
}

type DownloadSessionEvent struct {
	Progress  TrackProgress
	Completed bool
	// RetryWait не nil во время паузы между попытками; Remaining == 0 означает сброс.
	RetryWait *DownloadRetryWait
}

type DownloadSession struct {
	client         downloadTrackClient
	logger         *utils.DownloadLogger
	options        ya.DownloadOptions
	outputDir      string
	concurrency    int
	settings       config.Config
	folderArtist   string
	folderPlaylist string
}

// SessionConfig передаёт в сессию настройки из YAML и папки, вычисленные по источнику.
type SessionConfig struct {
	Settings       config.Config
	FolderArtist   string
	FolderPlaylist string
}

func NewDownloadSession(
	client downloadTrackClient,
	logger *utils.DownloadLogger,
	options ya.DownloadOptions,
	outputDir string,
	sessionConfig ...SessionConfig,
) *DownloadSession {
	if logger == nil {
		logger = utils.NewDiscardDownloadLogger()
	}
	cfg := SessionConfig{}
	if len(sessionConfig) > 0 {
		cfg = sessionConfig[0]
	}

	settings := cfg.Settings
	settings.Duplicates = settings.Duplicates.OrDefault()
	if settings.DownloadRetryInterval <= 0 {
		settings.DownloadRetryInterval = config.DefaultDownloadRetryInterval
	}

	return &DownloadSession{
		client:         client,
		logger:         logger,
		options:        options,
		outputDir:      outputDir,
		concurrency:    maxConcurrentDownloads,
		settings:       settings,
		folderArtist:   cfg.FolderArtist,
		folderPlaylist: cfg.FolderPlaylist,
	}
}

func (s *DownloadSession) Run(progress []TrackProgress) <-chan DownloadSessionEvent {
	events := make(chan DownloadSessionEvent)
	work := append([]TrackProgress(nil), progress...)

	go func() {
		defer close(events)
		s.logger.Info("download session started",
			"total_tracks", len(work),
			"max_concurrent_downloads", s.concurrency,
			"format", s.options.FormatOrDefault(),
			"duplicates", string(s.settings.Duplicates),
			"download_retries", s.settings.DownloadRetries,
			"download_retry_interval_sec", s.settings.DownloadRetryInterval,
		)

		published := batch.NewPublishedFiles()
		cloner, _ := s.client.(trackCloner)
		var wg sync.WaitGroup
		sem := make(chan struct{}, s.concurrency)
		for _, item := range work {
			if reason, shouldSkip := skipDownloadReason(item.status, s.settings.Duplicates); shouldSkip {
				s.logger.LogTrack(slog.LevelInfo, utils.NewTrackLogContext(*item.track), "skipped",
					"stage", "queue",
					"reason", reason,
				)
				continue
			}

			item := item
			wg.Add(1)
			go s.runTrack(item, &wg, sem, events, published, cloner)
		}

		wg.Wait()
		s.logger.Info("download session finished")
	}()

	return events
}

func (s *DownloadSession) runTrack(
	item TrackProgress,
	wg *sync.WaitGroup,
	sem chan struct{},
	events chan<- DownloadSessionEvent,
	published *batch.PublishedFiles,
	cloner trackCloner,
) {
	defer wg.Done()
	trackCtx := utils.NewTrackLogContext(*item.track)

	id := item.track.ID.String()
	var publishedFile *batch.PublishedFile
	leader := false
	if s.settings.Duplicates.CopiesSharedID() && id != "" {
		publishedFile, leader = published.Claim(id)
	}

	defer func() {
		if r := recover(); r != nil {
			if leader {
				publishedFile.Complete("")
			}
			item.status = TrackStatusError
			item.errMsg = fmt.Sprintf("panic: %v", r)
			s.logger.LogTrack(slog.LevelError, trackCtx, "panic recovered",
				"stage", "download_track",
				"error", fmt.Sprintf("%v", r),
				"stack", string(debug.Stack()),
			)
			events <- DownloadSessionEvent{Progress: item, Completed: true}
		}
	}()

	if !leader && publishedFile != nil {
		src := publishedFile.Wait(context.Background())
		sem <- struct{}{}
		defer func() { <-sem }()
		if src != "" && cloner != nil {
			s.finishClone(item, src, trackCtx, events, cloner)
			return
		}
		s.finishDownload(item, trackCtx, events, nil, false)
		return
	}

	sem <- struct{}{}
	defer func() { <-sem }()
	s.finishDownload(item, trackCtx, events, publishedFile, leader)
}

func (s *DownloadSession) finishClone(
	item TrackProgress,
	src string,
	trackCtx utils.TrackLogContext,
	events chan<- DownloadSessionEvent,
	cloner trackCloner,
) {
	dir, options, ok := s.prepareOutput(item, trackCtx, events)
	if !ok {
		return
	}

	filePath, err := cloner.CloneTaggedTrack(src, dir, *item.track, options)
	if errors.Is(err, ya.ErrDuplicateDestination) {
		item.status = TrackStatusDuplicate
		item.filename = filePath
		s.logger.LogTrack(slog.LevelInfo, trackCtx, "worker skipped",
			"stage", "clone_track",
			"status", item.status.String(),
			"filename", filePath,
			"reason", "duplicate",
		)
		events <- DownloadSessionEvent{Progress: item, Completed: true}
		return
	}

	item.status = TrackStatusDownloading
	s.logger.LogTrack(slog.LevelInfo, trackCtx, "worker started",
		"stage", "clone_track",
	)
	events <- DownloadSessionEvent{Progress: item}
	s.applyFileResult(item, trackCtx, events, filePath, err)
}

func (s *DownloadSession) finishDownload(
	item TrackProgress,
	trackCtx utils.TrackLogContext,
	events chan<- DownloadSessionEvent,
	publishedFile *batch.PublishedFile,
	leader bool,
) {
	item.status = TrackStatusDownloading
	s.logger.LogTrack(slog.LevelInfo, trackCtx, "worker started",
		"stage", "download_track",
	)
	events <- DownloadSessionEvent{Progress: item}

	dir, options, ok := s.prepareOutput(item, trackCtx, events)
	if !ok {
		if leader {
			publishedFile.Complete("")
		}
		return
	}

	filePath, err := ya.DownloadTrackWithRetries(
		context.Background(),
		s.client,
		*item.track,
		dir,
		options,
		s.settings.DownloadRetries,
		time.Duration(s.settings.DownloadRetryInterval)*time.Second,
		func(remaining int, until time.Time) {
			if remaining > 0 {
				s.logger.LogTrack(slog.LevelInfo, trackCtx, "download retry scheduled",
					"stage", "download_track",
					"retries_left", remaining,
					"retry_in_sec", s.settings.DownloadRetryInterval,
				)
			}
			wait := &DownloadRetryWait{Remaining: remaining, Until: until}
			events <- DownloadSessionEvent{Progress: item, RetryWait: wait}
		},
	)
	if leader {
		if err == nil || errors.Is(err, ya.ErrTrackAlreadyExists) {
			publishedFile.Complete(filePath)
		} else {
			publishedFile.Complete("")
		}
	}
	s.applyFileResult(item, trackCtx, events, filePath, err)
}

func (s *DownloadSession) prepareOutput(
	item TrackProgress,
	trackCtx utils.TrackLogContext,
	events chan<- DownloadSessionEvent,
) (string, ya.DownloadOptions, bool) {
	dir := layout.TrackOutputDir(s.outputDir, *item.track, layout.Options{
		Layout:         s.settings.Layout,
		FolderArtist:   s.folderArtist,
		FolderPlaylist: s.folderPlaylist,
	})
	if err := utils.CreateDirIfNotExists(dir); err != nil {
		item.status = TrackStatusError
		item.errMsg = err.Error()
		s.logger.LogTrack(slog.LevelError, trackCtx, "worker finished with error",
			"stage", "create_output_dir",
			"status", item.status.String(),
			"error", err,
		)
		events <- DownloadSessionEvent{Progress: item, Completed: true}
		return "", ya.DownloadOptions{}, false
	}

	options := s.options
	options.FilenameBase = layout.TrackFilenameBase(*item.track, s.settings.Layout.TrackFileMask)
	return dir, options, true
}

func (s *DownloadSession) applyFileResult(
	item TrackProgress,
	trackCtx utils.TrackLogContext,
	events chan<- DownloadSessionEvent,
	filePath string,
	err error,
) {
	if err != nil {
		item.status = TrackStatusError
		item.errMsg = err.Error()
		item.filename = filePath

		if errors.Is(err, ya.ErrTrackAlreadyExists) {
			item.status = TrackStatusAlreadyExists
			item.format = downloadFormatFromFilename(filePath)
			s.logger.LogTrack(slog.LevelInfo, trackCtx, "worker skipped",
				"stage", "download_track",
				"status", item.status.String(),
				"filename", filePath,
				"reason", "already_exists",
			)
			events <- DownloadSessionEvent{Progress: item, Completed: true}
			return
		}

		s.logger.LogTrack(slog.LevelError, trackCtx, "worker finished with error",
			"stage", "download_track",
			"status", item.status.String(),
			"filename", filePath,
			"error", err,
		)
	} else {
		item.status = TrackStatusDownloaded
		item.filename = filePath
		item.format = downloadFormatFromFilename(filePath)
		item.errMsg = ""

		s.logger.LogTrack(slog.LevelInfo, trackCtx, "worker finished",
			"stage", "download_track",
			"status", item.status.String(),
			"filename", filePath,
		)
	}

	events <- DownloadSessionEvent{Progress: item, Completed: true}
}
