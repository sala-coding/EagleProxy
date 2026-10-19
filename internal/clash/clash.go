// Package clash talks to the sing-box Clash API (experimental.clash_api) to
// retrieve traffic totals and the live connection list.
package clash

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Metadata describes a connection's endpoints.
type Metadata struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	DestinationIP   string `json:"destinationIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	ProcessPath     string `json:"processPath"`
}

// Connection is a single active connection.
type Connection struct {
	ID          string   `json:"id"`
	Upload      int64    `json:"upload"`
	Download    int64    `json:"download"`
	Start       string   `json:"start"`
	Chains      []string `json:"chains"`
	Rule        string   `json:"rule"`
	RulePayload string   `json:"rulePayload"`
	Metadata    Metadata `json:"metadata"`
}

// ConnectionsResp is the /connections payload.
type ConnectionsResp struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
}

// Client is a minimal Clash API client.
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// New creates a client for 127.0.0.1:port.
func New(port int, secret string) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		secret:  secret,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clash api %s: HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Connections fetches current totals and the connection list.
func (c *Client) Connections() (*ConnectionsResp, error) {
	data, err := c.get("/connections")
	if err != nil {
		return nil, err
	}
	var out ConnectionsResp
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProxiesResp is the /proxies payload: the outbound table, by tag.
type ProxiesResp struct {
	Proxies map[string]Proxy `json:"proxies"`
}

// Proxy is one entry of that table. Only the fields this app reads are declared — the rest
// of the payload is a dashboard's business.
type Proxy struct {
	Type string   `json:"type"`
	Now  string   `json:"now"`
	All  []string `json:"all"`
}

// Proxies fetches the outbound table.
func (c *Client) Proxies() (*ProxiesResp, error) {
	data, err := c.get("/proxies")
	if err != nil {
		return nil, err
	}
	var out ProxiesResp
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Select points a selector at one of its members.
//
// The body is the shape the dashboard sends. What the call buys is the reason it exists:
// the core changes which outbound the traffic takes without reading its config again, so
// nothing — not the core, and not the tun adapter it is holding — has to be restarted.
func (c *Client) Select(selector, member string) error {
	body, err := json.Marshal(map[string]string{"name": member})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut,
		c.baseURL+"/proxies/"+url.PathEscape(selector), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("clash api select %s: HTTP %d", selector, resp.StatusCode)
	}
	return nil
}

// CloseConnection closes a connection by id.
func (c *Client) CloseConnection(id string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/connections/"+id, nil)
	if err != nil {
		return err
	}
	if c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("clash api close: HTTP %d", resp.StatusCode)
	}
	return nil
}
