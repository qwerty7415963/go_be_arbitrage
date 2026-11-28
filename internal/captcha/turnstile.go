package captcha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type TurnstileVerifier struct {
	secret            string
	expectedHostnames []string
	timeout           time.Duration
	enabled           bool
	mode              string
	client            *http.Client
}

func NewTurnstileVerifier(secret string, expectedHostnames []string, timeout time.Duration, enabled bool, mode string) *TurnstileVerifier {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &TurnstileVerifier{
		secret:            secret,
		expectedHostnames: expectedHostnames,
		timeout:           timeout,
		enabled:           enabled,
		mode:              mode,
		client:            &http.Client{Timeout: timeout},
	}
}

type turnstileResponse struct {
	Success    bool     `json:"success"`
	Action     string   `json:"action"`
	Hostname   string   `json:"hostname"`
	ErrorCodes []string `json:"error-codes"`
}

func (v *TurnstileVerifier) Verify(ctx context.Context, token string, args ...string) error {
	if !v.enabled {
		if v.mode == "release" {
			return fmt.Errorf("captcha disabled in release")
		}
		return nil
	}
	if token == "" {
		return fmt.Errorf("missing captcha token")
	}
	if v.secret == "" {
		if v.mode == "release" {
			return fmt.Errorf("missing turnstile secret")
		}
		return nil
	}

	reqBody, _ := json.Marshal(map[string]string{
		"secret":   v.secret,
		"response": token,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return fmt.Errorf("turnstile request failed: %w", err)
	}
	defer resp.Body.Close()

	var tr turnstileResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return fmt.Errorf("invalid turnstile response: %w", err)
	}
	if !tr.Success {
		return fmt.Errorf("turnstile verification failed")
	}
	if tr.Action != "" && tr.Action != "register" {
		return fmt.Errorf("turnstile action mismatch")
	}
	if len(v.expectedHostnames) > 0 {
		ok := false
		for _, h := range v.expectedHostnames {
			if h == tr.Hostname {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("turnstile hostname mismatch")
		}
	}
	return nil
}
