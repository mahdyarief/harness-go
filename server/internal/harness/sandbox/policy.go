package sandbox

import "errors"

type Config struct {
	Mode                    string
	WorkspaceRoot           string
	AdditionalWritableRoots []string
}

func (c Config) EffectiveMode() string {
	if c.Mode == "" {
		return "read-only"
	}
	return c.Mode
}

func Validate(c Config) error {
	switch c.EffectiveMode() {
	case "read-only":
		return nil
	case "workspace-write":
		if !isAbsolute(c.WorkspaceRoot) {
			return errors.New("workspaceRoot harus path absolut")
		}
		for _, root := range c.AdditionalWritableRoots {
			if !isAbsolute(root) {
				return errors.New("additionalWritableRoots harus path absolut")
			}
		}
		return nil
	default:
		return errors.New("mode sandbox tidak dikenal")
	}
}

func isAbsolute(path string) bool {
	if path == "" {
		return false
	}
	if path[0] == '/' || path[0] == '\\' {
		return true
	}
	return len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}
