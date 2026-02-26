package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type statusResponse struct {
	Status  string `json:"status"`
	Uptime  string `json:"uptime"`
	TimeUTC string `json:"time_utc"`
	Version string `json:"version"`
}

type timeframeResponse struct {
	Start            string                 `json:"start"`
	End              string                 `json:"end"`
	QualityPerSymbol []symbolFrameQuality   `json:"quality_per_symbol"`
}

type symbolFrameQuality struct {
	Symbol                string `json:"symbol"`
	FrameQualityPerMinute []int  `json:"frame_quality_per_minute"`
}

type timeframeCache struct {
	mu        sync.RWMutex
	updatedAt time.Time
	payload   timeframeResponse
}

func main() {
	start := time.Now().UTC()
	port := envOrDefault("PORT", "8080")
	version := envOrDefault("APP_VERSION", "dev")
	allowedOrigins := parseOrigins(envOrDefault("BFF_ALLOWED_ORIGINS", "*"))
	dataDir := envOrDefault("MT5_DATA_DIR", "/data/mt5-ticker-uploader")
	cacheTTL := time.Minute
	cache := &timeframeCache{}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		resp := statusResponse{
			Status:  "ready",
			Uptime:  time.Since(start).Truncate(time.Second).String(),
			TimeUTC: time.Now().UTC().Format(time.RFC3339),
			Version: version,
		}

		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/timeframe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		resp, err := cache.getOrBuild(cacheTTL, func() (timeframeResponse, error) {
			return buildTimeframeResponse(dataDir)
		})
		if err != nil {
			http.Error(w, "could not build timeframe", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "market-visual-runner-bff",
			"status":  "ready",
			"version": version,
		})
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           withCORS(mux, allowedOrigins),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("market-visual-runner-bff listening on :%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

func withCORS(next http.Handler, allowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && originAllowed(origin, allowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func originAllowed(origin string, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		return false
	}
	if len(allowedOrigins) == 1 && allowedOrigins[0] == "*" {
		return true
	}
	for _, allowed := range allowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}
	return false
}

func parseOrigins(value string) []string {
	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		origins = append(origins, trimmed)
	}
	return origins
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(true)
	_ = encoder.Encode(payload)
}

func (c *timeframeCache) getOrBuild(ttl time.Duration, build func() (timeframeResponse, error)) (timeframeResponse, error) {
	c.mu.RLock()
	if !c.updatedAt.IsZero() && time.Since(c.updatedAt) < ttl {
		cached := c.payload
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.updatedAt.IsZero() && time.Since(c.updatedAt) < ttl {
		return c.payload, nil
	}

	payload, err := build()
	if err != nil {
		return timeframeResponse{}, err
	}
	c.payload = payload
	c.updatedAt = time.Now()
	return payload, nil
}

func buildTimeframeResponse(rootDir string) (timeframeResponse, error) {
	symbolFiles, err := collectSymbolFiles(rootDir)
	if err != nil {
		return timeframeResponse{}, err
	}

	if len(symbolFiles) == 0 {
		now := time.Now().UTC()
		empty := timeframeResponse{
			Start:            now.Format(time.RFC3339),
			End:              now.Format(time.RFC3339),
			QualityPerSymbol: []symbolFrameQuality{},
		}
		return empty, nil
	}

	var minTS int64 = -1
	var maxTS int64 = -1
	for _, timestamps := range symbolFiles {
		for _, ts := range timestamps {
			if minTS == -1 || ts < minTS {
				minTS = ts
			}
			if maxTS == -1 || ts > maxTS {
				maxTS = ts
			}
		}
	}

	if minTS <= 0 || maxTS <= 0 {
		return timeframeResponse{}, nil
	}

	startTime := time.UnixMilli(minTS).UTC()
	endTime := time.UnixMilli(maxTS).UTC()
	startMinute := startTime.Truncate(time.Minute)
	endMinute := endTime.Truncate(time.Minute)
	minuteCount := int(endMinute.Sub(startMinute).Minutes()) + 1
	if minuteCount < 1 {
		minuteCount = 1
	}

	symbols := make([]string, 0, len(symbolFiles))
	for symbol := range symbolFiles {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)

	quality := make([]symbolFrameQuality, 0, len(symbols))
	for _, symbol := range symbols {
		flags := make([]int, minuteCount)
		for _, ts := range symbolFiles[symbol] {
			tsTime := time.UnixMilli(ts).UTC().Truncate(time.Minute)
			index := int(tsTime.Sub(startMinute).Minutes())
			if index >= 0 && index < minuteCount {
				flags[index] = 1
			}
		}
		quality = append(quality, symbolFrameQuality{
			Symbol:                symbol,
			FrameQualityPerMinute: flags,
		})
	}

	resp := timeframeResponse{
		Start:            startTime.Format(time.RFC3339),
		End:              endTime.Format(time.RFC3339),
		QualityPerSymbol: quality,
	}
	return resp, nil
}

func collectSymbolFiles(rootDir string) (map[string][]int64, error) {
	result := make(map[string][]int64)

	dateDirs, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}

	for _, dateEntry := range dateDirs {
		if !dateEntry.IsDir() {
			continue
		}
		datePath := filepath.Join(rootDir, dateEntry.Name())
		symbolDirs, err := os.ReadDir(datePath)
		if err != nil {
			return nil, err
		}
		for _, symbolEntry := range symbolDirs {
			if !symbolEntry.IsDir() {
				continue
			}
			symbol := symbolEntry.Name()
			symbolPath := filepath.Join(datePath, symbol)
			files, err := os.ReadDir(symbolPath)
			if err != nil {
				return nil, err
			}
			for _, fileEntry := range files {
				if fileEntry.IsDir() {
					continue
				}
				name := fileEntry.Name()
				if !strings.HasSuffix(name, ".csv") {
					continue
				}
				base := strings.TrimSuffix(name, ".csv")
				ts, err := strconv.ParseInt(base, 10, 64)
				if err != nil {
					continue
				}
				result[symbol] = append(result[symbol], ts)
			}
		}
	}

	return result, nil
}
