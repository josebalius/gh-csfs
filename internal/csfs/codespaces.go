package csfs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Codespace struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Repository  string `json:"repository"`
}

func (c Codespace) Workspace() string {
	s := strings.Split(c.Repository, "/")
	return s[len(s)-1]
}

func ListCodespaces(ctx context.Context) ([]Codespace, error) {
	args := []string{
		"cs",
		"list",
		"--json",
		"name,displayName,repository",
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	o, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list codespaces: %w", err)
	}

	var response []Codespace
	if err := json.Unmarshal(o, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal codespaces: %w", err)
	}
	return response, nil
}

func GetCodespace(ctx context.Context, codespace string) (Codespace, error) {
	args := []string{
		"cs",
		"view",
		"-c",
		codespace,
		"--json",
		"name,displayName,repository",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	o, err := cmd.Output()
	if err != nil {
		return Codespace{}, fmt.Errorf("failed to get codespace: %w", err)
	}
	var response Codespace
	if err := json.Unmarshal(o, &response); err != nil {
		return response, fmt.Errorf("failed to unmarshal codespace: %w", err)
	}
	return response, nil
}
