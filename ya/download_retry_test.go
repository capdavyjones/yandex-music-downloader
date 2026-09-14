package ya

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"ya-music/ya/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubTrackDownloader struct {
	calls   atomic.Int32
	results []struct {
		filename string
		err      error
	}
}

func (s *stubTrackDownloader) DownloadTrackWithOptions(
	_ model.Track,
	_ string,
	_ DownloadOptions,
) (string, error) {
	i := int(s.calls.Add(1) - 1)
	if i >= len(s.results) {
		return "", errors.New("unexpected extra download call")
	}
	return s.results[i].filename, s.results[i].err
}

func TestDownloadTrackWithRetriesSuccessOnFirstTry(t *testing.T) {
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{filename: "track.mp3"},
	}}

	got, err := DownloadTrackWithRetries(
		context.Background(),
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		3,
		time.Hour,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "track.mp3", got)
	assert.Equal(t, int32(1), client.calls.Load())
}

func TestDownloadTrackWithRetriesFailThenSucceed(t *testing.T) {
	transient := errors.New("temporary network error")
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{err: transient},
		{filename: "ok.mp3"},
	}}

	got, err := DownloadTrackWithRetries(
		context.Background(),
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		2,
		0, // без паузы — тесты остаются быстрыми
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "ok.mp3", got)
	assert.Equal(t, int32(2), client.calls.Load())
}

func TestDownloadTrackWithRetriesExhaustsAttempts(t *testing.T) {
	first := errors.New("fail-1")
	second := errors.New("fail-2")
	third := errors.New("fail-3")
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{err: first},
		{err: second},
		{err: third},
	}}

	got, err := DownloadTrackWithRetries(
		context.Background(),
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		2,
		0,
		nil,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, third)
	assert.Empty(t, got)
	assert.Equal(t, int32(3), client.calls.Load())
}

func TestDownloadTrackWithRetriesZeroRetriesSingleAttempt(t *testing.T) {
	wantErr := errors.New("boom")
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{err: wantErr},
	}}

	_, err := DownloadTrackWithRetries(
		context.Background(),
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		0,
		0,
		nil,
	)
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, int32(1), client.calls.Load())
}

func TestDownloadTrackWithRetriesNegativeRetriesTreatedAsZero(t *testing.T) {
	wantErr := errors.New("boom")
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{err: wantErr},
	}}

	_, err := DownloadTrackWithRetries(
		context.Background(),
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		-5,
		0,
		nil,
	)
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, int32(1), client.calls.Load())
}

func TestDownloadTrackWithRetriesDoesNotRetryNonRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "already exists", err: ErrTrackAlreadyExists},
		{name: "duplicate destination", err: ErrDuplicateDestination},
		{name: "context canceled", err: context.Canceled},
		{name: "wrapped already exists", err: fmt.Errorf("already there: %w", ErrTrackAlreadyExists)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubTrackDownloader{results: []struct {
				filename string
				err      error
			}{
				{filename: "existing.mp3", err: tt.err},
			}}

			got, err := DownloadTrackWithRetries(
				context.Background(),
				client,
				model.Track{ID: "1"},
				t.TempDir(),
				DownloadOptions{},
				5,
				0,
				nil,
			)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.err)
			assert.Equal(t, "existing.mp3", got)
			assert.Equal(t, int32(1), client.calls.Load())
		})
	}
}

func TestDownloadTrackWithRetriesRetriesDeadlineExceeded(t *testing.T) {
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{err: fmt.Errorf("failed to download file: %w", context.DeadlineExceeded)},
		{filename: "ok.mp3"},
	}}

	got, err := DownloadTrackWithRetries(
		context.Background(),
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		1,
		0,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "ok.mp3", got)
	assert.Equal(t, int32(2), client.calls.Load())
}

func TestDownloadTrackWithRetriesNilClient(t *testing.T) {
	_, err := DownloadTrackWithRetries(
		context.Background(),
		nil,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		1,
		0,
		nil,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "download client is nil")
}

func TestDownloadTrackWithRetriesCanceledContextBeforeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{filename: "should-not-run.mp3"},
	}}

	_, err := DownloadTrackWithRetries(
		ctx,
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		2,
		0,
		nil,
	)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(0), client.calls.Load())
}

func TestDownloadTrackWithRetriesCancelDuringSleep(t *testing.T) {
	firstAttemptDone := make(chan struct{})
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{err: errors.New("transient")},
		{filename: "should-not-reach.mp3"},
	}}
	// Перехватываем первый вызов через обёртку с сигналом.
	signaling := &signalingDownloader{
		inner: client,
		onCall: func(n int32) {
			if n == 1 {
				close(firstAttemptDone)
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-firstAttemptDone:
			cancel()
		case <-time.After(2 * time.Second):
			cancel()
		}
	}()

	_, err := DownloadTrackWithRetries(
		ctx,
		signaling,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		3,
		time.Hour, // длинная пауза — отмена должна прервать ожидание
		nil,
	)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), client.calls.Load())
}

type signalingDownloader struct {
	inner  *stubTrackDownloader
	onCall func(n int32)
}

func (s *signalingDownloader) DownloadTrackWithOptions(
	track model.Track,
	dir string,
	options DownloadOptions,
) (string, error) {
	filename, err := s.inner.DownloadTrackWithOptions(track, dir, options)
	if s.onCall != nil {
		s.onCall(s.inner.calls.Load())
	}
	return filename, err
}

func TestDownloadTrackWithRetriesNilContextUsesBackground(t *testing.T) {
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{filename: "ok.mp3"},
	}}

	got, err := DownloadTrackWithRetries(
		//lint:ignore SA1012 the test verifies the nil-context fallback to context.Background.
		nil,
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		0,
		0,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "ok.mp3", got)
}

func TestDownloadTrackWithRetriesNotifiesWaitCallback(t *testing.T) {
	client := &stubTrackDownloader{results: []struct {
		filename string
		err      error
	}{
		{err: errors.New("transient")},
		{filename: "ok.mp3"},
	}}

	var remaining []int
	got, err := DownloadTrackWithRetries(
		context.Background(),
		client,
		model.Track{ID: "1"},
		t.TempDir(),
		DownloadOptions{},
		1,
		0,
		func(r int, _ time.Time) {
			remaining = append(remaining, r)
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "ok.mp3", got)
	assert.Equal(t, []int{1, 0}, remaining)
}
