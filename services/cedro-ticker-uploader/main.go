package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultUploadDir = "/data/cedro-ticker-uploader"

type cedroTick struct {
	TimeMSC int64
	Symbol  string
	Raw     string
}

func main() {
	host := strings.TrimSpace(os.Getenv("CEDRO_HOST"))
	if host == "" {
		host = "datafeed2.cedrotech.com"
	}

	port := strings.TrimSpace(os.Getenv("CEDRO_PORT"))
	if port == "" {
		port = "81"
	}

	username := strings.TrimSpace(os.Getenv("CEDRO_USERNAME"))
	if username == "" {
		log.Fatal("CEDRO_USERNAME is required")
	}

	password := strings.TrimSpace(os.Getenv("CEDRO_PASSWORD"))
	if password == "" {
		log.Fatal("CEDRO_PASSWORD is required")
	}

	commandList := strings.TrimSpace(os.Getenv("CEDRO_COMMANDS"))
	if commandList == "" {
		commandList = strings.TrimSpace(os.Getenv("CEDRO_COMMAND"))
	}
	if commandList == "" {
		commandList = "GQT BOVA11 S"
	}

	uploadDir := strings.TrimSpace(os.Getenv("CEDRO_DATA_DIR"))
	if uploadDir == "" {
		uploadDir = defaultUploadDir
	}

	address := net.JoinHostPort(host, port)
	log.Printf("starting cedro-ticker-uploader address=%s commands=%q data_dir=%s", address, commandList, uploadDir)

	backoff := 2 * time.Second
	for {
		if err := run(address, username, password, commandList, uploadDir); err != nil {
			log.Printf("tcp error: %v", err)
		}

		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func run(address, username, password, commandList, uploadDir string) error {
	conn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("connected to %s", address)

	reader := bufio.NewReader(conn)
	writer := &safeWriter{w: bufio.NewWriter(conn)}

	if err := handshake(conn, reader, writer, username, password); err != nil {
		return err
	}

	commands := splitCommands(commandList)
	for _, command := range commands {
		if err := writer.WriteLine(command); err != nil {
			return err
		}
		log.Printf("command sent: %s", command)
	}

	flushInterval := 1 * time.Minute
	acc := newTickAccumulator(flushInterval, func(symbol string, entries []cedroTick) error {
		return writeCSV(uploadDir, symbol, entries)
	})
	defer acc.Stop()

	for {
		line, err := readLine(reader)
		if err != nil {
			return err
		}

		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}

		if text == "SYN" {
			continue
		}

		if isCedroStatus(text) {
			log.Printf("status: %s", text)
			continue
		}

		ts := time.Now().UTC().UnixMilli()
		acc.Add(cedroTick{
			TimeMSC: ts,
			Symbol:  parseSymbol(text),
			Raw:     text,
		})
	}
}

func handshake(conn net.Conn, reader *bufio.Reader, writer *safeWriter, username, password string) error {
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))

	var sendUserOnce sync.Once
	var sendPassOnce sync.Once

	sendUsername := func(reason string) {
		sendUserOnce.Do(func() {
			log.Printf("handshake: sending username (%s)", reason)
			_ = writer.WriteLine(username)
		})
	}
	sendPassword := func(reason string) {
		sendPassOnce.Do(func() {
			log.Printf("handshake: sending password (%s)", reason)
			_ = writer.WriteLine(password)
		})
	}

	// Nudge the server to start prompts if needed.
	_ = writer.WriteLine("")

	go func() {
		time.Sleep(2 * time.Second)
		sendUsername("timeout")
	}()
	go func() {
		time.Sleep(4 * time.Second)
		sendPassword("timeout")
	}()

	for {
		token, err := waitForToken(reader, []string{
			"Connecting...",
			"Welcome to Cedro",
			"Username:",
			"Password:",
			"You are connected",
		})
		if err != nil {
			return err
		}

		log.Printf("handshake: %s", token)

		switch token {
		case "Username:":
			sendUsername("prompt")
		case "Password:":
			sendPassword("prompt")
		case "You are connected":
			_ = conn.SetReadDeadline(time.Time{})
			return nil
		}
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err == nil {
		return line, nil
	}
	if err == io.EOF && line != "" {
		return line, nil
	}
	return line, err
}

type safeWriter struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func (s *safeWriter) WriteLine(value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.WriteString(value + "\r\n"); err != nil {
		return err
	}
	return s.w.Flush()
}

func waitForToken(reader *bufio.Reader, tokens []string) (string, error) {
	var buf strings.Builder
	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF && buf.Len() > 0 {
				text := buf.String()
				for _, token := range tokens {
					if strings.Contains(text, token) {
						return token, nil
					}
				}
			}
			return "", err
		}

		_ = buf.WriteByte(b)
		text := buf.String()
		for _, token := range tokens {
			if strings.Contains(text, token) {
				return token, nil
			}
		}

		if buf.Len() > 4096 {
			buf.Reset()
		}
	}
}

func parseSymbol(text string) string {
	parts := strings.Split(text, ":")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func splitCommands(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	raw := strings.Split(input, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		cmd := strings.TrimSpace(item)
		if cmd == "" {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

func truncateForLog(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func isCedroStatus(text string) bool {
	switch text {
	case "Connecting...",
		"Welcome to Cedro",
		"Username:",
		"Password:",
		"You are connected":
		return true
	default:
		return false
	}
}

type tickAccumulator struct {
	mu      sync.Mutex
	bySymbol map[string][]cedroTick
	ticker  *time.Ticker
	stopCh  chan struct{}
	flushFn func(symbol string, entries []cedroTick) error
}

func newTickAccumulator(interval time.Duration, flushFn func(symbol string, entries []cedroTick) error) *tickAccumulator {
	acc := &tickAccumulator{
		bySymbol: make(map[string][]cedroTick),
		ticker:   time.NewTicker(interval),
		stopCh:   make(chan struct{}),
		flushFn:  flushFn,
	}

	go acc.loop()
	return acc
}

func (a *tickAccumulator) Add(tick cedroTick) {
	if tick.Raw == "" {
		return
	}
	a.mu.Lock()
	symbol := tick.Symbol
	if symbol == "" {
		symbol = "UNKNOWN"
	}
	a.bySymbol[symbol] = append(a.bySymbol[symbol], tick)
	a.mu.Unlock()
}

func (a *tickAccumulator) Stop() {
	close(a.stopCh)
	a.ticker.Stop()
	a.flush()
}

func (a *tickAccumulator) loop() {
	for {
		select {
		case <-a.ticker.C:
			a.flush()
		case <-a.stopCh:
			return
		}
	}
}

func (a *tickAccumulator) flush() {
	a.mu.Lock()
	if len(a.bySymbol) == 0 {
		a.mu.Unlock()
		return
	}
	pending := a.bySymbol
	a.bySymbol = make(map[string][]cedroTick)
	a.mu.Unlock()

	for symbol, entries := range pending {
		if len(entries) == 0 {
			continue
		}
		if err := a.flushFn(symbol, entries); err != nil {
			log.Printf("persist error: %v", err)
		}
	}
}

func writeCSV(uploadDir, symbol string, ticks []cedroTick) error {
	type bucket struct {
		dateDir string
		minute  string
	}

	groups := make(map[bucket][]cedroTick)
	order := make([]bucket, 0, 8)

	for _, tick := range ticks {
		ts := tick.TimeMSC
		if ts <= 0 {
			ts = time.Now().UTC().UnixMilli()
		}
		tm := time.UnixMilli(ts).UTC()
		key := bucket{
			dateDir: tm.Format("2006-01-02"),
			minute:  tm.Format("15_04"),
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], tick)
	}

	for _, key := range order {
		targetDir := filepath.Join(uploadDir, key.dateDir, symbol)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return err
		}

		entries := groups[key]
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].TimeMSC < entries[j].TimeMSC
		})

		outPath := filepath.Join(targetDir, fmt.Sprintf("%s.csv", key.minute))
		outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}

		for _, tick := range entries {
			line := fmt.Sprintf("%d|%s\n", tick.TimeMSC, tick.Raw)
			if _, err := outFile.WriteString(line); err != nil {
				_ = outFile.Close()
				return err
			}
		}

		if err := outFile.Close(); err != nil {
			return err
		}
	}

	return nil
}

func init() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetOutput(os.Stdout)
}
