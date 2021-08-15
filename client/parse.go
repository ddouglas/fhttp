package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fhttp/fhttp"
	"github.com/pkg/errors"
)

var httpClient = &http.Client{
	Timeout: time.Second * 5,
}

func parseOrFetchBrokerURI(uri string) (*url.URL, error) {

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parsed broker uri")
	}

	if ok := validateURIScheme(parsed.Scheme); !ok {
		return nil, errors.Errorf("uri scheme is in valid, must be one of %s", strings.Join(validURISchemes, ","))
	}

	if parsed.Fragment != "" {
		parsed.Fragment = ""
	}

	var hash string
	if strings.HasPrefix(parsed.Path, "/open/") {
		parts := strings.Split(parsed.Path, "/")
		if len(parts) != 3 {
			return nil, errors.Errorf("invalid format of open path, expected open/{hash}, got %s", parsed.Path)
		}

		hash = parts[2]
	} else if parsed.Path == "new" {
		hash = "new"
	}

	if len(hash) != 10 || hash == "new" {
		parsed, err = fetchNewHash(parsed)
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch hash for connection")
		}
	}

	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	}

	return parsed, nil

}

func fetchNewHash(uri *url.URL) (*url.URL, error) {

	if uri.Path != "new" {
		uri.Path = "new"
	}

	req, err := http.NewRequest(http.MethodPost, uri.String(), nil)
	if err != nil {
		return uri, errors.Wrap(err, "failed to build request for new connection id")
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return uri, errors.Wrap(err, "failed to execute request for new connection id")
	}

	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return uri, errors.Wrap(err, "unable to read response body")
	}

	if res.StatusCode != http.StatusOK {
		return uri, errors.Errorf("failed to fetch connection id, expected status code %d, got %d: %s", http.StatusOK, res.StatusCode, string(data))
	}

	var message = new(fhttp.Payload)
	err = json.Unmarshal(data, &message)
	if err != nil {
		return uri, errors.Wrap(err, "unable decode response body")
	}

	if message.MessageType != fhttp.MTHello {
		return uri, errors.Wrapf(err, "invalid message type received from server: %s", message.MessageType)
	}

	var helloMessage = new(fhttp.HelloMessage)
	err = json.Unmarshal(message.Message, helloMessage)
	if err != nil {
		return uri, errors.Wrap(err, "unable decode hello message")
	}

	uri.Path = helloMessage.OpenURI.RequestURI()

	return uri, nil

}

func parseConsumerURI(host string, port int) (*url.URL, error) {

	parsed, err := url.Parse(host)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse consumer uri")
	}

	if parsed.Port() == "" && port > 0 {
		parsed.Host = fmt.Sprintf("%s:%d", parsed.Host, port)
	}

	return parsed, nil

}

func validateURIScheme(scheme string) bool {
	for _, valid := range validURISchemes {
		if scheme == valid {
			return true
		}
	}

	return false
}
