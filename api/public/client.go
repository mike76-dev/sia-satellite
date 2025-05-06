package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
)

// ErrResponseError indicates that the server has returned an error.
var ErrResponseError = errors.New("error response")

// defaultTimeout is the default timeout for the API requests.
const defaultTimeout = 5 * time.Minute

// A Client makes HTTP requests to the satellite's public API.
type Client struct {
	BaseURL  string
	APIToken string

	c *http.Client
}

// NewClient creates a new Client.
func NewClient(addr, token string) *Client {
	return &Client{
		BaseURL:  addr,
		APIToken: token,
		c:        &http.Client{Timeout: defaultTimeout},
	}
}

// Get makes a GET request.
func (c *Client) Get(path string, resp any) error {
	if strings.Contains(path, "?") {
		path += "&token=" + c.APIToken
	} else {
		path += "?token=" + c.APIToken
	}

	res, err := c.c.Get(c.BaseURL + path)
	if err != nil {
		return utils.AddContext(err, "GET request failed")
	}

	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
		return utils.AddContext(err, "couldn't decode response")
	}

	return nil
}

// Post makes a POST request.
func (c *Client) Post(path string, req, resp any) error {
	if strings.Contains(path, "?") {
		path += "&token=" + c.APIToken
	} else {
		path += "?token=" + c.APIToken
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(req); err != nil {
		return utils.AddContext(err, "couldn't encode request")
	}

	res, err := c.c.Post(c.BaseURL+path, "application/json", &buf)
	if err != nil {
		return utils.AddContext(err, "POST request failed")
	}

	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
		return utils.AddContext(err, "couldn't decode response")
	}

	return nil
}

// Put makes a PUT request.
func (c *Client) Put(path string, req, resp any) error {
	if strings.Contains(path, "?") {
		path += "&token=" + c.APIToken
	} else {
		path += "?token=" + c.APIToken
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(req); err != nil {
		return utils.AddContext(err, "couldn't encode request")
	}

	r, err := http.NewRequest("PUT", c.BaseURL+path, &buf)
	if err != nil {
		return utils.AddContext(err, "couldn't construct PUT request")
	}

	res, err := c.c.Do(r)
	if err != nil {
		return utils.AddContext(err, "PUT request failed")
	}

	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
		return utils.AddContext(err, "couldn't decode response")
	}

	return nil
}

// Delete makes a DELETE request.
func (c *Client) Delete(path string, resp any) error {
	if strings.Contains(path, "?") {
		path += "&token=" + c.APIToken
	} else {
		path += "?token=" + c.APIToken
	}

	r, err := http.NewRequest("DELETE", c.BaseURL+path, nil)
	if err != nil {
		return utils.AddContext(err, "couldn't construct DELETE request")
	}

	res, err := c.c.Do(r)
	if err != nil {
		return utils.AddContext(err, "DELETE request failed")
	}

	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(resp); err != nil {
		return utils.AddContext(err, "couldn't decode response")
	}

	return nil
}
