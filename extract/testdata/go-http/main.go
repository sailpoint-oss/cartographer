package main

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// RequireRights is a fictional auth middleware marker used by golden tests.
func RequireRights(scope string, next http.Handler) http.Handler {
	return next
}

// Item is a sample JSON response type.
type Item struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func listItems(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode([]Item{{ID: 1, Name: "alpha"}})
}

func main() {
	r := mux.NewRouter()
	r.Handle("/api/v1/items",
		RequireRights("api:resource:read", http.HandlerFunc(listItems)),
	).Methods(http.MethodGet)
	_ = http.ListenAndServe(":8080", r)
}
