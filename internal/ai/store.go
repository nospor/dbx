package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/robertn/dbx/internal/config"
)

const sessionFileName = "ai_sessions.json"

type Message struct {
	Role    string `json:"role"` // "user" or "ai"
	Content string `json:"content"`
}

type ChatSession struct {
	SessionID string    `json:"session_id"`
	Messages  []Message `json:"messages"`
}

type Store struct {
	Sessions map[string]*ChatSession `json:"sessions"` // Keyed by conn:db
	Cfg      *config.Config          `json:"-"`
}

func GetSessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "dbx", sessionFileName), nil
}

func legacySessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "dbx", sessionFileName), nil
}

func LoadStore(cfg *config.Config) *Store {
	store := &Store{
		Sessions: make(map[string]*ChatSession),
		Cfg:      cfg,
	}

	path, err := GetSessionPath()
	if err != nil {
		return store
	}

	data, err := os.ReadFile(path)
	fromLegacy := false
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			legacy, lerr := legacySessionPath()
			if lerr == nil {
				data, err = os.ReadFile(legacy)
				if err == nil {
					fromLegacy = true
				}
			}
		}
	}
	if err == nil {
		_ = json.Unmarshal(data, store)
		if store.Sessions == nil {
			store.Sessions = make(map[string]*ChatSession)
		}
		if fromLegacy {
			_ = store.Save()
		}
	}
	return store
}

func (s *Store) Save() error {
	path, err := GetSessionPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func (s *Store) GetSession(connKey string) *ChatSession {
	chat, ok := s.Sessions[connKey]
	if !ok {
		chat = &ChatSession{Messages: make([]Message, 0)}
		s.Sessions[connKey] = chat
	}
	return chat
}

func (s *Store) EnsureSessionID(connKey string) error {
	chat := s.GetSession(connKey)
	if chat.SessionID != "" {
		return nil
	}

	if s.Cfg.AI == nil || s.Cfg.AI.SelectedApp == "" {
		return errors.New("no AI app selected in configuration")
	}

	appCfg, ok := s.Cfg.AI.Apps[s.Cfg.AI.SelectedApp]
	if !ok {
		return errors.New("selected AI app configuration not found")
	}

	// Run create chat command
	if appCfg.CreateSessionCommand == "" {
		// fallback
		chat.SessionID = "default"
		_ = s.Save()
		return nil
	}

	parts := strings.Fields(appCfg.CreateSessionCommand)
	if len(parts) == 0 {
		chat.SessionID = "default"
		return nil
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	if err := s.applyAIProcessDir(cmd); err != nil {
		return err
	}
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	chat.SessionID = strings.TrimSpace(string(out))
	return s.Save()
}

func (s *Store) AppendUserMessage(connKey, text string) {
	chat := s.GetSession(connKey)
	chat.Messages = append(chat.Messages, Message{Role: "user", Content: text})
	_ = s.Save()
}

func (s *Store) AppendAIMessage(connKey, text string) {
	chat := s.GetSession(connKey)
	chat.Messages = append(chat.Messages, Message{Role: "ai", Content: text})
	_ = s.Save()
}

func (s *Store) ClearSession(connKey string) {
	if chat, ok := s.Sessions[connKey]; ok {
		chat.SessionID = ""
		chat.Messages = make([]Message, 0)
		_ = s.Save()
	}
}

// ClearTranscript clears persisted messages and the CLI session id and saves immediately.
// Call EnsureSessionID afterward (possibly async) to allocate a new CLI chat id.
func (s *Store) ClearTranscript(connKey string) error {
	if connKey == "" {
		return errors.New("no connection")
	}
	chat := s.GetSession(connKey)
	chat.SessionID = ""
	chat.Messages = make([]Message, 0)
	return s.Save()
}

// ClearTranscriptAndNewCLIChat clears the transcript then runs CreateSessionCommand (via EnsureSessionID).
func (s *Store) ClearTranscriptAndNewCLIChat(connKey string) error {
	if err := s.ClearTranscript(connKey); err != nil {
		return err
	}
	return s.EnsureSessionID(connKey)
}

func (s *Store) GetConversationSize(connKey string) int {
	chat, ok := s.Sessions[connKey]
	if !ok {
		return 0
	}
	size := 0
	for _, m := range chat.Messages {
		size += len(m.Role) + len(m.Content)
	}
	return size
}

// Ask runs the selected AI app with the prompt and returns the full output
func (s *Store) Ask(connKey, prompt string) (string, error) {
	if s.Cfg.AI == nil || s.Cfg.AI.SelectedApp == "" {
		return "", errors.New("no AI app configured")
	}
	appCfg, ok := s.Cfg.AI.Apps[s.Cfg.AI.SelectedApp]
	if !ok {
		return "", errors.New("selected AI app configuration not found")
	}

	if err := s.EnsureSessionID(connKey); err != nil {
		return "", err
	}

	chat := s.GetSession(connKey)

	// Build command based on config
	baseCmd := s.Cfg.AI.SelectedApp // e.g., "cursor-agent"
	var args []string

	// e.g., --mode ask
	if appCfg.SessionModeFlag != "" {
		parts := strings.Fields(appCfg.SessionModeFlag)
		args = append(args, parts...)
	}

	// e.g., --resume <chat_id>
	if appCfg.ResumeSessionFlag != "" && chat.SessionID != "" && chat.SessionID != "default" {
		parts := strings.Fields(appCfg.ResumeSessionFlag)
		args = append(args, parts...)
		args = append(args, chat.SessionID)
	}

	// Determine where to create the temporary file (prefer the agent's workdir to avoid sandbox issues)
	var tempDir string
	if s.Cfg != nil {
		if dir, ok, err := s.Cfg.ResolveAIAgentWorkDir(); err == nil && ok && dir != "" {
			tempDir = dir
			_ = os.MkdirAll(tempDir, 0o755)
		}
	}

	// Write prompt to a temporary file to avoid "argument list too long" errors when prompt is large
	tmpFile, err := os.CreateTemp(tempDir, "dbx-ai-prompt-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file for AI prompt: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(prompt); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write AI prompt to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	// Pass instructional text pointing to the file instead of the raw prompt string
	instructionalPrompt := fmt.Sprintf("In file %s there are instructions to do with needed informations.", tmpPath)
	args = append(args, instructionalPrompt)

	cmd := exec.Command(baseCmd, args...)
	if err := s.applyAIProcessDir(cmd); err != nil {
		return "", err
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdin = strings.NewReader("") // EOF immediately; avoid attaching the TUI's stdin to the child
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	if err != nil {
		if errBuf.Len() > 0 {
			return "", errors.New(errBuf.String())
		}
		return "", err
	}

	response := strings.TrimSpace(outBuf.String())
	return response, nil
}

func (s *Store) applyAIProcessDir(cmd *exec.Cmd) error {
	if s == nil || s.Cfg == nil {
		return nil
	}
	dir, ok, err := s.Cfg.ResolveAIAgentWorkDir()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ai agent workdir: %w", err)
	}
	cmd.Dir = dir
	return nil
}
