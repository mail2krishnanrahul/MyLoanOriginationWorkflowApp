package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

func main() {
	root := http.NewServeMux()
	api := http.NewServeMux()

	api.HandleFunc("POST /ingest/deals", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("Matched!"))
	})

	root.Handle("/", api) // Direct nesting, no middleware

	req := httptest.NewRequest("POST", "/ingest/deals", nil)
	w := httptest.NewRecorder()

	root.ServeHTTP(w, req)

	fmt.Printf("Status: %d\nBody: %s\n", w.Result().StatusCode, w.Body.String())
}
