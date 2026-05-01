// Package watch implementa monitoramento passivo de sessions Claude Code
// rodando em background (junto com `nessy serve`). Roda detectores em
// goroutines e dispara notificações nativas (sysutil.Notify) quando achar:
//
//   - Loop: mesma tool com mesmo input ≥3× em ≤60s
//   - Cost spike: session em curso com cost > 2× mediana do skill
//
// Usa polling do filesystem (mtime) — mais simples que fsnotify e suficiente
// pra latência aceitável (5s default). fsnotify dá problema no macOS com
// arquivos que ficam abertos por muito tempo (Claude Code mantém handle).
package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/felipeness/nessy/internal/sysutil"
)

// Config controla os detectores. Zero = defaults razoáveis.
type Config struct {
	// PollInterval é o intervalo entre scans. Default 10s.
	PollInterval time.Duration
	// LoopWindowSecs: tool repetido em ≤N segundos = loop. Default 60.
	LoopWindowSecs float64
	// LoopMinCount: mínimo de repetições pra alertar. Default 3.
	LoopMinCount int
	// NotifyDebounceSecs: mesma alertKey não repete em ≤N segundos. Default 30.
	NotifyDebounceSecs float64
}

func (c *Config) defaults() {
	if c.PollInterval == 0 {
		c.PollInterval = 10 * time.Second
	}
	if c.LoopWindowSecs == 0 {
		c.LoopWindowSecs = 60
	}
	if c.LoopMinCount == 0 {
		c.LoopMinCount = 3
	}
	if c.NotifyDebounceSecs == 0 {
		c.NotifyDebounceSecs = 30
	}
}

// Watcher coordena os detectores e gerencia debounce de notifications.
type Watcher struct {
	cfg          Config
	projectsRoot string
	logger       *log.Logger

	// debounce tracks último envio por alertKey
	mu       sync.Mutex
	lastSent map[string]time.Time

	// loopState tracks tool_use por (sessionFile, hash) com sliding window
	loopState map[string][]toolEvent
}

type toolEvent struct {
	ts       time.Time
	toolName string
	hash     string
}

// New cria um Watcher pronto pra rodar. projectsRoot é tipicamente
// ~/.claude/projects/.
func New(projectsRoot string, cfg Config, logger *log.Logger) *Watcher {
	cfg.defaults()
	if logger == nil {
		logger = log.New(os.Stderr, "[watch] ", log.LstdFlags)
	}
	return &Watcher{
		cfg:          cfg,
		projectsRoot: projectsRoot,
		logger:       logger,
		lastSent:     map[string]time.Time{},
		loopState:    map[string][]toolEvent{},
	}
}

// Run loop polling até ctx cancelado. Bloqueia.
func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	w.scan() // primeiro scan imediato
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan()
		}
	}
}

// scan lista JSONLs e processa as últimas linhas de cada um.
// Stateless por design — cada poll relê só o tail.
func (w *Watcher) scan() {
	cutoff := time.Now().Add(-w.cfg.PollInterval - 5*time.Second)
	_ = filepath.WalkDir(w.projectsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// Skipa subagent files (subdir subagents/)
		if strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Skip arquivos sem mod recente — nada novo
		if info.ModTime().Before(cutoff) {
			return nil
		}
		w.processFile(path)
		return nil
	})
}

// processFile lê o arquivo inteiro e processa eventos novos por sessionFile.
// O state map por sessionFile garante que loop detection só conta dentro do
// mesmo arquivo. Otimização futura: tail incremental por offset.
func (w *Watcher) processFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	for dec.More() {
		var ev struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Message   *struct {
				Content json.RawMessage `json:"content"`
			} `json:"message,omitempty"`
		}
		if err := dec.Decode(&ev); err != nil {
			continue
		}
		if ev.Type != "assistant" || ev.Message == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, ev.Timestamp)
		if err != nil {
			continue
		}
		// Só processa events dos últimos 2× window — não vai realertar coisa
		// antiga toda vez que o arquivo muda
		if t.Before(time.Now().Add(-2 * time.Duration(w.cfg.LoopWindowSecs) * time.Second)) {
			continue
		}
		var blocks []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input,omitempty"`
		}
		if err := json.Unmarshal(ev.Message.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type != "tool_use" || b.Name == "" {
				continue
			}
			w.observeTool(path, t, b.Name, hashInput(b.Input))
		}
	}
}

// observeTool adiciona um evento e checa se virou loop.
func (w *Watcher) observeTool(sessionFile string, ts time.Time, name, hash string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	key := sessionFile + "|" + name + "|" + hash
	w.loopState[key] = append(w.loopState[key], toolEvent{ts, name, hash})

	// Trim janela
	cutoff := ts.Add(-time.Duration(w.cfg.LoopWindowSecs) * time.Second)
	pruned := w.loopState[key][:0]
	for _, e := range w.loopState[key] {
		if e.ts.After(cutoff) {
			pruned = append(pruned, e)
		}
	}
	w.loopState[key] = pruned

	if len(pruned) >= w.cfg.LoopMinCount {
		w.notifyLoop(sessionFile, name, len(pruned))
		// Reset esse key pra não realertar imediatamente — debounce externo já
		// cobre, mas zera o estado pra próximo "ciclo" começar limpo
		delete(w.loopState, key)
	}
}

// notifyLoop emite notificação nativa (cross-platform via sysutil),
// com debounce por alertKey.
func (w *Watcher) notifyLoop(sessionFile, toolName string, count int) {
	alertKey := "loop:" + filepath.Base(sessionFile) + ":" + toolName
	if !w.shouldNotify(alertKey) {
		return
	}
	sid := strings.TrimSuffix(filepath.Base(sessionFile), ".jsonl")
	if len(sid) > 8 {
		sid = sid[:8]
	}
	title := "Nessy: loop detectado"
	body := fmt.Sprintf("%s repetido %d× em [%s]. Vale revisar.", toolName, count, sid)
	w.logger.Printf("LOOP %s × %d in %s", toolName, count, sid)
	if err := sysutil.Notify(title, body); err != nil {
		w.logger.Printf("notify failed: %v", err)
	}
}

// shouldNotify devolve true se passou tempo suficiente desde último envio
// daquele alertKey. Mutex já tomado pelo caller.
func (w *Watcher) shouldNotify(key string) bool {
	now := time.Now()
	if last, ok := w.lastSent[key]; ok {
		if now.Sub(last) < time.Duration(w.cfg.NotifyDebounceSecs)*time.Second {
			return false
		}
	}
	w.lastSent[key] = now
	return true
}

// hashInput devolve hash curto e estável do input JSON.
// Não precisa ser tão robusto quanto o do parser (que canonicaliza chaves)
// porque aqui é só pra detectar loop dentro de poucos segundos — improvável
// que mesmo conteúdo apareça com chaves em ordem diferente em <60s.
func hashInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "0"
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}
