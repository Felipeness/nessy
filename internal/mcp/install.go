package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MCPServerEntry é o shape que vai em settings.json sob mcpServers.<name>.
type MCPServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// InstallOptions controla o install no settings.json. Mesmo padrão do
// statusline-install: backup atômico + merge preservando outras keys.
type InstallOptions struct {
	SettingsPath string // ex: ~/.claude/settings.json
	Name         string // ex: "claude-history"
	Command      string // ex: "/Users/x/.local/bin/claude-history"
	Args         []string // ex: ["mcp"]
	Force        bool
}

// InstallResult descreve o que aconteceu — pra mensagem ao user.
type InstallResult struct {
	Backup      string
	Replaced    bool
	HadConflict bool
}

// Install registra ou atualiza o server na chave mcpServers do settings.json,
// preservando qualquer outro server já configurado e backup-uando o original.
func Install(opts InstallOptions) (*InstallResult, error) {
	if opts.SettingsPath == "" || opts.Name == "" || opts.Command == "" {
		return nil, fmt.Errorf("settings_path, name e command são obrigatórios")
	}
	if err := os.MkdirAll(filepath.Dir(opts.SettingsPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir parent: %w", err)
	}

	settings := map[string]any{}
	data, err := os.ReadFile(opts.SettingsPath)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, fmt.Errorf("parse %s: %w", opts.SettingsPath, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", opts.SettingsPath, err)
	}

	res := &InstallResult{}

	// mcpServers existente?
	mcpServers, _ := settings["mcpServers"].(map[string]any)
	if mcpServers == nil {
		mcpServers = map[string]any{}
	}
	if existing, ok := mcpServers[opts.Name].(map[string]any); ok {
		if cmd, _ := existing["command"].(string); cmd != "" && cmd != opts.Command {
			res.HadConflict = true
			if !opts.Force {
				return res, fmt.Errorf(
					"settings.json já tem mcpServers.%s apontando pra %q — use --force pra sobrescrever",
					opts.Name, cmd,
				)
			}
			res.Replaced = true
		}
	}

	// Backup só se arquivo existir
	if len(data) > 0 {
		stamp := time.Now().Format("20060102-150405")
		backup := opts.SettingsPath + ".bak." + stamp
		if err := os.WriteFile(backup, data, 0644); err != nil {
			return nil, fmt.Errorf("backup: %w", err)
		}
		res.Backup = backup
	}

	entry := MCPServerEntry{Command: opts.Command, Args: opts.Args}
	entryBytes, _ := json.Marshal(entry)
	var entryMap map[string]any
	_ = json.Unmarshal(entryBytes, &entryMap)
	mcpServers[opts.Name] = entryMap
	settings["mcpServers"] = mcpServers

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	tmp := opts.SettingsPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return nil, fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, opts.SettingsPath); err != nil {
		os.Remove(tmp)
		return nil, fmt.Errorf("rename: %w", err)
	}
	return res, nil
}

// Uninstall remove a entrada mcpServers.<name>, preservando outras keys.
func Uninstall(settingsPath, name string) (removed bool, backup string, err error) {
	data, err := os.ReadFile(settingsPath)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, "", err
	}
	mcpServers, _ := settings["mcpServers"].(map[string]any)
	if mcpServers == nil {
		return false, "", nil
	}
	if _, ok := mcpServers[name]; !ok {
		return false, "", nil
	}
	stamp := time.Now().Format("20060102-150405")
	backup = settingsPath + ".bak." + stamp
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return false, "", err
	}
	delete(mcpServers, name)
	if len(mcpServers) == 0 {
		delete(settings, "mcpServers")
	} else {
		settings["mcpServers"] = mcpServers
	}
	out, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return false, backup, err
	}
	return true, backup, nil
}
