package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var baseURL = "https://eu.api.blizzard.com"

type transport struct {
	underlyingTransport http.RoundTripper
	accessToken         accessToken
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", t.accessToken.AccessToken))
	req.Header.Add("Battlenet-Namespace", "dynamic-eu")
	return t.underlyingTransport.RoundTrip(req)
}

type accessToken struct {
	AccessToken string
	ExpiresAt   time.Time
}

type wowAH struct {
	clientID     string
	clientSecret string
	accessToken  accessToken
	httpClient   *http.Client
}

func newWowAH(clientID, clientSecret string) (*wowAH, error) {
	if clientID == "" {
		return nil, errors.New("clientID is required")
	}

	if clientSecret == "" {
		return nil, errors.New("clientSecret is required")
	}

	w := &wowAH{
		clientID:     clientID,
		clientSecret: clientSecret,
	}
	if err := w.auth(); err != nil {
		return nil, fmt.Errorf("failed to authenticate wowah: %w", err)
	}

	return w, nil
}

func (w *wowAH) ensureAuth() error {
	if w.accessToken.ExpiresAt.Before(time.Now()) {
		return w.auth()
	}

	return nil
}

func (w *wowAH) auth() error {
	req, err := http.NewRequest(http.MethodPost, "https://eu.battle.net/oauth/token", bytes.NewBuffer([]byte("grant_type=client_credentials")))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.SetBasicAuth(w.clientID, w.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	t := struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return fmt.Errorf("failed to decode access token: %w", err)
	}

	w.accessToken = accessToken{
		AccessToken: t.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(t.ExpiresIn) * time.Second),
	}

	w.httpClient = &http.Client{
		Transport: &transport{
			underlyingTransport: http.DefaultTransport,
			accessToken:         w.accessToken,
		},
	}

	return nil
}

type WowToken struct {
	LastUpdatedTimestamp int `json:"last_updated_timestamp"`
	Price                int `json:"price"`
}

// GetToken returns the current wow token in Gold
func (w *wowAH) GetToken() (WowToken, error) {
	if err := w.ensureAuth(); err != nil {
		return WowToken{}, err
	}

	resp, err := w.httpClient.Get(fmt.Sprintf("%s/data/wow/token/", baseURL))
	if err != nil {
		return WowToken{}, fmt.Errorf("failed to get realms: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return WowToken{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	token := WowToken{}
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return WowToken{}, fmt.Errorf("failed to decode wow token: %w", err)
	}

	return token, nil
}
