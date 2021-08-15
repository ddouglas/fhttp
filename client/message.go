package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/fhttp/fhttp"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/sirupsen/logrus"
)

var (
	t table.Writer
)

func init() {
	t = table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Route", "Status"})
	t.AppendSeparator()

}

func (c *client) handleMessage(data []byte) {
	defer c.wg.Done()

	if !json.Valid(data) {
		// Ensure what we are dealing with is valid JSON,
		// if not, then bail out
		return
	}

	var payload = new(fhttp.Payload)
	err := json.Unmarshal(data, payload)
	if err != nil {
		c.logger.WithError(err).Error("failed to decode message")
		return
	}

	switch payload.MessageType {
	case fhttp.MTPing:
		return
	case fhttp.MTHello:
		var message = new(fhttp.HelloMessage)
		err = json.Unmarshal(payload.Message, message)
		if err != nil {
			c.logger.WithError(err).Error("failed to decode consumer message")
			return
		}

		c.handleHelloMessage(message)
	case fhttp.MTConsumerMessage:
		var message = new(fhttp.ConsumerMessage)
		err = json.Unmarshal(payload.Message, message)
		if err != nil {
			c.logger.WithError(err).Error("failed to decode consumer message")
			return
		}

		c.handleConsumerMessage(message)
	}

}

func (c *client) handleHelloMessage(message *fhttp.HelloMessage) {

	out := `
Forward HTTP Session Initialized Successfully
---------------------------------------------
Session Hash: %s
Request URI: %s
Consumer URI: %s

* Execute a Request to generate log below
---------------------------------------------`

	out = fmt.Sprintf(out, message.Hash, message.RequestURI, c.consumer)

	fmt.Println(out)

}

func (c *client) handleConsumerMessage(message *fhttp.ConsumerMessage) {

	route, err := url.ParseRequestURI(message.Route)
	if err != nil {
		c.logger.WithError(err).Error("message is unparsable")
		return
	}

	var scopedConsumerURI = new(url.URL)
	*scopedConsumerURI = *c.consumer
	scopedConsumerURI.Path = route.RequestURI()

	request, err := http.NewRequest(message.Method, scopedConsumerURI.String(), bytes.NewBuffer(message.Body))
	if err != nil {
		c.logger.WithError(err).Error("failed to build request to consumer")
		return
	}

	for key, headerArray := range message.Headers {
		for _, header := range headerArray {
			request.Header.Add(key, header)
		}
	}

	resp, err := httpClient.Do(request)
	if err != nil {
		c.logger.WithError(err).Error("failed execute request to consumer")
		return
	}

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.WithError(err).Error("failed to read request body")
		return
	}

	fmt.Printf("%s %s\t\t%s\t\t%s\n", message.Method, scopedConsumerURI.RequestURI(), resp.Status, string(data))

	c.logger.WithFields(logrus.Fields{
		"route":  scopedConsumerURI.RequestURI(),
		"status": resp.StatusCode,
	}).Debugln()
}
