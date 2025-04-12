package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/txsociety/spice-harvester/pkg/core"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	client *http.Client
	url    string
}

func NewClient(webhookURL string) (*Client, error) {
	_, err := url.ParseRequestURI(webhookURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %s", webhookURL)
	}
	return &Client{
		client: &http.Client{Timeout: 10 * time.Second},
		url:    webhookURL,
	}, nil
}

func (s *Client) Send(ctx context.Context, invoice core.PrivateInvoicePrintable) error {
	jsonData, err := json.Marshal(invoice)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", s.url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	for i := 1; i < 4; i++ {
		err := doRequest(s.client, request)
		if err != nil {
			slog.Info("webhook sending", "error", err.Error())
			time.Sleep(time.Second * time.Duration(i))
			continue
		}
		return nil
	}
	return fmt.Errorf("attempts to send a webhook ended")
}

func doRequest(client *http.Client, request *http.Request) error {
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("webhook sending error: %v", err)
	}
	defer func() {
		err := response.Body.Close()
		if err != nil {
			slog.Error("response body close", "error", err.Error())
		}
	}()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	} else {
		return fmt.Errorf("webhook response status: %v", response.Status)
	}
}
