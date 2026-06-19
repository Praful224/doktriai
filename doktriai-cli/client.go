package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	BaseURL string
	Role    string
	Actor   string
	HTTP    *http.Client
}

func NewClient(url, role, actor string) *Client {
	return &Client{
		BaseURL: url,
		Role:    role,
		Actor:   actor,
		HTTP:    &http.Client{},
	}
}

func (c *Client) Call(method, path string, payload any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doktri-Role", c.Role)
	req.Header.Set("X-Doktri-Actor", c.Actor)

	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("%s", strings.TrimSpace(string(data)))
	}

	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(data))
	}
	return nil
}
