package crdballowlist

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultIPURL = "https://api.ipify.org"

var ipHTTPClient = &http.Client{Timeout: 8 * time.Second}

// DiscoverEgressIPv4 returns this process's current public SNAT IPv4.
func DiscoverEgressIPv4(ctx context.Context) (string, error) {
	return discoverEgressIPv4(ctx, ipHTTPClient, defaultIPURL)
}

func discoverEgressIPv4(ctx context.Context, hc *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ip discovery: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("ip discovery: empty response")
	}
	if err := validateIPv4(ip); err != nil {
		return "", err
	}
	return ip, nil
}
