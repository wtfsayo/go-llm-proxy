package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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

func createProxy(target string) *httputil.ReverseProxy {
	parsedURL, err := url.Parse(target)
	if err != nil {
		log.Fatalf("Failed to parse target URL: %v", err)
	}
	return httputil.NewSingleHostReverseProxy(parsedURL)
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("[%s] Incoming request: %s %s", time.Now().Format(time.RFC3339), r.Method, r.URL.Path)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	log.Printf("[%s] Incoming request body: %s", time.Now().Format(time.RFC3339), string(body))

	var reqBody RequestBody
	if err := json.Unmarshal(body, &reqBody); err != nil {
		http.Error(w, "Invalid JSON in request body", http.StatusBadRequest)
		return
	}

	switch {
	case strings.HasPrefix(r.URL.Path, "/anthropic/v1/messages"):
		reqBody.Model = "sw-claude-3-5-sonnet"
		reqBody.MaxTokens = 2048
	case strings.HasPrefix(r.URL.Path, "/v1/chat/completions"):
		reqBody.Model = "sw-gpt-4o"
	default:
		http.Error(w, "Unknown endpoint", http.StatusBadRequest)
		return
	}

	if reqBody.Stream == false {
		reqBody.Stream = true
	}

	modifiedBody, err := json.Marshal(reqBody)
	if err != nil {
		http.Error(w, "Failed to modify request body", http.StatusInternalServerError)
		return
	}

	r.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	r.ContentLength = int64(len(modifiedBody))
	r.Header.Set("Content-Length", fmt.Sprintf("%d", len(modifiedBody)))

	log.Printf("[%s] Outgoing request body: %s", time.Now().Format(time.RFC3339), string(modifiedBody))

	r.Header.Set("Host", os.Getenv("HOST"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-ID", os.Getenv("X_ID"))
	r.Header.Set("X-Signature", os.Getenv("X_SIGNATURE"))
	r.Header.Set("Accept", "*/*")
	r.Header.Set("Connection", "keep-alive")
	r.Header.Set("User-Agent", os.Getenv("USER_AGENT"))
	r.Header.Set("X-License", os.Getenv("X_LICENSE"))
	r.Header.Set("Accept-Encoding", "br;q=1.0, gzip;q=0.9, deflate;q=0.8")
	r.Header.Set("Accept-Language", "en-US;q=1.0, en-IN;q=0.9")

	targetURL := "https://" + os.Getenv("HOST")
	targetURL = targetURL + r.URL.Path
	log.Printf("[%s] Forwarding request to target URL: %s", time.Now().Format(time.RFC3339), targetURL)

	proxy := createProxy(targetURL)

	respRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(respRecorder, r)

	resp := respRecorder.Result()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[%s] Failed to read response body: %v", time.Now().Format(time.RFC3339), err)
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}
	log.Printf("[%s] Outgoing response body: %s", time.Now().Format(time.RFC3339), string(respBody))

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if _, err := w.Write(respBody); err != nil {
		log.Printf("[%s] Failed to write response to client: %v", time.Now().Format(time.RFC3339), err)
	}
}

func main() {
	http.HandleFunc("/v1/", proxyHandler)
	http.HandleFunc("/anthropic/", proxyHandler)

	log.Println("Starting proxy server on port 443...")
	if err := http.ListenAndServe(":443", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
