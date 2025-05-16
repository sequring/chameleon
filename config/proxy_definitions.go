// config/proxy_definitions.go
package config

import (
	"encoding/json"
	"os"
	"sync"
)

type ProxyDefinition struct {
	Address     string   `json:"address"`
	Username    string   `json:"username,omitempty"`
	Password    string   `json:"password,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"`
}

type ProxyDefinitionsManager struct {
	filePath      string
	mu            sync.RWMutex
	definitions   []ProxyDefinition
	reloadToken   string 
	reloadChannel chan struct{} 
}

func NewProxyDefinitionsManager(filePath string, reloadToken string) *ProxyDefinitionsManager {
	return &ProxyDefinitionsManager{
		filePath:      filePath,
		definitions:   make([]ProxyDefinition, 0),
		reloadToken:   reloadToken,
		reloadChannel: make(chan struct{}, 1), 
	}
}

func (m *ProxyDefinitionsManager) LoadDefinitions() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return err
	}

	var newDefinitions []ProxyDefinition
	if err := json.Unmarshal(data, &newDefinitions); err != nil {
		return err
	}

	m.definitions = newDefinitions
	return nil
}

func (m *ProxyDefinitionsManager) GetDefinitions() []ProxyDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	defsCopy := make([]ProxyDefinition, len(m.definitions))
	copy(defsCopy, m.definitions)
	return defsCopy
}

func (m *ProxyDefinitionsManager) ReloadChannel() <-chan struct{} {
	return m.reloadChannel
}

func (m *ProxyDefinitionsManager) TriggerReload() {
	select {
	case m.reloadChannel <- struct{}{}:
	default:
	}
}

func (m *ProxyDefinitionsManager) CheckReloadToken(token string) bool {
	return m.reloadToken != "" && m.reloadToken == token
}