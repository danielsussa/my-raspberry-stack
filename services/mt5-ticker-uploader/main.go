package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	maxUploadSize = 20 << 20 // 20 MB
	uploadDir     = "/data/mt5-ticker-uploader"
)

type uploadRequest struct {
	Symbol string `json:"symbol"`
	Ticks  []tick `json:"ticks"`
}

type tick struct {
	TimeMSC int64   `json:"time_msc"`
	Bid     float64 `json:"bid"`
	Ask     float64 `json:"ask"`
	Last    float64 `json:"last"`
	Volume  int64   `json:"volume"`
	Flags   int64   `json:"flags"`
}

func main() {
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/upload", uploadHandler)

	server := &http.Server{
		Addr:              ":8080",
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	defer r.Body.Close()

	var payload uploadRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if payload.Symbol == "" {
		http.Error(w, "missing symbol", http.StatusBadRequest)
		return
	}

	if len(payload.Ticks) == 0 {
		http.Error(w, "ticks must not be empty", http.StatusBadRequest)
		return
	}

	symbolDir := filepath.Join(uploadDir, payload.Symbol)
	if err := os.MkdirAll(symbolDir, 0o755); err != nil {
		http.Error(w, "could not create upload directory", http.StatusInternalServerError)
		return
	}

	timestamp := payload.Ticks[0].TimeMSC
	if timestamp <= 0 {
		timestamp = time.Now().UTC().UnixMilli()
	}

	outPath := filepath.Join(symbolDir, fmt.Sprintf("%d.csv", timestamp))
	outFile, err := os.Create(outPath)
	if err != nil {
		http.Error(w, "could not save file", http.StatusInternalServerError)
		return
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	if err := writer.Write([]string{"time_msc", "bid", "ask", "last", "volume", "flags"}); err != nil {
		http.Error(w, "could not write file", http.StatusInternalServerError)
		return
	}

	for _, tick := range payload.Ticks {
		row := []string{
			fmt.Sprintf("%d", tick.TimeMSC),
			fmt.Sprintf("%g", tick.Bid),
			fmt.Sprintf("%g", tick.Ask),
			fmt.Sprintf("%g", tick.Last),
			fmt.Sprintf("%d", tick.Volume),
			fmt.Sprintf("%d", tick.Flags),
		}
		if err := writer.Write(row); err != nil {
			http.Error(w, "could not write file", http.StatusInternalServerError)
			return
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		http.Error(w, "could not write file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
