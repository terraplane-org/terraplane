package orchestratorconfig

import (
	"fmt"
	"os"
	"strings"
)

// LoadFile reads an orchestrator config. An empty path returns a nil config
// and no error, which leaves gates and apply requirements off.
func LoadFile(path string) (*ConfigFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read orchestrator config: %w", err)
	}
	cfg, err := ParseConfigFile(data)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
