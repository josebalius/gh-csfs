package csfs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2"
)

type Codespace struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Repository  string `json:"repository"`
	State       string `json:"state"`
}

const codespaceFields = "name,displayName,repository,state"

func (c Codespace) Active() bool {
	return c.State == "Available"
}

func (c Codespace) Workspace() string {
	s := strings.Split(c.Repository, "/")
	return s[len(s)-1]
}

func ListCodespaces(ctx context.Context) ([]Codespace, error) {
	stdout, _, err := gh.ExecContext(ctx, "cs", "list", "--json", codespaceFields)
	if err != nil {
		return nil, fmt.Errorf("failed to list codespaces: %w", err)
	}
	var response []Codespace
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal codespaces: %w", err)
	}
	return response, nil
}

func GetCodespace(ctx context.Context, codespace string) (Codespace, error) {
	stdout, _, err := gh.ExecContext(ctx, "cs", "view", "-c", codespace, "--json", codespaceFields)
	if err != nil {
		return Codespace{}, fmt.Errorf("failed to get codespace: %w", err)
	}
	var response Codespace
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return response, fmt.Errorf("failed to unmarshal codespace: %w", err)
	}
	return response, nil
}
