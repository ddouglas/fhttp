package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/fhttp/fhttp"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

func main() {

	program := cli.NewApp()
	program.Name = "Forward HTTP (FHttp) Client"
	program.Usage = "A CLI for Receiving FHTTP Broker Payloads"
	program.UsageText = "fhttp [global options] [arguments...]"
	program.HideHelpCommand = true
	program.Description = "Establishes Websocket with an FHttp Broker and listens for messages from the broker that can be forwarded to a consumer"
	program.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "broker",
			Usage:       "Set the url of the message broker that the client will connect to. Must be a valid HTTP(S) URI",
			DefaultText: "https://fhttp.dev",
			Value:       "https://fhttp.dev",
		},
		&cli.StringFlag{
			Name:        "consumer",
			Usage:       "Set the URL of the message consumer that the client will forward payloads to. Must be a valid HTTP(S) URI",
			DefaultText: "http://127.0.0.1",
			Value:       "http://127.0.0.1",
		},
		&cli.BoolFlag{
			Name:  "Debug",
			Usage: "Enable debug logs",
		},
	}
	program.Action = actionListen

	err := program.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}

}

func actionListen(c *cli.Context) error {

	broker, err := parseOrFetchBrokerURI(c.String("broker"))
	if err != nil {
		return cli.Exit(err, 1)
	}

	consumer, err := parseConsumerURI(c.String("consumer"), c.Int("port"))
	if err != nil {
		return cli.Exit(err, 1)
	}

	client, err := newClient(broker, consumer, c.Bool("debug"))
	if err != nil {
		return cli.Exit(err, 1)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	client.logger.Debugf("connecting to %s", broker.String())

	defer client.tunnel.Close()
	defer client.ticker.Stop()

	go client.readFromWire()

	for {
		select {
		case <-client.done:
			return cli.Exit("Goodbye!", 1)
		case <-client.ticker.C:
			client.tunnel.SetWriteDeadline(time.Now().Add(time.Second * 5))

			ping := &fhttp.Payload{MessageType: fhttp.MTPing}

			data, err := json.Marshal(ping)
			if err != nil {
				return client.logAndExitError(err, "failed to encode ping message", 1)
			}

			if err := client.tunnel.WriteMessage(websocket.PingMessage, data); err != nil {
				return client.logAndExitError(err, "failed to write ping message", 1)
			}
		case <-interrupt:
			client.logger.Info("Initializing FHttp Client Shutdown per user request")

			err := client.tunnel.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				return client.logAndExitError(err, "failed to write socket close message", 1)
			}
			time.Sleep(closeGracePeriod)
			client.tunnel.Close()
			return cli.Exit("Client Shutdown Successfully!", 0)
		}
	}

}

func (c *client) logAndExitError(err error, msg string, code int) error {
	c.logger.WithError(err).Error(msg)
	return cli.Exit(errors.Wrap(err, msg), code)
}
