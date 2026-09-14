package batch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ya-music/internal/config"
	"ya-music/internal/layout"
	"ya-music/utils"
	"ya-music/ya"
	"ya-music/ya/model"
)

const DefaultConcurrency = 3

type Status string

const (
	StatusDownloading Status = "downloading"
	StatusDone        Status = "done"
	StatusSkipped     Status = "skipped"
	StatusError       Status = "error"
)

type Container string

const (
	ContainerMP3  Container = "mp3"
	ContainerFLAC Container = "flac"
	ContainerM4A  Container = "m4a"
)

const (
	SkipUnavailable   = "unavailable"
	SkipDuplicate     = "duplicate"
	SkipAlreadyExists = "already exists"
)

type Event struct {
	Index  int
	Track  model.Track
	Status Status
	Format Container
	Reason string
}

type trackDownloader interface {
	DownloadTrackWithOptions(model.Track, string, ya.DownloadOptions) (string, error)
}

type trackCloner interface {
	CloneTaggedTrack(string, string, model.Track, ya.DownloadOptions) (string, error)
}

type Config struct {
	Client                trackDownloader
	Tracks                []model.Track
	OutputDir             string
	Options               ya.DownloadOptions
	Concurrency           int
	Context               context.Context
	Layout                config.Layout
	FolderArtist          string
	FolderPlaylist        string
	Duplicates            config.Duplicates
	DownloadRetries       int
	DownloadRetryInterval int // секунды между попытками
}

// Run downloads all eligible tracks and reports immutable lifecycle events.
func Run(cfg Config) <-chan Event {
	events := make(chan Event)
	ctx := cfg.Context
	if ctx == nil {
		ctx = context.Background()
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	go func() {
		defer close(events)

		policy := cfg.Duplicates.OrDefault()
		seenIDs := make(map[string]struct{}, len(cfg.Tracks))
		published := NewPublishedFiles()
		cloner, _ := cfg.Client.(trackCloner)
		dirs := trackOutputDirs(cfg)
		suffixes := filenameSuffixes(cfg.Tracks, dirs, cfg.Layout.TrackFileMask)
		sem := make(chan struct{}, concurrency)
		var workers sync.WaitGroup

		for position, track := range cfg.Tracks {
			if ctx.Err() != nil {
				break
			}

			event := Event{Index: position + 1, Track: track}
			if !track.Available {
				event.Status = StatusSkipped
				event.Reason = SkipUnavailable
				events <- event
				continue
			}

			id := track.ID.String()
			if policy == config.DuplicatesID && id != "" {
				if _, exists := seenIDs[id]; exists {
					event.Status = StatusSkipped
					event.Reason = SkipDuplicate
					events <- event
					continue
				}
				seenIDs[id] = struct{}{}
			}

			options := cfg.Options
			options.FilenameSuffix = suffixes[position]
			options.FilenameBase = layout.TrackFilenameBase(track, cfg.Layout.TrackFileMask)
			dir := dirs[position]

			var publishedFile *PublishedFile
			leader := false
			if policy.CopiesSharedID() && id != "" {
				publishedFile, leader = published.Claim(id)
				if !leader {
					workers.Add(1)
					go runCopyWaiter(copyWaiterArgs{
						ctx:                   ctx,
						sem:                   sem,
						events:                events,
						event:                 event,
						options:               options,
						dir:                   dir,
						client:                cfg.Client,
						cloner:                cloner,
						source:                publishedFile,
						workers:               &workers,
						downloadRetries:       cfg.DownloadRetries,
						downloadRetryInterval: cfg.DownloadRetryInterval,
					})
					continue
				}
			}

			if !acquireSlot(ctx, sem) {
				if leader {
					publishedFile.Complete("")
				}
				break
			}

			workers.Add(1)
			go runDownloadWorker(downloadWorkerArgs{
				ctx:                   ctx,
				sem:                   sem,
				events:                events,
				event:                 event,
				options:               options,
				dir:                   dir,
				client:                cfg.Client,
				publishedFile:         publishedFile,
				leader:                leader,
				workers:               &workers,
				downloadRetries:       cfg.DownloadRetries,
				downloadRetryInterval: cfg.DownloadRetryInterval,
			})
		}

		workers.Wait()
	}()

	return events
}

type downloadWorkerArgs struct {
	ctx                   context.Context
	sem                   chan struct{}
	events                chan<- Event
	event                 Event
	options               ya.DownloadOptions
	dir                   string
	client                trackDownloader
	publishedFile         *PublishedFile
	leader                bool
	workers               *sync.WaitGroup
	downloadRetries       int
	downloadRetryInterval int
}

func runDownloadWorker(args downloadWorkerArgs) {
	defer args.workers.Done()
	defer func() { <-args.sem }()
	defer func() {
		if recovered := recover(); recovered != nil {
			if args.leader {
				args.publishedFile.Complete("")
			}
			args.event.Status = StatusError
			args.event.Reason = fmt.Sprintf("panic: %v", recovered)
			args.events <- args.event
		}
	}()

	if args.ctx.Err() != nil {
		if args.leader {
			args.publishedFile.Complete("")
		}
		return
	}

	filename, err := downloadToDir(
		args.ctx,
		args.events,
		&args.event,
		args.dir,
		args.client,
		args.options,
		args.downloadRetries,
		args.downloadRetryInterval,
	)
	publishDownloadedFile(args.publishedFile, args.leader, filename, err)
}

type copyWaiterArgs struct {
	ctx                   context.Context
	sem                   chan struct{}
	events                chan<- Event
	event                 Event
	options               ya.DownloadOptions
	dir                   string
	client                trackDownloader
	cloner                trackCloner
	source                *PublishedFile
	workers               *sync.WaitGroup
	downloadRetries       int
	downloadRetryInterval int
}

func runCopyWaiter(args copyWaiterArgs) {
	defer args.workers.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			args.event.Status = StatusError
			args.event.Reason = fmt.Sprintf("panic: %v", recovered)
			args.events <- args.event
		}
	}()

	src := args.source.Wait(args.ctx)
	if args.ctx.Err() != nil {
		return
	}

	if src != "" && args.cloner != nil {
		if !acquireSlot(args.ctx, args.sem) {
			return
		}
		defer func() { <-args.sem }()
		cloneToDir(args.ctx, args.events, &args.event, src, args.dir, args.cloner, args.options)
		return
	}

	if !acquireSlot(args.ctx, args.sem) {
		return
	}
	defer func() { <-args.sem }()
	_, _ = downloadToDir(
		args.ctx,
		args.events,
		&args.event,
		args.dir,
		args.client,
		args.options,
		args.downloadRetries,
		args.downloadRetryInterval,
	)
}

func downloadToDir(
	ctx context.Context,
	events chan<- Event,
	event *Event,
	dir string,
	client trackDownloader,
	options ya.DownloadOptions,
	retries int,
	retryIntervalSec int,
) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	event.Status = StatusDownloading
	events <- *event

	if err := utils.CreateDirIfNotExists(dir); err != nil {
		event.Status = StatusError
		event.Reason = err.Error()
		events <- *event
		return "", err
	}

	if retryIntervalSec <= 0 {
		retryIntervalSec = config.DefaultDownloadRetryInterval
	}

	filename, err := ya.DownloadTrackWithRetries(
		ctx,
		client,
		event.Track,
		dir,
		options,
		retries,
		time.Duration(retryIntervalSec)*time.Second,
		nil,
	)
	applyDownloadResult(event, filename, err)
	events <- *event
	return filename, err
}

func cloneToDir(
	ctx context.Context,
	events chan<- Event,
	event *Event,
	src string,
	dir string,
	cloner trackCloner,
	options ya.DownloadOptions,
) {
	if ctx.Err() != nil {
		return
	}

	if err := utils.CreateDirIfNotExists(dir); err != nil {
		event.Status = StatusError
		event.Reason = err.Error()
		events <- *event
		return
	}

	filename, err := cloner.CloneTaggedTrack(src, dir, event.Track, options)
	if errors.Is(err, ya.ErrDuplicateDestination) {
		event.Status = StatusSkipped
		event.Reason = SkipDuplicate
		events <- *event
		return
	}

	event.Status = StatusDownloading
	events <- *event
	applyDownloadResult(event, filename, err)
	events <- *event
}

func applyDownloadResult(event *Event, filename string, err error) {
	if errors.Is(err, ya.ErrTrackAlreadyExists) {
		event.Status = StatusSkipped
		event.Reason = SkipAlreadyExists
		event.Format = formatFromFilename(filename)
		return
	}
	if err != nil {
		event.Status = StatusError
		event.Reason = err.Error()
		return
	}
	event.Status = StatusDone
	event.Format = formatFromFilename(filename)
}

func publishDownloadedFile(entry *PublishedFile, leader bool, filename string, err error) {
	if !leader {
		return
	}
	if err == nil || errors.Is(err, ya.ErrTrackAlreadyExists) {
		entry.Complete(filename)
		return
	}
	entry.Complete("")
}

func acquireSlot(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func trackOutputDirs(cfg Config) []string {
	dirs := make([]string, len(cfg.Tracks))
	opts := layout.Options{
		Layout:         cfg.Layout,
		FolderArtist:   cfg.FolderArtist,
		FolderPlaylist: cfg.FolderPlaylist,
	}
	for i, track := range cfg.Tracks {
		dirs[i] = layout.TrackOutputDir(cfg.OutputDir, track, opts)
	}
	return dirs
}

func filenameSuffixes(tracks []model.Track, dirs []string, mask string) []string {
	idsByFilenameKey := make(map[string]map[string]struct{}, len(tracks))
	for i, track := range tracks {
		key := collisionKey(dirs[i], track, mask)
		if idsByFilenameKey[key] == nil {
			idsByFilenameKey[key] = make(map[string]struct{})
		}
		idsByFilenameKey[key][track.ID.String()] = struct{}{}
	}

	suffixes := make([]string, len(tracks))
	for position, track := range tracks {
		if len(idsByFilenameKey[collisionKey(dirs[position], track, mask)]) > 1 {
			suffixes[position] = fmt.Sprintf("[%s]", track.ID.String())
		}
	}

	return suffixes
}

func collisionKey(dir string, track model.Track, mask string) string {
	return dir + "\x00" + layout.TrackFilenameBase(track, mask)
}

func formatFromFilename(filename string) Container {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".flac":
		return ContainerFLAC
	case ".m4a", ".mp4":
		return ContainerM4A
	default:
		return ContainerMP3
	}
}
