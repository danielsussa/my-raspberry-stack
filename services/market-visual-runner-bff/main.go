package main

import (
	"bufio"
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"errors"
	"encoding/hex"
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

	"github.com/gorilla/websocket"
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
	Resolution       string                 `json:"resolution"`
	FrameQuality     []symbolFrameQuality   `json:"frame_quality"`
}

type symbolFrameQuality struct {
	Symbol                string `json:"symbol"`
	Quality               []int  `json:"quality"`
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

type computeState struct {
	ComputeMode bool           `json:"compute_mode"`
	RangeStart  int            `json:"range_start"`
	RangeEnd    int            `json:"range_end"`
	Markers     map[string]int `json:"markers,omitempty"`
	TicksRequested int         `json:"ticks_requested"`
	LastSymbol     string      `json:"last_symbol,omitempty"`
	RangeStartTime string      `json:"range_start_time,omitempty"`
	RangeEndTime   string      `json:"range_end_time,omitempty"`
	Resolution     string      `json:"resolution,omitempty"`
	CustomResolutionSeconds int `json:"custom_resolution_seconds,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type sessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*computeState
}

type wsRequest struct {
	Type       string   `json:"type"`
	RequestID  string   `json:"request_id,omitempty"`
	Symbol     string   `json:"symbol,omitempty"`
	Symbols    []string `json:"symbols,omitempty"`
	Start      string   `json:"start,omitempty"`
	End        string   `json:"end,omitempty"`
	RangeStart int      `json:"range_start,omitempty"`
	RangeEnd   int      `json:"range_end,omitempty"`
	ComputeMode *bool  `json:"compute_mode,omitempty"`
	Resolution int      `json:"resolution,omitempty"`
	Ticks      int      `json:"ticks,omitempty"`
	State      *computeStatePayload `json:"state,omitempty"`
}

type wsResponse struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Data      any    `json:"data,omitempty"`
	Message   string `json:"message,omitempty"`
}

type wsPriceOverviewItem struct {
	Symbol string                `json:"symbol"`
	Data   *priceOverviewResponse `json:"data,omitempty"`
}

type wsIncreaseResolutionPayload struct {
	ResolutionSeconds int                  `json:"resolution_seconds"`
	Items             []wsPriceOverviewItem `json:"items"`
}

type computeStatePayload struct {
	ComputeMode bool           `json:"compute_mode"`
	RangeStart  int            `json:"range_start"`
	RangeEnd    int            `json:"range_end"`
	Markers     map[string]int `json:"markers,omitempty"`
	TicksRequested int         `json:"ticks_requested"`
	LastSymbol     string      `json:"last_symbol,omitempty"`
	RangeStartTime string      `json:"range_start_time,omitempty"`
	RangeEndTime   string      `json:"range_end_time,omitempty"`
	Resolution     string      `json:"resolution,omitempty"`
	CustomResolutionSeconds int `json:"custom_resolution_seconds,omitempty"`
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
	dataDirs := parseDirs(envOrDefault("DATA_DIRS", "/data/cedro-ticker-uploader,/data/massive-ticker-uploader"))
	cacheTTL := time.Minute
	refreshInterval := 30 * time.Minute
	cache := &timeframeCache{}
	store := newDataStore()
	sessions := newSessionManager()

	if err := store.loadFromDirs(dataDirs); err != nil {
		log.Printf("failed to preload data: %v", err)
	}
	go startDataReloader(refreshInterval, dataDirs, store, cache)

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

	mux.HandleFunc("/ws", handleWebsocket(store, cache, cacheTTL, allowedOrigins, dataDirs, sessions))

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

func handleWebsocket(store *dataStore, cache *timeframeCache, cacheTTL time.Duration, allowedOrigins []string, dataDirs []string, sessions *sessionManager) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return originAllowed(origin, allowedOrigins)
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, created := sessions.getOrCreateID(r)
		headers := http.Header{}
		if created {
			headers.Add("Set-Cookie", buildSessionCookie(sessionID))
		}
		conn, err := upgrader.Upgrade(w, r, headers)
		if err != nil {
			log.Printf("ws upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		for {
			var msg wsRequest
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				log.Printf("ws read error: %v", err)
				return
			}

			switch strings.TrimSpace(msg.Type) {
			case "state_get":
				state := sessions.getState(sessionID)
				_ = conn.WriteJSON(wsResponse{Type: "state", RequestID: msg.RequestID, Data: state})

			case "state_update":
				if msg.State == nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "missing state"})
					continue
				}
				sessions.setState(sessionID, msg.State.toComputeState())
				_ = conn.WriteJSON(wsResponse{Type: "state_update", RequestID: msg.RequestID, Data: map[string]string{"status": "ok"}})

			case "range_selection":
				start, end, err := parseStartEndStrings(msg.Start, msg.End)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: err.Error()})
					continue
				}
				sessions.updateRange(sessionID, start, end, msg.RangeStart, msg.RangeEnd, msg.ComputeMode)
				_ = conn.WriteJSON(wsResponse{Type: "range_selection", RequestID: msg.RequestID, Data: map[string]string{"status": "ok"}})

			case "state_reset":
				state := sessions.resetState(sessionID)
				_ = conn.WriteJSON(wsResponse{Type: "state_reset", RequestID: msg.RequestID, Data: state})

			case "timeframe":
				resp, err := cache.getOrBuild(cacheTTL, func() (timeframeResponse, error) {
					return store.buildTimeframeResponse()
				})
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "could not build timeframe"})
					continue
				}
				_ = conn.WriteJSON(wsResponse{Type: "timeframe", RequestID: msg.RequestID, Data: resp})

			case "price_overview":
				symbol := strings.TrimSpace(msg.Symbol)
				if symbol == "" {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "missing symbol"})
					continue
				}
				start, end, err := parseStartEndStrings(msg.Start, msg.End)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: err.Error()})
					continue
				}
				resolutionSeconds, err := parseResolutionValue(msg.Resolution)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: err.Error()})
					continue
				}
				resp, ok, err := store.buildPriceOverview(symbol, start, end, resolutionSeconds)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "could not build price overview"})
					continue
				}
				if !ok {
					_ = conn.WriteJSON(wsResponse{Type: "price_overview", RequestID: msg.RequestID, Data: nil})
					continue
				}
				_ = conn.WriteJSON(wsResponse{Type: "price_overview", RequestID: msg.RequestID, Data: resp})

			case "price_overview_batch":
				start, end, err := parseStartEndStrings(msg.Start, msg.End)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: err.Error()})
					continue
				}
				resolutionSeconds, err := parseResolutionValue(msg.Resolution)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: err.Error()})
					continue
				}
				items := make([]wsPriceOverviewItem, 0, len(msg.Symbols))
				for _, rawSymbol := range msg.Symbols {
					symbol := strings.TrimSpace(rawSymbol)
					if symbol == "" {
						continue
					}
					resp, ok, err := store.buildPriceOverview(symbol, start, end, resolutionSeconds)
					if err != nil {
						_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "could not build price overview"})
						items = nil
						break
					}
					if !ok {
						items = append(items, wsPriceOverviewItem{Symbol: symbol})
						continue
					}
					respCopy := resp
					items = append(items, wsPriceOverviewItem{Symbol: symbol, Data: &respCopy})
				}
				if items == nil {
					continue
				}
				_ = conn.WriteJSON(wsResponse{Type: "price_overview_batch", RequestID: msg.RequestID, Data: items})

			case "compute_mode":
				start, end, err := parseStartEndStrings(msg.Start, msg.End)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: err.Error()})
					continue
				}
				if err := store.loadFromDirsRange(dataDirs, start, end); err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "could not load range"})
					continue
				}
				cache.reset()
				_ = conn.WriteJSON(wsResponse{Type: "compute_mode", RequestID: msg.RequestID, Data: map[string]string{"status": "ok"}})

			case "increase_resolution":
				start, end, err := parseStartEndStrings(msg.Start, msg.End)
				if err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: err.Error()})
					continue
				}
				ticks := msg.Ticks
				if ticks <= 0 {
					ticks = 5000
				}
				if err := store.loadFromDirsRange(dataDirs, start, end); err != nil {
					_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "could not load range"})
					continue
				}
				cache.reset()
				resolutionSeconds := computeResolutionSecondsForTicks(start, end, ticks)
				symbols := msg.Symbols
				if len(symbols) == 0 {
					symbols = store.listSymbols()
				}
				items := make([]wsPriceOverviewItem, 0, len(symbols))
				for _, rawSymbol := range symbols {
					symbol := strings.TrimSpace(rawSymbol)
					if symbol == "" {
						continue
					}
					resp, ok, err := store.buildPriceOverview(symbol, start, end, resolutionSeconds)
					if err != nil {
						_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "could not build price overview"})
						items = nil
						break
					}
					if !ok {
						items = append(items, wsPriceOverviewItem{Symbol: symbol})
						continue
					}
					respCopy := resp
					items = append(items, wsPriceOverviewItem{Symbol: symbol, Data: &respCopy})
				}
				if items == nil {
					continue
				}
				payload := wsIncreaseResolutionPayload{
					ResolutionSeconds: resolutionSeconds,
					Items:             items,
				}
				_ = conn.WriteJSON(wsResponse{Type: "increase_resolution", RequestID: msg.RequestID, Data: payload})

			default:
				_ = conn.WriteJSON(wsResponse{Type: "error", RequestID: msg.RequestID, Message: "unknown message type"})
			}
		}
	}
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

func parseDirs(value string) []string {
	parts := strings.Split(value, ",")
	dirs := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		dirs = append(dirs, trimmed)
	}
	return dirs
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

func parseStartEndStrings(startRaw, endRaw string) (time.Time, time.Time, error) {
	startRaw = strings.TrimSpace(startRaw)
	endRaw = strings.TrimSpace(endRaw)

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

func parseResolutionSeconds(r *http.Request) (int, error) {
	query := r.URL.Query()
	raw := strings.TrimSpace(query.Get("resolution"))
	if raw == "" {
		return 300, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, errors.New("resolution must be a positive integer in seconds")
	}
	return seconds, nil
}

func parseResolutionValue(seconds int) (int, error) {
	if seconds == 0 {
		return 300, nil
	}
	if seconds < 0 {
		return 0, errors.New("resolution must be a positive integer in seconds")
	}
	return seconds, nil
}

func computeResolutionSecondsForTicks(start, end time.Time, ticks int) int {
	if ticks <= 1 {
		return 60
	}
	if end.Before(start) {
		return 60
	}
	totalSeconds := int(end.Sub(start).Seconds())
	if totalSeconds <= 0 {
		return 60
	}
	steps := ticks - 1
	seconds := totalSeconds / steps
	if totalSeconds%steps != 0 {
		seconds += 1
	}
	if seconds < 1 {
		return 1
	}
	return seconds
}

func newSessionManager() *sessionManager {
	return &sessionManager{
		sessions: make(map[string]*computeState),
	}
}

func (m *sessionManager) getOrCreateID(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie("mvr_session"); err == nil && cookie.Value != "" {
		return cookie.Value, false
	}
	return newSessionID(), true
}

func (m *sessionManager) getState(id string) *computeState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if state, ok := m.sessions[id]; ok {
		return state
	}
	return nil
}

func (m *sessionManager) setState(id string, state *computeState) {
	if id == "" || state == nil {
		return
	}
	state.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	m.sessions[id] = state
	m.mu.Unlock()
}

func (m *sessionManager) updateRange(id string, start, end time.Time, rangeStart, rangeEnd int, computeMode *bool) {
	if id == "" {
		return
	}
	m.mu.Lock()
	state, ok := m.sessions[id]
	if !ok || state == nil {
		state = &computeState{}
		m.sessions[id] = state
	}
	state.RangeStart = rangeStart
	state.RangeEnd = rangeEnd
	state.RangeStartTime = start.UTC().Format(time.RFC3339Nano)
	state.RangeEndTime = end.UTC().Format(time.RFC3339Nano)
	if computeMode != nil {
		state.ComputeMode = *computeMode
	}
	state.UpdatedAt = time.Now().UTC()
	m.mu.Unlock()
}

func (m *sessionManager) resetState(id string) *computeState {
	if id == "" {
		return nil
	}
	m.mu.Lock()
	state := &computeState{
		ComputeMode: false,
		RangeStart:  0,
		RangeEnd:    0,
		Markers:     nil,
		TicksRequested: 0,
		LastSymbol:     "",
		RangeStartTime: "",
		RangeEndTime:   "",
		Resolution:     "",
		CustomResolutionSeconds: 0,
		UpdatedAt:      time.Now().UTC(),
	}
	m.sessions[id] = state
	m.mu.Unlock()
	return state
}

func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(b)
}

func buildSessionCookie(id string) string {
	return (&http.Cookie{
		Name:     "mvr_session",
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}).String()
}

func (p *computeStatePayload) toComputeState() *computeState {
	if p == nil {
		return nil
	}
	state := &computeState{
		ComputeMode: p.ComputeMode,
		RangeStart:  p.RangeStart,
		RangeEnd:    p.RangeEnd,
		TicksRequested: p.TicksRequested,
		LastSymbol:     p.LastSymbol,
		RangeStartTime: p.RangeStartTime,
		RangeEndTime:   p.RangeEndTime,
		Resolution:     p.Resolution,
		CustomResolutionSeconds: p.CustomResolutionSeconds,
	}
	if len(p.Markers) > 0 {
		state.Markers = make(map[string]int, len(p.Markers))
		for k, v := range p.Markers {
			state.Markers[k] = v
		}
	}
	return state
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

func (s *dataStore) loadFromDirs(rootDirs []string) error {
	startTS := int64(0)
	endTS := int64(0)
	quality := make(map[string]map[int64]bool)
	prices := make(map[string]map[int64]minutePrice)

	for _, rootDir := range rootDirs {
		if strings.TrimSpace(rootDir) == "" {
			continue
		}
		if _, err := os.Stat(rootDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := loadFromDir(rootDir, quality, prices, &startTS, &endTS); err != nil {
			return err
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

func (s *dataStore) loadFromDirsRange(rootDirs []string, start, end time.Time) error {
	startTS := int64(0)
	endTS := int64(0)
	quality := make(map[string]map[int64]bool)
	prices := make(map[string]map[int64]minutePrice)

	startMs := start.UTC().UnixMilli()
	endMs := end.UTC().UnixMilli()

	for _, rootDir := range rootDirs {
		if strings.TrimSpace(rootDir) == "" {
			continue
		}
		if _, err := os.Stat(rootDir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if err := loadFromDirRange(rootDir, startMs, endMs, quality, prices, &startTS, &endTS); err != nil {
			return err
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

func loadFromDir(rootDir string, quality map[string]map[int64]bool, prices map[string]map[int64]minutePrice, startTS, endTS *int64) error {
	dateDirs, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}

	for _, dateEntry := range dateDirs {
		if !dateEntry.IsDir() {
			continue
		}
		dateName := dateEntry.Name()
		datePath := filepath.Join(rootDir, dateName)
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
				updateRangeFromPath(dateName, name, startTS, endTS)
				path := filepath.Join(symbolPath, name)
				if err := ingestFile(path, quality, prices, startTS, endTS); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func loadFromDirRange(rootDir string, startMs, endMs int64, quality map[string]map[int64]bool, prices map[string]map[int64]minutePrice, startTS, endTS *int64) error {
	dateDirs, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}

	for _, dateEntry := range dateDirs {
		if !dateEntry.IsDir() {
			continue
		}
		dateName := dateEntry.Name()
		datePath := filepath.Join(rootDir, dateName)
		symbolDirs, err := os.ReadDir(datePath)
		if err != nil {
			return err
		}
		for _, symbolEntry := range symbolDirs {
			if !symbolEntry.IsDir() {
				continue
			}
			symbolPath := filepath.Join(datePath, symbolEntry.Name())
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
				ts, ok := parseDirFileTimestamp(dateName, name)
				if !ok {
					continue
				}
				if ts < startMs || ts > endMs {
					continue
				}
				updateRangeFromPath(dateName, name, startTS, endTS)
				path := filepath.Join(symbolPath, name)
				if err := ingestFile(path, quality, prices, startTS, endTS); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func updateRangeFromPath(dateName, fileName string, minTS, maxTS *int64) {
	ts, ok := parseDirFileTimestamp(dateName, fileName)
	if !ok {
		return
	}
	if *minTS == 0 || ts < *minTS {
		*minTS = ts
	}
	if *maxTS == 0 || ts > *maxTS {
		*maxTS = ts
	}
}

func parseDirFileTimestamp(dateName, fileName string) (int64, bool) {
	dateParts := strings.Split(dateName, "-")
	if len(dateParts) != 3 {
		return 0, false
	}
	year, err := strconv.Atoi(dateParts[0])
	if err != nil {
		return 0, false
	}
	month, err := strconv.Atoi(dateParts[1])
	if err != nil || month < 1 || month > 12 {
		return 0, false
	}
	day, err := strconv.Atoi(dateParts[2])
	if err != nil || day < 1 || day > 31 {
		return 0, false
	}

	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	timeParts := strings.Split(baseName, "_")
	if len(timeParts) != 2 {
		return 0, false
	}
	hour, err := strconv.Atoi(timeParts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, false
	}
	minute, err := strconv.Atoi(timeParts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, false
	}

	t := time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
	return t.UnixMilli(), true
}

func (s *dataStore) buildTimeframeResponse() (timeframeResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.startTS <= 0 || s.endTS <= 0 || len(s.qualityBySymbol) == 0 {
		now := time.Now().UTC()
		return timeframeResponse{
			Start:            now.Format(time.RFC3339),
			End:              now.Format(time.RFC3339),
			Resolution:       "1m",
			FrameQuality:     []symbolFrameQuality{},
		}, nil
	}

	startTime := time.UnixMilli(s.startTS).UTC()
	endTime := time.UnixMilli(s.endTS).UTC()
	startMinute := startTime.Truncate(time.Minute)
	endMinute := endTime.Truncate(time.Minute)
	totalMinutes := int(endMinute.Sub(startMinute).Minutes())
	if totalMinutes < 0 {
		totalMinutes = 0
	}
	resolutionMinutes := 1
	resolutionLabel := "1m"
	switch {
	case totalMinutes > 7*24*60:
		resolutionMinutes = 12 * 60
		resolutionLabel = "12h"
	case totalMinutes > 24*60:
		resolutionMinutes = 60
		resolutionLabel = "1h"
	case totalMinutes > 6*60:
		resolutionMinutes = 10
		resolutionLabel = "10m"
	case totalMinutes > 2*60:
		resolutionMinutes = 5
		resolutionLabel = "5m"
	}
	bucketCount := totalMinutes/resolutionMinutes + 1

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
		flags := make([]int, bucketCount)
		for minute := range s.qualityBySymbol[symbol] {
			tsTime := time.Unix(minute, 0).UTC().Truncate(time.Minute)
			index := int(tsTime.Sub(startMinute).Minutes()) / resolutionMinutes
			if index >= 0 && index < bucketCount {
				flags[index] = 1
			}
		}
		quality = append(quality, symbolFrameQuality{
			Symbol:  symbol,
			Quality: flags,
		})
	}

	return timeframeResponse{
		Start:            startTime.Format(time.RFC3339),
		End:              endTime.Format(time.RFC3339),
		Resolution:       resolutionLabel,
		FrameQuality:     quality,
	}, nil
}

func (s *dataStore) buildPriceOverview(symbol string, start, end time.Time, resolutionSeconds int) (priceOverviewResponse, bool, error) {
	start = start.UTC().Truncate(time.Second)
	end = end.UTC().Truncate(time.Second)
	if resolutionSeconds <= 0 {
		resolutionSeconds = 300
	}
	resolutionDuration := time.Duration(resolutionSeconds) * time.Second
	if end.Before(start) {
		end = start
	}
	totalSeconds := int(end.Sub(start).Seconds())
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	buckets := totalSeconds/resolutionSeconds + 1

	datetimes := make([]string, 0, buckets)
	prices := make([]*float64, 0, buckets)

	s.mu.RLock()
	points := s.priceBySymbol[symbol]
	s.mu.RUnlock()
	if len(points) == 0 {
		return priceOverviewResponse{}, false, nil
	}

	hasAny := false
	for i := 0; i < buckets; i++ {
		bucketStart := start.Add(time.Duration(i) * resolutionDuration)
		if bucketStart.After(end) {
			break
		}
		bucketEnd := bucketStart.Add(resolutionDuration - time.Second)
		if bucketEnd.After(end) {
			bucketEnd = end
		}
		datetimes = append(datetimes, formatDateTime(bucketStart))

		var latest *float64
		if resolutionSeconds < 60 {
			key := bucketEnd.Truncate(time.Minute).Unix()
			if point, ok := points[key]; ok {
				value := point.price
				latest = &value
			}
		} else {
			for t := bucketStart.Truncate(time.Minute); !t.After(bucketEnd); t = t.Add(time.Minute) {
				key := t.Unix()
				point, ok := points[key]
				if !ok {
					continue
				}
				value := point.price
				latest = &value
			}
		}
		if latest == nil {
			prices = append(prices, nil)
			continue
		}
		prices = append(prices, latest)
		hasAny = true
	}

	if !hasAny {
		return priceOverviewResponse{}, false, nil
	}

	return priceOverviewResponse{
		Resolution: strconv.Itoa(resolutionSeconds) + "s",
		Prices:     prices,
		Datetimes:  datetimes,
	}, true, nil
}

func (s *dataStore) listSymbols() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.qualityBySymbol) == 0 {
		return nil
	}
	symbols := make([]string, 0, len(s.qualityBySymbol))
	for symbol := range s.qualityBySymbol {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

func ingestFile(path string, quality map[string]map[int64]bool, prices map[string]map[int64]minutePrice, minTS, maxTS *int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return scanner.Err()
	}
	firstLine := strings.TrimSpace(scanner.Text())
	if firstLine == "" {
		return nil
	}

	if strings.Contains(firstLine, "|") && !strings.Contains(firstLine, ",") {
		if err := ingestCedroLine(firstLine, path, quality, prices, minTS, maxTS); err != nil {
			return err
		}
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if err := ingestCedroLine(line, path, quality, prices, minTS, maxTS); err != nil {
				return err
			}
		}
		return scanner.Err()
	}

	headers, err := parseCSVHeader(firstLine)
	if err != nil {
		return err
	}
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	return ingestCSVWithHeaders(reader, headers, path, quality, prices, minTS, maxTS)
}

func parseCSVHeader(line string) ([]string, error) {
	reader := csv.NewReader(strings.NewReader(line))
	reader.FieldsPerRecord = -1
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}
	return headers, nil
}

func ingestCSVWithHeaders(reader *csv.Reader, headers []string, path string, quality map[string]map[int64]bool, prices map[string]map[int64]minutePrice, minTS, maxTS *int64) error {
	idxTime := indexOf(headers, "time_msc")
	if idxTime == -1 {
		idxTime = indexOf(headers, "t")
	}
	if idxTime == -1 {
		return errors.New("missing time column")
	}
	idxLast := indexOf(headers, "last")
	idxBid := indexOf(headers, "bid")
	idxAsk := indexOf(headers, "ask")
	idxPrice := indexOf(headers, "p")

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
		ts, ok := parseTimestamp(record[idxTime])
		if !ok {
			continue
		}
		price, ok := parsePrice(record, idxLast, idxBid, idxAsk)
		if !ok && idxPrice >= 0 && idxPrice < len(record) {
			price, ok = parseFloat(record[idxPrice])
		}
		if !ok {
			continue
		}
		applyPoint(path, ts, price, quality, prices, minTS, maxTS)
	}
}

func ingestCedroLine(line, path string, quality map[string]map[int64]bool, prices map[string]map[int64]minutePrice, minTS, maxTS *int64) error {
	parts := strings.Split(line, "|")
	if len(parts) < 2 {
		return nil
	}
	ts, ok := parseTimestamp(parts[0])
	if !ok {
		return nil
	}
	fields := strings.Split(parts[1], ":")
	if len(fields) < 5 {
		return nil
	}
	price, ok := parseFloat(fields[4])
	if !ok {
		return nil
	}
	applyPoint(path, ts, price, quality, prices, minTS, maxTS)
	return nil
}

func applyPoint(path string, ts int64, price float64, quality map[string]map[int64]bool, prices map[string]map[int64]minutePrice, minTS, maxTS *int64) {
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
}

func parseTimestamp(value string) (int64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	ts, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, false
	}
	if ts < 10_000_000_000 {
		ts *= 1000
	}
	return ts, true
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

func (c *timeframeCache) reset() {
	c.mu.Lock()
	c.payload = timeframeResponse{}
	c.updatedAt = time.Time{}
	c.mu.Unlock()
}

func startDataReloader(interval time.Duration, dataDirs []string, store *dataStore, cache *timeframeCache) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := store.loadFromDirs(dataDirs); err != nil {
			log.Printf("failed to reload data: %v", err)
			continue
		}
		cache.reset()
	}
}
