package ya

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"ya-music/utils"
	"ya-music/ya/model"
)

// CloneTaggedTrack copies an already downloaded file and rewrites its tags for the current entry.
func (c *Client) CloneTaggedTrack(src string, destDir string, track model.Track, options DownloadOptions) (string, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", fmt.Errorf("empty source path")
	}

	ext := filepath.Ext(src)
	if ext == "" {
		return "", fmt.Errorf("source file has no extension")
	}

	destination := buildTrackFilenameWithExtension(track, destDir, ext, options)
	if samePath(src, destination) {
		return destination, fmt.Errorf("%w: %s", ErrDuplicateDestination, destination)
	}

	exists, err := utils.FileExists(destination)
	if err != nil {
		return "", fmt.Errorf("failed to inspect destination file: %w", err)
	}
	if exists {
		return destination, fmt.Errorf("%w: %s", ErrTrackAlreadyExists, destination)
	}

	spec, err := c.artifactSpecForExtension(ext)
	if err != nil {
		return "", err
	}

	result, err := c.publishAudioArtifact(
		track,
		destination,
		options,
		spec,
		func(tempFilename string) error {
			return copyFileContents(src, tempFilename)
		},
	)
	if err != nil {
		return result.Filename, err
	}
	return result.Filename, nil
}

func (c *Client) artifactSpecForExtension(ext string) (artifactSpec, error) {
	switch strings.ToLower(ext) {
	case ".mp3":
		return c.mp3ArtifactSpec(), nil
	case ".flac":
		return flacArtifactSpec(), nil
	case ".m4a", ".mp4":
		return c.m4aArtifactSpec(), nil
	default:
		return artifactSpec{}, fmt.Errorf("unsupported audio extension %s", ext)
	}
}

func copyFileContents(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open clone temp file: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy audio file: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close clone temp file: %w", err)
	}
	return nil
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
