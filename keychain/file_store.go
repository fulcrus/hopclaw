package keychain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fileStorePathEnv = "HOPCLAW_KEYCHAIN_FILE"

type fileStore struct {
	path string
}

func activeStore() Store {
	if path := strings.TrimSpace(os.Getenv(fileStorePathEnv)); path != "" {
		return &fileStore{path: path}
	}
	return store
}

func (s *fileStore) Get(service, key string) (string, error) {
	secrets, err := s.load()
	if err != nil {
		return "", err
	}
	svc, ok := secrets[service]
	if !ok {
		return "", fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	value, ok := svc[key]
	if !ok {
		return "", fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	return value, nil
}

func (s *fileStore) Set(service, key, value string) error {
	secrets, err := s.load()
	if err != nil {
		return err
	}
	if secrets[service] == nil {
		secrets[service] = map[string]string{}
	}
	secrets[service][key] = value
	return s.save(secrets)
}

func (s *fileStore) Delete(service, key string) error {
	secrets, err := s.load()
	if err != nil {
		return err
	}
	svc, ok := secrets[service]
	if !ok {
		return fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	if _, ok := svc[key]; !ok {
		return fmt.Errorf("keychain %s/%s: %w", service, key, ErrNotFound)
	}
	delete(svc, key)
	if len(svc) == 0 {
		delete(secrets, service)
	}
	return s.save(secrets)
}

func (s *fileStore) load() (map[string]map[string]string, error) {
	if strings.TrimSpace(s.path) == "" {
		return nil, fmt.Errorf("keychain file store path must not be empty")
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("keychain file read %s: %w", s.path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]map[string]string{}, nil
	}
	var secrets map[string]map[string]string
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("keychain file parse %s: %w", s.path, err)
	}
	if secrets == nil {
		secrets = map[string]map[string]string{}
	}
	return secrets, nil
}

func (s *fileStore) save(secrets map[string]map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("keychain file create dir %s: %w", filepath.Dir(s.path), err)
	}
	payload, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("keychain file encode %s: %w", s.path, err)
	}
	payload = append(payload, '\n')
	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return fmt.Errorf("keychain file write %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("keychain file rename %s: %w", s.path, err)
	}
	return nil
}
