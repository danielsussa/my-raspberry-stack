package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const uploadDir = "/data/massive-ticker-uploader"

type massiveTick struct {
	Ev  string  `json:"ev"`
	Sym string  `json:"sym"`
	I   string  `json:"i"`
	X   int64   `json:"x"`
	P   float64 `json:"p"`
	S   int64   `json:"s"`
	C   []int   `json:"c"`
	T   int64   `json:"t"`
	Q   int64   `json:"q"`
	Z   int64   `json:"z"`
	DS  string  `json:"ds"`
}

type actionMessage struct {
	Action string `json:"action"`
	Params string `json:"params"`
}

type statusMessage struct {
	Ev      string `json:"ev"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func main() {
	apiKey := strings.TrimSpace(os.Getenv("MASSIVE_API_KEY"))
	if apiKey == "" {
		log.Fatal("MASSIVE_API_KEY is required")
	}

	wssURL := strings.TrimSpace(os.Getenv("MASSIVE_WSS_URL"))
	if wssURL == "" {
		wssURL = "wss://delayed.massive.com/stocks"
	}

	subscribe := strings.TrimSpace(os.Getenv("MASSIVE_SUBSCRIBE"))
	if subscribe == "" {
		subscribe = "T.EWZ"
	}

	log.Printf("starting massive-ticker-uploader wss_url=%s subscribe=%s", wssURL, subscribe)

	backoff := 2 * time.Second
	for {
		if err := run(wssURL, apiKey, subscribe); err != nil {
			log.Printf("websocket error: %v", err)
		}

		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func run(wssURL, apiKey, subscribe string) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(wssURL, http.Header{})
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("connected to %s", wssURL)

	if err := conn.WriteJSON(actionMessage{Action: "auth", Params: apiKey}); err != nil {
		return err
	}

	log.Printf("auth sent")

	if err := waitForStatus(conn, "auth_success"); err != nil {
		return err
	}

	if err := conn.WriteJSON(actionMessage{Action: "subscribe", Params: subscribe}); err != nil {
		return err
	}

	log.Printf("subscribe sent: %s", subscribe)

	var messageCount int64
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		messageCount++
		if len(data) == 0 {
			continue
		}

		if data[0] == '[' {
			var ticks []massiveTick
			if err := json.Unmarshal(data, &ticks); err != nil {
				log.Printf("json array unmarshal error: %v", err)
				continue
			}

			if len(ticks) == 0 {
				continue
			}

			if err := persistTicks(ticks); err != nil {
				log.Printf("persist error: %v", err)
			}
			continue
		}

		var status map[string]any
		if err := json.Unmarshal(data, &status); err == nil {
			log.Printf("status message: %v", status)
			continue
		}

		if messageCount%100 == 1 {
			log.Printf("non-json message: %s", truncateForLog(data, 200))
		}
	}
}

func persistTicks(ticks []massiveTick) error {
	bySymbol := make(map[string][]massiveTick)
	for _, tick := range ticks {
		if tick.Sym == "" {
			continue
		}
		bySymbol[tick.Sym] = append(bySymbol[tick.Sym], tick)
	}

	for symbol, entries := range bySymbol {
		if len(entries) == 0 {
			continue
		}
		if err := writeCSV(symbol, entries); err != nil {
			return err
		}
	}

	return nil
}

func writeCSV(symbol string, ticks []massiveTick) error {
	timestamp := ticks[0].T
	if timestamp <= 0 {
		timestamp = time.Now().UTC().UnixMilli()
	}

	dateDir := time.UnixMilli(timestamp).UTC().Format("2006-01-02")
	symbolDir := filepath.Join(uploadDir, dateDir, symbol)
	if err := os.MkdirAll(symbolDir, 0o755); err != nil {
		return err
	}

	outPath := filepath.Join(symbolDir, fmt.Sprintf("%d.csv", timestamp))
	outFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	writer := csv.NewWriter(outFile)
	if err := writer.Write([]string{"ev", "sym", "i", "x", "p", "s", "c", "t", "q", "z", "ds"}); err != nil {
		return err
	}

	for _, tick := range ticks {
		row := []string{
			tick.Ev,
			tick.Sym,
			tick.I,
			fmt.Sprintf("%d", tick.X),
			fmt.Sprintf("%g", tick.P),
			fmt.Sprintf("%d", tick.S),
			joinInts(tick.C),
			fmt.Sprintf("%d", tick.T),
			fmt.Sprintf("%d", tick.Q),
			fmt.Sprintf("%d", tick.Z),
			tick.DS,
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}

	return nil
}

func init() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetOutput(os.Stdout)
}

func waitForStatus(conn *websocket.Conn, target string) error {
	deadline := time.Now().Add(20 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		log.Printf("status raw: %s", truncateForLog(data, 500))

		var statuses []statusMessage
		if err := json.Unmarshal(data, &statuses); err != nil {
			continue
		}

		for _, status := range statuses {
			if status.Ev == "status" && status.Status == target {
				_ = conn.SetReadDeadline(time.Time{})
				log.Printf("status ok: %s", target)
				return nil
			}
		}
	}
}

func truncateForLog(data []byte, limit int) string {
	text := strings.TrimSpace(string(data))
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprintf("%d", value)
	}
	return strings.Join(parts, "|")
}
