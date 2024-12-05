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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

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

	log.Printf("Outgoing request body: %s", string(modifiedBody))
	log.Printf("Outgoing headers: %v", r.Header)

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

	log.Printf("Forwarding request to target URL: %s", targetURL)

	proxy := createProxy(targetURL)
	proxy.ServeHTTP(w, r)
}

func main() {
	http.HandleFunc("/v1/", proxyHandler)
	http.HandleFunc("/anthropic/", proxyHandler)

	log.Println("Starting proxy server on port 443...")
	if err := http.ListenAndServe(":443", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
