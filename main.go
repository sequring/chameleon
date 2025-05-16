package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

func main() {
	r := mux.NewRouter()

	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello, Chameleon 🦎!")
	})

	fmt.Println("Server started: http://localhost:8081")
	http.ListenAndServe(":8081", r)
}