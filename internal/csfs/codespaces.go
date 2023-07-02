package csfs

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Codespace struct {
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	Repository  Repository `json:"repository"`
}

func (c Codespace) Workspace() string {
	s := strings.Split(c.Repository.FullName, "/")
	return s[len(s)-1]
}

type Repository struct {
	FullName string `json:"full_name"`
}

func ListCodespaces(ctx context.Context) ([]Codespace, error) {
	args := []string{
		"api",
		"user/codespaces",
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	o, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list codespaces: %w", err)
	}

	var response struct {
		Codespaces []Codespace `json:"codespaces"`
	}
	if err := json.Unmarshal(o, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal codespaces: %w", err)
	}
	return response.Codespaces, nil
}
