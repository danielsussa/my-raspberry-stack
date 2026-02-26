package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
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

type priceOverviewResponse struct {
	Resolution string     `json:"resolution"`
	Prices     []*float64 `json:"prices"`
	Datetimes  []string   `json:"datetimes"`
}

type timeframeCache struct {
	mu        sync.RWMutex
	updatedAt time.Time
	payload   timeframeResponse
}

type dataStore struct {
	mu              sync.RWMutex
	startTS         int64
	endTS           int64
	qualityBySymbol map[string]map[int64]bool
	priceBySymbol   map[string]map[int64]minutePrice
}

func main() {
	start := time.Now().UTC()
	port := envOrDefault("PORT", "8080")
	version := envOrDefault("APP_VERSION", "dev")
	allowedOrigins := parseOrigins(envOrDefault("BFF_ALLOWED_ORIGINS", "*"))
	dataDir := envOrDefault("MT5_DATA_DIR", "/data/mt5-ticker-uploader")
	cacheTTL := time.Minute
	cache := &timeframeCache{}
	store := newDataStore()

	if err := store.loadFromDir(dataDir); err != nil {
		log.Printf("failed to preload data: %v", err)
	}

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
			return store.buildTimeframeResponse()
		})
		if err != nil {
			http.Error(w, "could not build timeframe", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/symbols/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/symbols/")
		if !strings.HasSuffix(path, "/price-overview") {
			http.NotFound(w, r)
			return
		}
		symbol := strings.TrimSuffix(path, "/price-overview")
		symbol = strings.Trim(symbol, "/")
		if symbol == "" {
			http.Error(w, "missing symbol", http.StatusBadRequest)
			return
		}

		start, end, err := parseStartEnd(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, ok, err := store.buildPriceOverview(symbol, start, end)
		if err != nil {
			http.Error(w, "could not build price overview", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "no data for symbol", http.StatusNotFound)
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

func parseStartEnd(r *http.Request) (time.Time, time.Time, error) {
	query := r.URL.Query()
	startRaw := strings.TrimSpace(query.Get("start"))
	endRaw := strings.TrimSpace(query.Get("end"))

	now := time.Now().UTC().Truncate(time.Minute)
	start := now.Add(-60 * time.Minute)
	end := now

	if startRaw != "" {
		parsed, err := parseDateTime(startRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		start = parsed
	}

	if endRaw != "" {
		parsed, err := parseDateTime(endRaw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		end = parsed
	}

	if end.Before(start) {
		return time.Time{}, time.Time{}, errors.New("end must be after start")
	}

	return start, end, nil
}

func parseDateTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("invalid datetime")
	}
	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		if ts > 10_000_000_000 {
			return time.UnixMilli(ts).UTC(), nil
		}
		return time.Unix(ts, 0).UTC(), nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, errors.New("invalid datetime format")
}

func formatDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

type minutePrice struct {
	ts    int64
	price float64
}

func parsePrice(record []string, idxLast, idxBid, idxAsk int) (float64, bool) {
	if idxLast >= 0 && idxLast < len(record) {
		if value, ok := parseFloat(record[idxLast]); ok {
			return value, true
		}
	}
	if idxBid >= 0 && idxBid < len(record) {
		if value, ok := parseFloat(record[idxBid]); ok {
			return value, true
		}
	}
	if idxAsk >= 0 && idxAsk < len(record) {
		if value, ok := parseFloat(record[idxAsk]); ok {
			return value, true
		}
	}
	return 0, false
}

func parseFloat(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func indexOf(values []string, key string) int {
	for i, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), key) {
			return i
		}
	}
	return -1
}

func newDataStore() *dataStore {
	return &dataStore{
		qualityBySymbol: make(map[string]map[int64]bool),
		priceBySymbol:   make(map[string]map[int64]minutePrice),
	}
}

func (s *dataStore) loadFromDir(rootDir string) error {
	startTS := int64(0)
	endTS := int64(0)
	quality := make(map[string]map[int64]bool)
	prices := make(map[string]map[int64]minutePrice)

	dateDirs, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}

	for _, dateEntry := range dateDirs {
		if !dateEntry.IsDir() {
			continue
		}
		datePath := filepath.Join(rootDir, dateEntry.Name())
		symbolDirs, err := os.ReadDir(datePath)
		if err != nil {
			return err
		}
		for _, symbolEntry := range symbolDirs {
			if !symbolEntry.IsDir() {
				continue
			}
			symbol := symbolEntry.Name()
			symbolPath := filepath.Join(datePath, symbol)
			files, err := os.ReadDir(symbolPath)
			if err != nil {
				return err
			}
			for _, fileEntry := range files {
				if fileEntry.IsDir() {
					continue
				}
				name := fileEntry.Name()
				if !strings.HasSuffix(name, ".csv") {
					continue
				}
				path := filepath.Join(symbolPath, name)
				if err := ingestCSV(path, quality, prices, &startTS, &endTS); err != nil {
					return err
				}
			}
		}
	}

	s.mu.Lock()
	s.startTS = startTS
	s.endTS = endTS
	s.qualityBySymbol = quality
	s.priceBySymbol = prices
	s.mu.Unlock()

	return nil
}

func (s *dataStore) buildTimeframeResponse() (timeframeResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.startTS <= 0 || s.endTS <= 0 || len(s.qualityBySymbol) == 0 {
		now := time.Now().UTC()
		return timeframeResponse{
			Start:            now.Format(time.RFC3339),
			End:              now.Format(time.RFC3339),
			QualityPerSymbol: []symbolFrameQuality{},
		}, nil
	}

	startTime := time.UnixMilli(s.startTS).UTC()
	endTime := time.UnixMilli(s.endTS).UTC()
	startMinute := startTime.Truncate(time.Minute)
	endMinute := endTime.Truncate(time.Minute)
	minuteCount := int(endMinute.Sub(startMinute).Minutes()) + 1
	if minuteCount < 1 {
		minuteCount = 1
	}

	symbols := make([]string, 0, len(s.qualityBySymbol))
	qualityCounts := make(map[string]int, len(s.qualityBySymbol))
	for symbol, minutes := range s.qualityBySymbol {
		symbols = append(symbols, symbol)
		qualityCounts[symbol] = len(minutes)
	}
	sort.Slice(symbols, func(i, j int) bool {
		ci := qualityCounts[symbols[i]]
		cj := qualityCounts[symbols[j]]
		if ci == cj {
			return symbols[i] < symbols[j]
		}
		return ci > cj
	})

	quality := make([]symbolFrameQuality, 0, len(symbols))
	for _, symbol := range symbols {
		flags := make([]int, minuteCount)
		for minute := range s.qualityBySymbol[symbol] {
			tsTime := time.Unix(minute, 0).UTC().Truncate(time.Minute)
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

	return timeframeResponse{
		Start:            startTime.Format(time.RFC3339),
		End:              endTime.Format(time.RFC3339),
		QualityPerSymbol: quality,
	}, nil
}

func (s *dataStore) buildPriceOverview(symbol string, start, end time.Time) (priceOverviewResponse, bool, error) {
	start = start.UTC().Truncate(time.Minute)
	end = end.UTC().Truncate(time.Minute)
	minutes := int(end.Sub(start).Minutes()) + 1
	if minutes < 1 {
		minutes = 1
	}

	datetimes := make([]string, 0, minutes)
	prices := make([]*float64, 0, minutes)

	s.mu.RLock()
	points := s.priceBySymbol[symbol]
	s.mu.RUnlock()
	if len(points) == 0 {
		return priceOverviewResponse{}, false, nil
	}

	hasAny := false
	for i := 0; i < minutes; i++ {
		t := start.Add(time.Duration(i) * time.Minute)
		datetimes = append(datetimes, formatDateTime(t))
		key := t.Unix()
		point, ok := points[key]
		if !ok {
			prices = append(prices, nil)
			continue
		}
		value := point.price
		prices = append(prices, &value)
		hasAny = true
	}

	if !hasAny {
		return priceOverviewResponse{}, false, nil
	}

	return priceOverviewResponse{
		Resolution: "1min",
		Prices:     prices,
		Datetimes:  datetimes,
	}, true, nil
}

func ingestCSV(path string, quality map[string]map[int64]bool, prices map[string]map[int64]minutePrice, minTS, maxTS *int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1

	headers, err := reader.Read()
	if err != nil {
		return err
	}

	idxTime := indexOf(headers, "time_msc")
	if idxTime == -1 {
		return errors.New("missing time_msc column")
	}
	idxLast := indexOf(headers, "last")
	idxBid := indexOf(headers, "bid")
	idxAsk := indexOf(headers, "ask")

	for {
		record, err := reader.Read()
		if err != nil {
			if err == csv.ErrFieldCount {
				continue
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if idxTime >= len(record) {
			continue
		}
		ts, err := strconv.ParseInt(strings.TrimSpace(record[idxTime]), 10, 64)
		if err != nil {
			continue
		}
		price, ok := parsePrice(record, idxLast, idxBid, idxAsk)
		if !ok {
			continue
		}
		minute := time.UnixMilli(ts).UTC().Truncate(time.Minute)
		key := minute.Unix()

		symbol := filepath.Base(filepath.Dir(path))
		if quality[symbol] == nil {
			quality[symbol] = make(map[int64]bool)
		}
		quality[symbol][key] = true

		if prices[symbol] == nil {
			prices[symbol] = make(map[int64]minutePrice)
		}
		current, exists := prices[symbol][key]
		if !exists || ts > current.ts {
			prices[symbol][key] = minutePrice{ts: ts, price: price}
		}

		if *minTS == 0 || ts < *minTS {
			*minTS = ts
		}
		if *maxTS == 0 || ts > *maxTS {
			*maxTS = ts
		}
	}
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
