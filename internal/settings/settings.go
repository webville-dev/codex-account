package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	AgentPi       = "pi"
	AgentCodex    = "codex"
	AgentOpenCode = "opencode"
)

var primaryAgents = [...]string{AgentPi, AgentCodex, AgentOpenCode}

func PrimaryAgents() []string {
	return append([]string(nil), primaryAgents[:]...)
}

func IsPrimaryAgent(agent string) bool {
	for _, candidate := range primaryAgents {
		if agent == candidate {
			return true
		}
	}
	return false
}

// File is the on-disk settings.json object.
type File struct {
	PrimaryAgent string `json:"primaryAgent"`
}

func Default() File {
	return File{PrimaryAgent: AgentPi}
}

func (f File) Normalize() (File, error) {
	agent := strings.ToLower(strings.TrimSpace(f.PrimaryAgent))
	if agent == "" {
		agent = AgentPi
	}
	if IsPrimaryAgent(agent) {
		f.PrimaryAgent = agent
		return f, nil
	}
	return File{}, fmt.Errorf("primaryAgent must be %q, %q, or %q, found %q", AgentPi, AgentCodex, AgentOpenCode, f.PrimaryAgent)
}

func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return File{}, fmt.Errorf("cannot read settings %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var f File
	if err := decoder.Decode(&f); err != nil {
		return File{}, fmt.Errorf("invalid settings in %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return File{}, fmt.Errorf("invalid settings in %s: expected one JSON object", path)
	}
	out, err := f.Normalize()
	if err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}
