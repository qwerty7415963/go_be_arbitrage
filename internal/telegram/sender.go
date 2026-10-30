package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Sender struct {
	Token   string
	BaseURL string
	Client  *http.Client
}

type SenderError struct {
	Permanent   bool
	StatusCode  int
	Description string
	Err         error
}

func (e *SenderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("telegram send: %s: %v", e.Description, e.Err)
	}
	return fmt.Sprintf("telegram send: %s", e.Description)
}
func (e *SenderError) Unwrap() error { return e.Err }

func IsPermanent(err error) bool {
	var se *SenderError
	if errors.As(err, &se) {
		return se.Permanent
	}
	return false
}
func IsRetryable(err error) bool      { return !IsPermanent(err) }
func IsPermanentError(err error) bool { return IsPermanent(err) }

func NewSender(args ...interface{}) *Sender {
	s := &Sender{BaseURL: "https://api.telegram.org", Client: &http.Client{Timeout: 10 * time.Second}}
	for _, a := range args {
		switch v := a.(type) {
		case string:
			if s.Token == "" {
				s.Token = v
			} else {
				s.BaseURL = v
			}
		case *http.Client:
			s.Client = v
		}
	}
	return s
}

func (s *Sender) SendMessage(args ...interface{}) error {
	ctx := context.Background()
	var chatID int64
	var html string
	for _, a := range args {
		switch v := a.(type) {
		case context.Context:
			ctx = v
		case int64:
			chatID = v
		case int:
			chatID = int64(v)
		case float64:
			chatID = int64(v)
		case string:
			html = v
		}
	}
	if s == nil {
		return fmt.Errorf("sender is nil")
	}
	if s.Client == nil {
		s.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if s.BaseURL == "" {
		s.BaseURL = "https://api.telegram.org"
	}
	payload := map[string]interface{}{
		"chat_id":                  chatID,
		"text":                     html,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	b, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/bot%s/sendMessage", s.BaseURL, s.Token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return &SenderError{Permanent: false, Description: "build request", Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return &SenderError{Permanent: false, Description: "http error", Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.Unmarshal(body, &tr)
	if resp.StatusCode == http.StatusOK && tr.OK {
		return nil
	}
	desc := tr.Description
	if desc == "" {
		desc = string(body)
	}
	permanent := false
	if resp.StatusCode == http.StatusForbidden {
		permanent = true
	}
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(desc), "chat not found") {
		permanent = true
	}
	return &SenderError{Permanent: permanent, StatusCode: resp.StatusCode, Description: desc}
}
