package web

import (
	"fmt"
	"log"
	"net/http"

	"github.com/sequring/chameleon/config"
)

func StartProxyReloadHttpServer(addr string, manager *config.ProxyDefinitionsManager) {
	if addr == "" {
		log.Println("Proxy reload HTTP endpoint is disabled (no listen address specified).")
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/reload-proxies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		token := r.Header.Get("X-Reload-Token")
		if !manager.CheckReloadToken(token) {
			log.Printf("Unauthorized attempt to reload proxies from %s", r.RemoteAddr)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		log.Printf("Received authorized request to reload proxies from %s", r.RemoteAddr)
		if err := manager.LoadDefinitions(); err != nil {
			log.Printf("Error reloading proxy definitions: %v", err)
			http.Error(w, fmt.Sprintf("Error reloading proxy definitions: %v", err), http.StatusInternalServerError)
			return
		}
		
		manager.TriggerReload()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Proxy definitions reload triggered successfully.\n"))
		log.Println("Proxy definitions reload triggered successfully.")
	})

	log.Printf("Starting proxy reload HTTP server on %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("Failed to start proxy reload HTTP server: %v", err)
		}
	}()
}