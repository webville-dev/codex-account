package fileutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	FileMode = 0o600
	DirMode  = 0o700
)

func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return WriteFile(path, data, FileMode)
}

func WriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp.*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	cleanup = false
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if df, err := os.Open(dir); err == nil {
		_ = df.Sync()
		_ = df.Close()
	}
	return nil
}

func ReadJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return nil
}

func OverlayJSON(path, key string, value any) error {
	var obj map[string]json.RawMessage
	existing, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(existing, &obj); err != nil {
			return fmt.Errorf("invalid JSON in %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	obj[key] = raw
	return WriteJSON(path, obj)
}

func MkdirSecret(path string) error {
	if err := os.MkdirAll(path, DirMode); err != nil {
		return err
	}
	return os.Chmod(path, DirMode)
}
