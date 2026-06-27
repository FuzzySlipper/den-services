package artifacts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type BlobStore interface {
	Save(ctx context.Context, storageKey string, data []byte) error
	Open(ctx context.Context, storageKey string) (io.ReadCloser, error)
}

type FilesystemBlobStore struct {
	rootPath string
}

func NewFilesystemBlobStore(rootPath string) (*FilesystemBlobStore, error) {
	if !filepath.IsAbs(rootPath) {
		return nil, fmt.Errorf("%w: blob root must be absolute", ErrInvalidArtifact)
	}
	return &FilesystemBlobStore{rootPath: filepath.Clean(rootPath)}, nil
}

func (s *FilesystemBlobStore) Save(ctx context.Context, storageKey string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	targetPath, err := s.pathForKey(storageKey)
	if err != nil {
		return err
	}
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking artifact blob %s: %w", storageKey, err)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		return fmt.Errorf("creating artifact blob directory: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".upload-*")
	if err != nil {
		return fmt.Errorf("creating artifact blob temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("writing artifact blob: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("closing artifact blob: %w", err)
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("installing artifact blob: %w", err)
	}
	cleanup = false
	return nil
}

func (s *FilesystemBlobStore) Open(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	blobPath, err := s.pathForKey(storageKey)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(blobPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, notFound(storageKey)
	}
	if err != nil {
		return nil, fmt.Errorf("opening artifact blob %s: %w", storageKey, err)
	}
	return file, nil
}

func (s *FilesystemBlobStore) pathForKey(storageKey string) (string, error) {
	if strings.TrimSpace(storageKey) == "" {
		return "", fmt.Errorf("%w: storage key is required", ErrInvalidArtifact)
	}
	cleanKey := filepath.Clean(filepath.FromSlash(storageKey))
	if filepath.IsAbs(cleanKey) || cleanKey == "." || strings.HasPrefix(cleanKey, ".."+string(filepath.Separator)) || cleanKey == ".." {
		return "", fmt.Errorf("%w: unsafe storage key", ErrInvalidArtifact)
	}
	fullPath := filepath.Join(s.rootPath, cleanKey)
	if !strings.HasPrefix(fullPath, s.rootPath+string(filepath.Separator)) && fullPath != s.rootPath {
		return "", fmt.Errorf("%w: storage key escapes root", ErrInvalidArtifact)
	}
	return fullPath, nil
}
