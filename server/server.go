package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	am "github.com/probeo-io/anymodel-go"
)

// Options configures the HTTP server.
type Options struct {
	Port   int
	Host   string
	Config *am.Config
}

// Start starts the anymodel HTTP server.
func Start(opts Options) error {
	if opts.Port == 0 {
		opts.Port = 4141
	}
	if opts.Host == "" {
		opts.Host = "0.0.0.0"
	}
	client := am.New(opts.Config)
	mux := NewHandler(client)
	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	log.Printf("anymodel server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

// NewHandler creates an HTTP handler for the anymodel API.
func NewHandler(client *am.Client) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req am.ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if req.Stream {
			handleStream(w, r, client, req)
			return
		}
		result, err := client.Chat.Completions.Create(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, result)
	})

	mux.HandleFunc("GET /api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		provider := r.URL.Query().Get("provider")
		models, err := client.Models.List(r.Context(), provider)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": models})
	})

	mux.HandleFunc("GET /api/v1/generation/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		stats := client.Generation.Get(id)
		if stats == nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, stats)
	})

	mux.HandleFunc("POST /api/v1/batches", func(w http.ResponseWriter, r *http.Request) {
		var req am.BatchCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		batch, err := client.Batches.Create(r.Context(), req)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, batch)
	})

	mux.HandleFunc("GET /api/v1/batches", func(w http.ResponseWriter, r *http.Request) {
		batches, err := client.Batches.List()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, batches)
	})

	mux.HandleFunc("GET /api/v1/batches/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		batch, err := client.Batches.Get(id)
		if err != nil {
			writeError(w, err)
			return
		}
		if batch == nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, batch)
	})

	mux.HandleFunc("GET /api/v1/batches/{id}/results", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		results, err := client.Batches.Results(id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, results)
	})

	mux.HandleFunc("POST /api/v1/batches/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		batch, err := client.Batches.Cancel(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, 200, batch)
	})

	return corsMiddleware(mux)
}

func handleStream(w http.ResponseWriter, r *http.Request, client *am.Client, req am.ChatCompletionRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming not supported"})
		return
	}
	chunkCh, errCh, err := client.Chat.Completions.CreateStream(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)

	for {
		select {
		case chunk, ok := <-chunkCh:
			if !ok {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case err, ok := <-errCh:
			if ok && err != nil {
				errData, _ := json.Marshal(map[string]string{"error": err.Error()})
				fmt.Fprintf(w, "data: %s\n\n", errData)
				flusher.Flush()
			}
			return
		}
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	if e, ok := err.(*am.Error); ok {
		writeJSON(w, e.Code, e.ToMap())
		return
	}
	writeJSON(w, 500, map[string]string{"error": err.Error()})
}
