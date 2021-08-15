package main

import (
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

type client struct {
	tunnel   *websocket.Conn
	logger   *logrus.Logger
	wg       *sync.WaitGroup
	ticker   *time.Ticker
	broker   *url.URL
	consumer *url.URL
	done     chan struct{}
	debug    bool
}

func newClient(broker, consumer *url.URL, debug bool) (*client, error) {
	tunnel, _, err := websocket.DefaultDialer.Dial(broker.String(), nil)
	if err != nil {
		return nil, cli.Exit(errors.Wrap(err, "failed to dial broker"), 1)
	}

	logger := buildLogger()
	if debug {
		logger.SetLevel(logrus.DebugLevel)
	}

	return &client{
		tunnel:   tunnel,
		logger:   logger,
		wg:       new(sync.WaitGroup),
		ticker:   time.NewTicker(time.Second * 5),
		broker:   broker,
		consumer: consumer,
		done:     make(chan struct{}),
		debug:    debug,
	}, nil
}

func (c *client) readFromWire() {
	defer close(c.done)
	for {
		_, message, err := c.tunnel.ReadMessage()
		if err != nil {
			var webErr = new(websocket.CloseError)
			if errors.As(err, &webErr) {
				if webErr.Code == 1000 {
					break
				}
			}
			c.logger.WithError(err).Error("Read Error")
			break
		}
		c.wg.Add(1)
		go c.handleMessage(message)

	}
	c.logger.Debug("Reader Loop has been stopped, waiting for any inflight requests to complete")
	c.wg.Wait()
}
