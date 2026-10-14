package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/infrai-examples/legal-matter-search/internal/infrai"
	"github.com/infrai-examples/legal-matter-search/internal/workflow"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	collection := os.Getenv("INFRAI_COLLECTION")
	if collection == "" {
		collection = "legal-matter-documents"
	}

	service := workflow.New(infrai.NewClient(key, nil), collection)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /matters/intake", func(w http.ResponseWriter, r *http.Request) {
		var input workflow.Intake
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		result, err := service.Ingest(r.Context(), input)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("POST /matters/answer", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			MatterID string `json:"matter_id"`
			Question string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		result, err := service.Ask(r.Context(), input.MatterID, input.Question)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("legal-search listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func writeServiceError(w http.ResponseWriter, err error) {
	var apiErr *infrai.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
		writeJSON(w, apiErr.StatusCode, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
		return
	}
	log.Printf("request failed: %v", err)
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "document processing request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
