package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ccgo/internal/platform"
)

const credentialsFileName = "credentials.json"

type CredentialStore interface {
	Load(context.Context) (Credentials, error)
	Save(context.Context, Credentials) error
	Delete(context.Context) error
}

type FileCredentialStore struct {
	Path string
}

func DefaultCredentialsPath() string {
	return filepath.Join(platform.ClaudeHomeDir(), credentialsFileName)
}

func NewFileCredentialStore(path string) *FileCredentialStore {
	if path == "" {
		path = DefaultCredentialsPath()
	}
	return &FileCredentialStore{Path: path}
}

func (s *FileCredentialStore) Load(ctx context.Context) (Credentials, error) {
	if err := ctx.Err(); err != nil {
		return Credentials{}, err
	}
	if s == nil || s.Path == "" {
		return Credentials{Source: SourceNone}, nil
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{Source: SourceNone}, nil
		}
		return Credentials{}, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return Credentials{Source: SourceNone}, nil
	}
	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("decode credentials: %w", err)
	}
	if credentials.Source == "" {
		credentials.Source = SourceNone
	}
	return credentials, nil
}

func (s *FileCredentialStore) Save(ctx context.Context, credentials Credentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.Path == "" {
		return fmt.Errorf("credential store path is required")
	}
	if credentials.Source == "" {
		credentials.Source = SourceNone
	}
	if err := credentials.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.Path)
}

func (s *FileCredentialStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.Path == "" {
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
