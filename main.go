package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

type RequestBody struct {
	Messages  []map[string]interface{} `json:"messages"`
	Model     string                   `json:"model,omitempty"`
	Stream    bool                     `json:"stream,omitempty"`
	MaxTokens int                      `json:"max_tokens,omitempty"`
	System    string                   `json:"system,omitempty"`
}

func debugLog(format string, v ...interface{}) {
	log.Printf("[DEBUG][%s] %s", time.Now().Format(time.RFC3339), fmt.Sprintf(format, v...))
}

func createProxy(target string) *httputil.ReverseProxy {
	parsedURL, err := url.Parse(target)
	if err != nil {
		log.Fatalf("Failed to parse target URL: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(parsedURL)

	// Customize the director to modify the request before it's sent
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		debugLog("Modified request headers: %+v", req.Header)
	}

	// Add error handling
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		debugLog("Proxy error: %v", err)
		http.Error(w, fmt.Sprintf("Proxy error: %v", err), http.StatusBadGateway)
	}

	// Modify the response before sending it back
	proxy.ModifyResponse = func(resp *http.Response) error {
		debugLog("Response status: %d", resp.StatusCode)
		debugLog("Response headers: %+v", resp.Header)
		return nil
	}

	return proxy
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	debugLog("Incoming request: %s %s", r.Method, r.URL.Path)
	debugLog("Incoming headers: %+v", r.Header)

	// Check environment variables
	requiredEnvVars := []string{"HOST", "X_ID", "X_SIGNATURE", "USER_AGENT", "X_LICENSE"}
	for _, env := range requiredEnvVars {
		if os.Getenv(env) == "" {
			debugLog("Missing required environment variable: %s", env)
			http.Error(w, fmt.Sprintf("Missing required environment variable: %s", env), http.StatusInternalServerError)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		debugLog("Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	debugLog("Incoming request body: %s", string(body))

	var reqBody RequestBody
	if err := json.Unmarshal(body, &reqBody); err != nil {
		debugLog("Invalid JSON in request body: %v", err)
		http.Error(w, "Invalid JSON in request body", http.StatusBadRequest)
		return
	}

	// Store original stream setting
	originalStream := reqBody.Stream
	debugLog("Original stream setting: %v", originalStream)

	switch {
	case strings.HasPrefix(r.URL.Path, "/anthropic/v1/messages"):
		reqBody.Model = "sw-claude-3-5-sonnet"
		reqBody.MaxTokens = 2048
	case strings.HasPrefix(r.URL.Path, "/v1/chat/completions"):
		reqBody.Model = "sw-gpt-4o"
	default:
		debugLog("Unknown endpoint: %s", r.URL.Path)
		http.Error(w, "Unknown endpoint", http.StatusBadRequest)
		return
	}

	modifiedBody, err := json.Marshal(reqBody)
	if err != nil {
		debugLog("Failed to modify request body: %v", err)
		http.Error(w, "Failed to modify request body", http.StatusInternalServerError)
		return
	}

	r.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	r.ContentLength = int64(len(modifiedBody))
	r.Header.Set("Content-Length", fmt.Sprintf("%d", len(modifiedBody)))

	debugLog("Outgoing request body: %s", string(modifiedBody))

	// Set required headers
	headers := map[string]string{
		"Host":            os.Getenv("HOST"),
		"Content-Type":    "application/json",
		"X-ID":            os.Getenv("X_ID"),
		"X-Signature":     os.Getenv("X_SIGNATURE"),
		"Accept":          "*/*",
		"Connection":      "keep-alive",
		"User-Agent":      os.Getenv("USER_AGENT"),
		"X-License":       os.Getenv("X_LICENSE"),
		"Accept-Encoding": "br;q=1.0, gzip;q=0.9, deflate;q=0.8",
		"Accept-Language": "en-US;q=1.0, en-IN;q=0.9",
	}

	for key, value := range headers {
		r.Header.Set(key, value)
		debugLog("Setting header %s: %s", key, value)
	}

	targetURL := "https://" + os.Getenv("HOST")
	targetURL = targetURL + r.URL.Path
	debugLog("Forwarding request to target URL: %s", targetURL)

	proxy := createProxy(targetURL)

	if originalStream {
		debugLog("Handling streaming response")
		// Set proper headers for streaming
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Transfer-Encoding", "chunked")

		// Create a custom response writer that doesn't close the connection
		flusher, ok := w.(http.Flusher)
		if !ok {
			debugLog("Streaming not supported")
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		proxy.ServeHTTP(w, r)
		flusher.Flush()
	} else {
		debugLog("Handling non-streaming response")
		proxy.ServeHTTP(w, r)
	}
}

func main() {
	// Configure logging
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	debugLog("Starting server with environment variables:")
	debugLog("HOST: %s", os.Getenv("HOST"))
	debugLog("HOST: %s", os.Getenv("HOST"))
	debugLog("X_ID: %s", os.Getenv("X_ID"))
	debugLog("X_SIGNATURE: %s", os.Getenv("X_SIGNATURE"))
	debugLog("X_LICENSE: %s", os.Getenv("X_LICENSE"))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Default to 8080 instead of 443 for testing
	}

	http.HandleFunc("/v1/", proxyHandler)
	http.HandleFunc("/anthropic/", proxyHandler)

	debugLog("Starting proxy server on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
