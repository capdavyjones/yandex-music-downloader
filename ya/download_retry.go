package ya

import (
	"context"
	"errors"
	"time"

	"ya-music/ya/model"
)

// TrackDownloader — клиент, умеющий скачивать трек с опциями.
type TrackDownloader interface {
	DownloadTrackWithOptions(track model.Track, outputDir string, options DownloadOptions) (string, error)
}

// OnRetryWait вызывается перед паузой между попытками (remainingRetries > 0)
// и сразу после неё (remainingRetries == 0 — сброс индикатора ожидания).
type OnRetryWait func(remainingRetries int, waitUntil time.Time)

// DownloadTrackWithRetries скачивает трек, повторяя попытки при ошибках.
// retries — сколько раз повторить после первой неудачи (всего попыток = 1 + retries).
// interval — пауза между попытками; при interval <= 0 пауза не делается.
// onRetryWait может быть nil.
func DownloadTrackWithRetries(
	ctx context.Context,
	client TrackDownloader,
	track model.Track,
	outputDir string,
	options DownloadOptions,
	retries int,
	interval time.Duration,
	onRetryWait OnRetryWait,
) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		return "", errors.New("download client is nil")
	}
	if retries < 0 {
		retries = 0
	}

	attempts := 1 + retries
	var filename string
	var err error

	for attempt := 1; attempt <= attempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}

		filename, err = client.DownloadTrackWithOptions(track, outputDir, options)
		if err == nil || !isRetryableDownloadError(err) {
			return filename, err
		}
		if attempt == attempts {
			return filename, err
		}

		remaining := attempts - attempt
		waitUntil := time.Now().Add(interval)
		if onRetryWait != nil && remaining > 0 {
			onRetryWait(remaining, waitUntil)
		}

		waitErr := waitRetryInterval(ctx, interval)
		if onRetryWait != nil {
			onRetryWait(0, time.Time{})
		}
		if waitErr != nil {
			return "", waitErr
		}
	}

	return filename, err
}

func isRetryableDownloadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrTrackAlreadyExists) || errors.Is(err, ErrDuplicateDestination) {
		return false
	}
	// Отмена пользователем — без повторов. DeadlineExceeded от --timeout
	// считаем временной сетевой ошибкой и ретраим.
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

func waitRetryInterval(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
