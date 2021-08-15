package fhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

type ConsumerMessage struct {
	Headers map[string][]string `json:"headers"`
	Method  string              `json:"method"`
	Route   string              `json:"path,omitempty"`
	Body    json.RawMessage     `json:"body"`
}

func (c *ConsumerMessage) ReadFromRequest(r *http.Request) error {
	buf := &bytes.Buffer{}
	buf.Grow(int(r.ContentLength) + bytes.MinRead)

	_, err := io.Copy(buf, r.Body)
	if err != nil {
		return err
	}

	if buf.Len() > 0 {
		c.Body = buf.Bytes()
	}

	return nil
}

type HelloMessage struct {
	Hash       string   `json:"hash"`
	OpenURI    *url.URL `json:"open"`
	RequestURI *url.URL `json:"request"`
}

type Payload struct {
	MessageType messageType     `json:"type"`
	Message     json.RawMessage `json:"message,omitempty"`
}

type messageType int

const (
	MTHello messageType = iota
	MTPing
	MTConsumerMessage
)
