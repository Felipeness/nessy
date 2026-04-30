package statusline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StatusLineSetting é o shape que vai em settings.json sob a key "statusLine".
// type "command" é o único valor suportado pelo Claude Code.
type StatusLineSetting struct {
	Type            string `json:"type"`
	Command         string `json:"command"`
	RefreshInterval int    `json:"refreshInterval,omitempty"` // 1-60s, opcional
}

// InstallOptions controla o install no settings.json.
type InstallOptions struct {
	SettingsPath    string // ex: ~/.claude/settings.json
	Command         string // ex: "/Users/x/.local/bin/claude-history statusline-render"
	RefreshInterval int    // 0 = event-driven (default), 1-60 = interval-driven
	Force           bool   // sobrescreve sem perguntar se já existe statusLine
}

// InstallResult descreve o que aconteceu — pra mensagem ao user.
type InstallResult struct {
	Backup      string // path do backup criado, vazio se não houve backup
	Replaced    bool   // true se substituímos um statusLine existente
	HadConflict bool   // true se já tinha statusLine com command diferente (mesmo com Force)
}

// Install merge atomicamente a entrada statusLine no settings.json.
// Estratégia: backup → load → modificar só a key statusLine → write tmp → rename.
//
// Se já existir statusLine com command diferente, retorna erro a menos que
// opts.Force seja true. Outras keys do settings.json são preservadas.
func Install(opts InstallOptions) (*InstallResult, error) {
	if opts.SettingsPath == "" {
		return nil, fmt.Errorf("settings path required")
	}
	if opts.Command == "" {
		return nil, fmt.Errorf("command required")
	}

	if err := os.MkdirAll(filepath.Dir(opts.SettingsPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir parent: %w", err)
	}

	// Load existing (ou {} se não existe)
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

	// Detect conflito
	if existing, ok := settings["statusLine"].(map[string]any); ok {
		if cmd, _ := existing["command"].(string); cmd != "" && cmd != opts.Command {
			res.HadConflict = true
			if !opts.Force {
				return res, fmt.Errorf(
					"settings.json já tem statusLine apontando pra %q — use --force pra sobrescrever",
					cmd,
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

	// Modificar só a chave statusLine
	entry := StatusLineSetting{
		Type:    "command",
		Command: opts.Command,
	}
	if opts.RefreshInterval > 0 {
		entry.RefreshInterval = opts.RefreshInterval
	}
	entryBytes, _ := json.Marshal(entry)
	var entryMap map[string]any
	_ = json.Unmarshal(entryBytes, &entryMap)
	settings["statusLine"] = entryMap

	// Write atômico via tmp + rename
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

// Uninstall remove a key statusLine do settings.json (preservando o resto).
// Faz backup. Retorna true se removeu, false se não tinha.
func Uninstall(settingsPath string) (removed bool, backup string, err error) {
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
	if _, ok := settings["statusLine"]; !ok {
		return false, "", nil
	}
	stamp := time.Now().Format("20060102-150405")
	backup = settingsPath + ".bak." + stamp
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return false, "", err
	}
	delete(settings, "statusLine")
	out, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return false, backup, err
	}
	return true, backup, nil
}
