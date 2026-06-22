package docpublish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type FilePublicationStore struct {
	root string
}

func NewFilePublicationStore(root string) *FilePublicationStore {
	return &FilePublicationStore{root: root}
}

func (s *FilePublicationStore) Get(_ context.Context, id string) (*PublicationRecord, error) {
	if !safeName(id) {
		return nil, invalidRequest("publication id is invalid")
	}
	return s.read(filepath.Join(s.root, id+".json"))
}

func (s *FilePublicationStore) FindBySource(_ context.Context, source DocumentSource) (*PublicationRecord, error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, notFound("no publication record for source document")
	}
	if err != nil {
		return nil, fmt.Errorf("reading publication records: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		record, err := s.read(filepath.Join(s.root, entry.Name()))
		if err != nil {
			return nil, err
		}
		if record.DocumentProjectID == source.DocumentProjectID && record.DocumentSlug == source.DocumentSlug {
			return record, nil
		}
	}
	return nil, notFound("no publication record for source document")
}

func (s *FilePublicationStore) Save(_ context.Context, record PublicationRecord) error {
	if !safeName(record.ID) {
		return invalidRequest("publication id is invalid")
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("creating publication records dir: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding publication record: %w", err)
	}
	path := filepath.Join(s.root, record.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing publication record: %w", err)
	}
	return nil
}

func (s *FilePublicationStore) read(path string) (*PublicationRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, notFound("publication record not found")
	}
	if err != nil {
		return nil, fmt.Errorf("reading publication record: %w", err)
	}
	var record PublicationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("decoding publication record: %w", err)
	}
	return &record, nil
}

func safeName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
