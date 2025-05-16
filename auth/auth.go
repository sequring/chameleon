package auth // Имя пакета

import (
	"log"
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