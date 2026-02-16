package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Alert represents the distilled data we care about from a Splunk webhook.
type Alert struct {
	ID         int       `json:"id"`
	ReceivedAt time.Time `json:"received_at"`
	Title      string    `json:"title,omitempty"`
	Host       string    `json:"host"`
	Source     string    `json:"source"`
	SrcIP      string    `json:"src_ip"`
	SearchName string    `json:"search_name"`
	AlertType  string    `json:"alert_type"`
	Severity   string    `json:"severity,omitempty"`

	// Collector-style fields (if the webhook sender is our audit collector).
	Exe   string `json:"exe,omitempty"`
	Comm  string `json:"comm,omitempty"`
	UID   string `json:"uid,omitempty"`
	EUID  string `json:"euid,omitempty"`
	AUID  string `json:"auid,omitempty"`
	PID   string `json:"pid,omitempty"`
	PPID  string `json:"ppid,omitempty"`
	TTY   string `json:"tty,omitempty"`
	Key   string `json:"key,omitempty"`
	Audit string `json:"audit,omitempty"`
	Text  string `json:"text,omitempty"`
	RawEv string `json:"raw_ev,omitempty"`

	Raw     json.RawMessage `json:"raw"`
	RawText string          `json:"raw_text,omitempty"`
}

// CollectorAlert is the JSON payload sent by our audit collector.
// Keep all fields as strings because upstream may serialize numbers as strings.
type CollectorAlert struct {
	Alert string `json:"alert"`
	Host  string `json:"host"`
	Exe   string `json:"exe"`
	Comm  string `json:"comm"`
	UID   string `json:"uid"`
	EUID  string `json:"euid"`
	AUID  string `json:"auid"`
	PID   string `json:"pid"`
	PPID  string `json:"ppid"`
	TTY   string `json:"tty"`
	Key   string `json:"key"`
	Audit string `json:"audit"`
	Text  string `json:"text"`
	Raw   string `json:"raw"`
}

var (
	alerts   []Alert
	alertsMu sync.Mutex
	nextID   = 1
	maxStore = 500 // keep a rolling window to avoid unbounded memory growth
	dataFile = filepath.Join(".", "alerts_history.json")
)

//go:embed web
var embedded embed.FS

func main() {
	addr := resolveAddr()

	if err := loadHistory(); err != nil {
		log.Printf("warning: could not load history: %v", err)
	}

	webFS, err := fs.Sub(embedded, "web")
	if err != nil {
		log.Fatalf("cannot load embedded assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", spaHandler(webFS))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(webFS))))
	mux.HandleFunc("/api/alerts", getAlerts)
	mux.HandleFunc("/alerts", getAlertsText)
	mux.HandleFunc("/webhook", webhookHandler)
	mux.HandleFunc("/api/history/reload", reloadHistory)
	mux.HandleFunc("/api/history/rotate", rotateHistory)

	log.Printf("Splunk webhook receiver listening on %s", addr)
	log.Printf("POST Splunk alerts to http://<ip>%s/webhook", addr)

	if err := http.ListenAndServe(addr, logRequests(mux)); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

func resolveAddr() string {
	val := strings.TrimSpace(os.Getenv("PORT"))
	if val == "" {
		val = "5123"
	}
	if strings.Contains(val, ":") {
		return val
	}
	return ":" + val
}

func spaHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "missing index", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

func rotateHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	alertsMu.Lock()
	defer alertsMu.Unlock()

	ts := time.Now().Format("20060102-150405")
	newFile := filepath.Join(".", "alerts_history_"+ts+".json")

	// Persist current alerts to timestamped file
	snap := snapshot{Alerts: alerts, NextID: nextID}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		http.Error(w, "failed to rotate: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(newFile, data, 0644); err != nil {
		http.Error(w, "failed to rotate: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Reset in-memory and start new file
	alerts = nil
	nextID = 1
	dataFile = newFile
	_ = saveHistoryLocked()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "rotated",
		"filename": filepath.Base(newFile),
	})
}

func getAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	alertsMu.Lock()
	defer alertsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]interface{}{"alerts": alerts}); err != nil {
		http.Error(w, "cannot encode", http.StatusInternalServerError)
	}
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var alert Alert

	rawBody, readErr := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if readErr != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var parseErr error
	if a, ok, err := tryDecodeCollector(rawBody); err == nil && ok {
		sev := severityFromCollector(a)
		alert = Alert{
			ReceivedAt: time.Now().UTC(),
			Title:      collectorTitle(a),
			Host:       a.Host,
			AlertType:  a.Alert,
			Severity:   sev,
			Exe:        a.Exe,
			Comm:       a.Comm,
			UID:        a.UID,
			EUID:       a.EUID,
			AUID:       a.AUID,
			PID:        a.PID,
			PPID:       a.PPID,
			TTY:        a.TTY,
			Key:        a.Key,
			Audit:      a.Audit,
			Text:       a.Text,
			RawEv:      a.Raw,
			Source:     a.Exe,
			Raw:        json.RawMessage(rawBody),
		}

		// Human-readable server logs.
		log.Printf("[SEV=%s][ALERT=%s] host=%s exe=%s auid=%s tty=%s audit=%s pid=%s",
			sev, a.Alert, a.Host, a.Exe, a.AUID, a.TTY, a.Audit, a.PID)
		if strings.TrimSpace(a.Text) != "" {
			log.Printf("  %s", a.Text)
		}
	} else {
		// Fallback to generic Splunk-style payloads (JSON or payload=<json>).
		payload, rawJSON, err := decodePayloadBytes(rawBody)
		if err != nil {
			parseErr = err
			alert = Alert{
				ReceivedAt: time.Now().UTC(),
				AlertType:  "unparsed",
				RawText:    string(rawBody),
			}
		} else {
			alert = extractAlert(payload, rawJSON)
		}
	}

	if alert.SrcIP == "" {
		if host, _, splitErr := net.SplitHostPort(r.RemoteAddr); splitErr == nil {
			alert.SrcIP = host
		}
	}

	alertsMu.Lock()
	defer alertsMu.Unlock()

	if len(alerts) >= maxStore {
		// drop the oldest to keep memory predictable
		alerts = alerts[1:]
	}
	alert.ID = nextID
	nextID++
	alerts = append(alerts, alert)
	_ = saveHistoryLocked()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]interface{}{
		"ok":     true,
		"status": "stored",
		"id":     alert.ID,
	}
	if parseErr != nil {
		resp["parse_error"] = parseErr.Error()
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func reloadHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := loadHistory(); err != nil {
		http.Error(w, "failed to reload: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "reloaded",
		"count":  len(alerts),
	})
}

func decodePayload(r *http.Request) (rawBody []byte, payload map[string]interface{}, rawJSON []byte, err error) {
	rawBody, err = io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, nil, errors.New("failed to read body")
	}
	defer r.Body.Close()

	// 1) Try plain JSON body
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &payload); err == nil {
			return rawBody, payload, rawBody, nil
		}
	}

	// 2) Try payload=<json> form style used by some Splunk configs
	values, parseErr := url.ParseQuery(string(rawBody))
	if parseErr == nil {
		if p := values.Get("payload"); p != "" {
			if err := json.Unmarshal([]byte(p), &payload); err == nil {
				return rawBody, payload, []byte(p), nil
			}
		}
	}

	return rawBody, nil, nil, errors.New("invalid payload: expected JSON body or payload=<json>")
}

func decodePayloadBytes(rawBody []byte) (payload map[string]interface{}, rawJSON []byte, err error) {
	// 1) Try plain JSON body.
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &payload); err == nil {
			return payload, rawBody, nil
		}
	}

	// 2) Try payload=<json> form style used by some Splunk configs.
	values, parseErr := url.ParseQuery(string(rawBody))
	if parseErr == nil {
		if p := values.Get("payload"); p != "" {
			if err := json.Unmarshal([]byte(p), &payload); err == nil {
				return payload, []byte(p), nil
			}
		}
	}

	return nil, nil, errors.New("invalid payload: expected JSON body or payload=<json>")
}

func tryDecodeCollector(rawBody []byte) (CollectorAlert, bool, error) {
	var a CollectorAlert
	if err := json.Unmarshal(rawBody, &a); err != nil {
		return CollectorAlert{}, false, err
	}
	// Avoid misclassifying random JSON as collector payload: require alert field.
	if strings.TrimSpace(a.Alert) == "" {
		return CollectorAlert{}, false, nil
	}
	return a, true, nil
}

func severityFromCollector(a CollectorAlert) string {
	exe := strings.TrimSpace(a.Exe)
	euid := strings.TrimSpace(a.EUID)

	if euid == "0" {
		if strings.HasPrefix(exe, "/tmp/") || strings.HasPrefix(exe, "/dev/shm/") || strings.HasPrefix(exe, "/var/tmp/") {
			return "HIGH"
		}
	}

	if exe != "" {
		allowed := []string{"/usr/bin/", "/bin/", "/usr/sbin/", "/sbin/"}
		for _, p := range allowed {
			if strings.HasPrefix(exe, p) {
				return "LOW"
			}
		}
		return "MED"
	}

	return "LOW"
}

func collectorTitle(a CollectorAlert) string {
	actor := strings.TrimSpace(a.AUID)
	// Prefer human-readable AUID from raw if present (e.g., AUID="nala").
	if v := extractQuotedKV(a.Raw, "AUID"); v != "" {
		actor = v
	}
	if actor == "" {
		actor = "unknown user"
	}

	acting := ""
	if strings.TrimSpace(a.EUID) == "0" {
		acting = ", acting as root"
	}

	verb := "executed"
	if strings.Contains(a.Raw, "success=yes") {
		verb = "successfully executed"
	}

	exe := strings.TrimSpace(a.Exe)
	if exe == "" {
		exe = "(unknown exe)"
	}

	return actor + acting + ", " + verb + " " + exe
}

func extractQuotedKV(raw, key string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || key == "" {
		return ""
	}
	pat := key + "=\""
	i := strings.Index(raw, pat)
	if i < 0 {
		return ""
	}
	start := i + len(pat)
	j := strings.IndexByte(raw[start:], '"')
	if j < 0 {
		return ""
	}
	return raw[start : start+j]
}

func getAlertsText(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	alertsMu.Lock()
	defer alertsMu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for i := len(alerts) - 1; i >= 0; i-- {
		a := alerts[i]
		msg := strings.TrimSpace(a.Text)
		if msg == "" {
			msg = strings.TrimSpace(a.RawText)
		}
		if msg == "" && len(a.Raw) > 0 {
			msg = "(raw json available)"
		}

		// Example:
		// 2026-02-15T01:02:03Z [SEV=HIGH][ALERT=RED_EXEC] host=debian exe=/tmp/x auid=1000 tty=pts0 audit=... pid=1234 test...
		_, _ = io.WriteString(w, a.ReceivedAt.UTC().Format(time.RFC3339))
		_, _ = io.WriteString(w, " [SEV="+orDash(a.Severity)+"]")
		_, _ = io.WriteString(w, "[ALERT="+orDash(a.AlertType)+"]")
		_, _ = io.WriteString(w, " host="+orDash(a.Host))
		if a.Exe != "" {
			_, _ = io.WriteString(w, " exe="+a.Exe)
		}
		if a.AUID != "" {
			_, _ = io.WriteString(w, " auid="+a.AUID)
		}
		if a.EUID != "" {
			_, _ = io.WriteString(w, " euid="+a.EUID)
		}
		if a.TTY != "" {
			_, _ = io.WriteString(w, " tty="+a.TTY)
		}
		if a.Audit != "" {
			_, _ = io.WriteString(w, " audit="+a.Audit)
		}
		if a.PID != "" {
			_, _ = io.WriteString(w, " pid="+a.PID)
		}
		if msg != "" {
			_, _ = io.WriteString(w, " ")
			_, _ = io.WriteString(w, msg)
		}
		_, _ = io.WriteString(w, "\n")
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func extractAlert(payload map[string]interface{}, raw []byte) Alert {
	result := extractResult(payload)

	host := pickString(result, "host", "hostname", "computer")
	source := pickString(result, "source")
	srcIP := pickString(result, "src", "src_ip", "source_ip", "srcip", "clientip", "ip")
	if srcIP == "" {
		srcIP = pickString(payload, "src", "src_ip", "source_ip", "srcip", "clientip", "ip")
	}

	alertType := pickString(result, "alert_type", "type", "signature")
	if alertType == "" {
		alertType = pickString(payload, "search_name", "search", "savedsearch_name")
	}
	if alertType == "" {
		alertType = pickString(result, "sourcetype")
	}

	searchName := pickString(payload, "search_name", "search", "savedsearch_name")

	return Alert{
		Host:       host,
		Source:     source,
		SrcIP:      srcIP,
		SearchName: searchName,
		AlertType:  alertType,
		ReceivedAt: time.Now().UTC(),
		Raw:        json.RawMessage(raw),
	}
}

type snapshot struct {
	Alerts []Alert `json:"alerts"`
	NextID int     `json:"next_id"`
}

func loadHistory() error {
	alertsMu.Lock()
	defer alertsMu.Unlock()

	data, err := os.ReadFile(dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	alerts = snap.Alerts
	if snap.NextID > 0 {
		nextID = snap.NextID
	} else {
		nextID = len(alerts) + 1
	}
	return nil
}

func saveHistoryLocked() error {
	snap := snapshot{
		Alerts: alerts,
		NextID: nextID,
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dataFile, data, 0644)
}

func extractResult(payload map[string]interface{}) map[string]interface{} {
	if v, ok := payload["result"].(map[string]interface{}); ok {
		return v
	}
	if v, ok := payload["result"].(map[string]json.RawMessage); ok {
		out := make(map[string]interface{}, len(v))
		for k, raw := range v {
			var val interface{}
			if err := json.Unmarshal(raw, &val); err == nil {
				out[k] = val
			}
		}
		return out
	}
	if arr, ok := payload["results"].([]interface{}); ok && len(arr) > 0 {
		if first, ok := arr[0].(map[string]interface{}); ok {
			return first
		}
	}
	return map[string]interface{}{}
}

func pickString(src map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := src[k]; ok {
			if s := stringify(v); s != "" {
				return s
			}
		}
		// allow lower-case only lookup if incoming has different casing
		for sk, sv := range src {
			if strings.EqualFold(sk, k) {
				if s := stringify(sv); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func stringify(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return ""
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%v)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
