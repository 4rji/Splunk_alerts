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
	"strconv"
	"strings"
	"sync"
	"time"
)

// Alert represents the distilled data we care about from a Splunk webhook.
type Alert struct {
	ID         int             `json:"id"`
	ReceivedAt time.Time       `json:"received_at"`
	Host       string          `json:"host"`
	Source     string          `json:"source"`
	SrcIP      string          `json:"src_ip"`
	SearchName string          `json:"search_name"`
	AlertType  string          `json:"alert_type"`
	Raw        json.RawMessage `json:"raw"`
	RawText    string          `json:"raw_text,omitempty"`
}

var (
	alerts   []Alert
	alertsMu sync.Mutex
	nextID   = 1
	maxStore = 500 // keep a rolling window to avoid unbounded memory growth
)

//go:embed web
var embedded embed.FS

func main() {
	addr := resolveAddr()

	webFS, err := fs.Sub(embedded, "web")
	if err != nil {
		log.Fatalf("cannot load embedded assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", spaHandler(webFS))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(webFS))))
	mux.HandleFunc("/api/alerts", getAlerts)
	mux.HandleFunc("/webhook", webhookHandler)

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

	rawBody, payload, rawJSON, err := decodePayload(r)
	var alert Alert
	if err != nil {
		// Store even if unparseable so we can inspect payloads.
		alert = Alert{
			ReceivedAt: time.Now().UTC(),
			AlertType:  "unparsed",
			RawText:    string(rawBody),
		}
	} else {
		alert = extractAlert(payload, rawJSON)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	resp := map[string]interface{}{
		"status": "stored",
		"id":     alert.ID,
	}
	if err != nil {
		resp["parse_error"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(resp)
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
