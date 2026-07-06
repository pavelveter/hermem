package config

import (
	"fmt"
	"os"
	"strings"
)

func AddKeyToFile(path string, key, scope, label string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		data = []byte{}
	}
	content := string(data)
	spec := key
	if scope != "" {
		spec += ":" + scope
	}
	if label != "" {
		spec += ":" + label
	}
	serverSection := findOrCreateSection(&content, "server")
	line := insertKeyLine(serverSection)
	indent := ""
	if line > 0 {
		indent = "\n"
	}
	apiKeysLine := findAPILine(serverSection)
	if apiKeysLine >= 0 {
		lines := strings.Split(content, "\n")
		existing := lines[apiKeysLine]
		lines[apiKeysLine] = existing + ", " + spec
		content = strings.Join(lines, "\n")
	} else {
		content = content + indent + "api_keys = " + spec + "\n"
	}
	// hermem.ini carries api_keys and AI-provider secrets (see Admin / Server
	// sections). The previous 0644 mode was world-readable: any local user
	// could `cat` the file and lift the keys. We route every mutator through
	// writeConfig() below, which writes with mode 0o600 AND issues an explicit
	// Chmod so legacy 0o644 installations are actively narrowed on the next
	// mutation. Belt-and-suspenders: mode-on-create plus post-mutation chmod.
	return writeConfig(path, []byte(content))
}

func RemoveKeyFromFile(path, labelOrValue string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	var newLines []string
	inServer := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if lower == "[server]" {
			inServer = true
			newLines = append(newLines, line)
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inServer = false
			newLines = append(newLines, line)
			continue
		}
		if inServer && strings.HasPrefix(lower, "api_keys") {
			value := extractValue(trimmed)
			entries := strings.Split(value, ",")
			var kept []string
			for _, e := range entries {
				e = strings.TrimSpace(e)
				if e == "" {
					continue
				}
				parts := strings.Split(e, ":")
				if len(parts) >= 3 && parts[2] == labelOrValue {
					continue
				}
				if parts[0] == labelOrValue {
					continue
				}
				kept = append(kept, e)
			}
			if len(kept) > 0 {
				newLines = append(newLines, "api_keys = "+strings.Join(kept, ", "))
			}
			continue
		}
		newLines = append(newLines, line)
	}
	// 0o600 + post-Chmod narrowing — see rationale in AddKeyToFile above.
	return writeConfig(path, []byte(strings.Join(newLines, "\n")))
}

func RotateKeyInFile(path, labelOrValue, newKey string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	inServer := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if lower == "[server]" {
			inServer = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inServer = false
			continue
		}
		if inServer && strings.HasPrefix(lower, "api_keys") {
			value := extractValue(trimmed)
			entries := strings.Split(value, ",")
			var replaced []string
			for _, e := range entries {
				e = strings.TrimSpace(e)
				if e == "" {
					continue
				}
				parts := strings.Split(e, ":")
				match := (len(parts) >= 3 && parts[2] == labelOrValue) || parts[0] == labelOrValue
				if match {
					replacement := newKey
					if len(parts) >= 2 {
						replacement += ":" + parts[1]
					}
					if len(parts) >= 3 {
						replacement += ":" + parts[2]
					}
					replaced = append(replaced, replacement)
				} else {
					replaced = append(replaced, e)
				}
			}
			lines[i] = "api_keys = " + strings.Join(replaced, ", ")
		}
	}
	// 0o600 — see rationale in AddKeyToFile above.
	return writeConfig(path, []byte(strings.Join(lines, "\n")))
}

func findOrCreateSection(content *string, section string) string {
	lower := strings.ToLower(*content)
	needle := "[" + strings.ToLower(section) + "]"
	idx := strings.Index(lower, needle)
	if idx >= 0 {
		lineStart := strings.LastIndex((*content)[:idx], "\n")
		if lineStart < 0 {
			return *content
		}
		return (*content)[lineStart+1:]
	}
	if len(*content) > 0 && !strings.HasSuffix(*content, "\n") {
		*content += "\n"
	}
	*content += "\n[" + section + "]\n"
	return ""
}

func insertKeyLine(sectionContent string) int {
	lines := strings.Split(sectionContent, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		return i
	}
	return len(lines)
}

func findAPILine(sectionContent string) int {
	lines := strings.Split(sectionContent, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "api_keys") {
			return i
		}
	}
	return -1
}

func extractValue(line string) string {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

// writeConfig writes content to path with mode 0o600 and then explicitly
// chmods the result to 0o600. Plain os.WriteFile is NOT enough: open(2)'s
// mode argument only applies on file creation, so a pre-existing 0o644
// hermem.ini would stay 0o644 after a WriteFile+truncate. This helper is
// the single chokepoint for hermem.ini mutations; route every new mutator
// through it. Bypassing it requires an explicit justification.
func writeConfig(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
