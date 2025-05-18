package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/things-go/go-socks5"
)

type ClientConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Allowed  bool   `json:"allowed"`
}

type MultiAuth struct {
	clients map[string]ClientConfig
	mu      sync.RWMutex
}

// DefaultAuth is the default global authentication instance.
// Note: For testing purposes, use ResetDefaultAuth() to reset this instance between tests
// to ensure test isolation. In production code, prefer creating your own MultiAuth instance
// using New() when possible to avoid global state.
var DefaultAuth = New()

// ResetDefaultAuth resets the DefaultAuth instance to a fresh state.
// This is primarily intended for testing to ensure test isolation.
// Example usage in tests:
//   func TestSomething(t *testing.T) {
//     defer auth.ResetDefaultAuth()
//     // Test code that uses DefaultAuth
//   }
func ResetDefaultAuth() {
	DefaultAuth = New()
}

var _ socks5.CredentialStore = (*MultiAuth)(nil)

func New() *MultiAuth { 
	return &MultiAuth{
		clients: make(map[string]ClientConfig),
	}
}

func (a *MultiAuth) AddClient(username, password string, allowed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clients[username] = ClientConfig{Username: username, Password: password, Allowed: allowed}
}

func (a *MultiAuth) Valid(username, password, addr string) bool {
	a.mu.RLock()
	client, ok := a.clients[username]
	a.mu.RUnlock()

	if !ok {
		log.Printf("Auth attempt: client not found '%s'", username)
		return false
	}
	if !client.Allowed {
		log.Printf("Auth attempt: client access denied for '%s'", username)
		return false
	}
	if client.Password != password {
		log.Printf("Auth attempt: invalid password for '%s'", username)
		return false
	}
	log.Printf("Auth success for client '%s'", username)
	return true
}

// LoadUsersFromFile loads users from a JSON file
func LoadUsersFromFile(filePath string) ([]ClientConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read users file %q: %w", filePath, err)
	}

	var users []ClientConfig
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("failed to unmarshal users JSON from %q: %w", filePath, err)
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("no users found in file %q", filePath)
	}

	return users, nil
}

// SetUsers sets the users in the default authentication instance
func SetUsers(users []ClientConfig) {
	DefaultAuth.mu.Lock()
	defer DefaultAuth.mu.Unlock()

	DefaultAuth.clients = make(map[string]ClientConfig)
	for _, user := range users {
		DefaultAuth.clients[user.Username] = user
	}
}

// GetCredentialStore returns the default authentication instance as a credential store
// that can be used with the SOCKS5 server for user authentication.
func GetCredentialStore() socks5.CredentialStore {
	return DefaultAuth
}