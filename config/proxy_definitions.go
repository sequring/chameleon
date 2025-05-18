package config

import (
	"encoding/json"
	"fmt"
	"log"
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
	filePath string
	mu       sync.RWMutex
	definitions []ProxyDefinition
}

func NewProxyDefinitionsManager(filePath string) *ProxyDefinitionsManager {
	return &ProxyDefinitionsManager{
		filePath: filePath,
		definitions: make([]ProxyDefinition, 0),
	}
}

// readAndParse reads and parses the proxy definitions file
func readAndParse(filePath string) ([]byte, []ProxyDefinition, error) {
	// Check if file exists and is readable
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("file does not exist: %s", filePath)
	} else if err != nil {
		return nil, nil, fmt.Errorf("error accessing file: %v", err)
	}

	// Read the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("error reading file: %v", err)
	}

	// Check if file is empty
	if len(data) == 0 {
		return data, []ProxyDefinition{}, nil
	}

	// Parse the JSON
	var defs []ProxyDefinition
	if err := json.Unmarshal(data, &defs); err != nil {
		return data, nil, fmt.Errorf("error parsing JSON: %v", err)
	}

	return data, defs, nil
}

func (m *ProxyDefinitionsManager) LoadDefinitions() error {
	// 1. read & parse without holding the lock
	data, defs, err := readAndParse(m.filePath)
	if err != nil {
		return err
	}

	// 2. swap slice under the lock – O(1)
	m.mu.Lock()
	defer m.mu.Unlock()

	// If file was empty, set empty slice and return
	if len(data) == 0 {
		m.definitions = []ProxyDefinition{}
		log.Println("Warning: Proxy definitions file is empty")
		return nil
	}

	// 3. Validate the loaded definitions
	seenAddrs := make(map[string]int) // track seen addresses and their first occurrence index
	for i, def := range defs {
		if def.Address == "" {
			return fmt.Errorf("proxy definition at index %d is missing required field 'address'", i)
		}
		// Check for duplicate addresses
		if firstIndex, exists := seenAddrs[def.Address]; exists {
			return fmt.Errorf("duplicate proxy address '%s' found at index %d (first occurrence at index %d)", def.Address, i, firstIndex)
		}
		seenAddrs[def.Address] = i
	}

	m.definitions = defs
	log.Printf("Loaded %d proxy definitions", len(defs))
	return nil
}

// findLineAndColumn finds the line and column number for a given offset in a byte slice
func findLineAndColumn(data []byte, offset int) (line, col int) {
	line = 1
	col = 1
	for i := 0; i < offset && i < len(data); i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func (m *ProxyDefinitionsManager) GetDefinitions() []ProxyDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	defsCopy := make([]ProxyDefinition, len(m.definitions))
	copy(defsCopy, m.definitions)
	return defsCopy
}