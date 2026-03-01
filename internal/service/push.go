package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const expoPushURL = "https://exp.host/--/api/v2/push/send"

// PushMessage is an Expo push notification message.
type PushMessage struct {
	To         string            `json:"to"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	Data       map[string]string `json:"data,omitempty"`
	Sound      string            `json:"sound,omitempty"`
	Priority   string            `json:"priority,omitempty"`
	CategoryID string            `json:"categoryId,omitempty"`
}

// PushClient sends notifications via the Expo Push Service.
type PushClient struct {
	client *http.Client
}

// NewPushClient creates a new Expo push client.
func NewPushClient() *PushClient {
	return &PushClient{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send sends one or more push messages to the Expo Push Service.
func (p *PushClient) Send(messages []PushMessage) error {
	if len(messages) == 0 {
		return nil
	}

	body, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("push: marshal: %w", err)
	}

	req, err := http.NewRequest("POST", expoPushURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("push: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("push: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("push: server returned %d", resp.StatusCode)
	}
	return nil
}
