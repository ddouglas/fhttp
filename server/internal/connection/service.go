package connection

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/fhttp/fhttp"
	pe "github.com/fhttp/fhttp/pkg/errors"
	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Service interface {
	Connect(ctx context.Context, hash string, w http.ResponseWriter, r *http.Request) error
	CreateConnection(ctx context.Context) (*fhttp.Payload, error)
	PushMessage(ctx context.Context, hash string, message *fhttp.ConsumerMessage) error
}

type service struct {
	connections  map[string]*Link
	upgrader     websocket.Upgrader
	logger       *logrus.Logger
	broker       *url.URL
	deadlineWait time.Duration
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

func New(broker *url.URL, logger *logrus.Logger, readBuffer, writeBuffer int, deadlineWait time.Duration) Service {

	return &service{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  readBuffer,
			WriteBufferSize: writeBuffer,
		},
		connections:  make(map[string]*Link),
		logger:       logger,
		broker:       broker,
		deadlineWait: deadlineWait,
	}
}

func (s *service) CreateConnection(ctx context.Context) (*fhttp.Payload, error) {

	mux := &sync.Mutex{}
	mux.Lock()
	defer mux.Unlock()

	hash := s.generateConnectionID(10)
	if hash == "" {
		return nil, pe.New(pe.ValidateErrorType, errors.New("failed to generate hash, please try again shortly"))
	}

	link := &Link{
		Hash:      hash,
		mux:       mux,
		message:   make(chan []byte),
		connected: false,
	}

	s.connections[hash] = link

	openURI, requestURI := link.URIsFromHash(s.broker)

	message := &fhttp.HelloMessage{
		Hash:       link.Hash,
		OpenURI:    openURI,
		RequestURI: requestURI,
	}

	data, err := json.Marshal(message)
	if err != nil {
		s.logger.WithError(err).Error("failed to encode hello message")
		return nil, errors.Wrap(err, "failed to encode hello message")
	}

	return &fhttp.Payload{
		MessageType: fhttp.MTHello,
		Message:     data,
	}, nil

}

func (s *service) Connect(ctx context.Context, hash string, w http.ResponseWriter, r *http.Request) error {

	// has the conn previous been initialized?
	if _, ok := s.connections[hash]; !ok {
		return pe.New(pe.ValidateErrorType, errors.New("unknown connection"))
	}

	// Attempt to upgrade the connection
	// If the upgrade fail, the Gorrila lib responds to the client for us
	// so return an internal error type so upstream doesn't attempt to
	// reply as well
	socket, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.WithError(err).Error("failed to upgrade connection")
		return nil
	}

	// Upgrade was successful, store the connection,
	// set the expiration for a few hours, and return
	// link
	link := s.connections[hash]
	link.conn = socket
	link.message = make(chan []byte, 10)
	link.connected = true
	s.connections[hash] = link
	go s.handleLink(link)

	return nil
}

func (s *service) PushMessage(ctx context.Context, hash string, message *fhttp.ConsumerMessage) error {

	// has the conn previous been initialized?
	var link *Link
	var ok bool
	if link, ok = s.connections[hash]; !ok {
		return pe.New(pe.ValidateErrorType, errors.New("unknown connection"))
	}

	if !link.connected {
		return pe.New(pe.InternalErrorType, errors.New("client is not connected"))
	}

	data, err := json.Marshal(message)
	if err != nil {
		return pe.New(pe.ValidateErrorType, errors.Wrap(err, "failed to encode message"))
	}

	payload := &fhttp.Payload{
		MessageType: fhttp.MTConsumerMessage,
		Message:     data,
	}

	data, err = json.Marshal(payload)
	if err != nil {
		return pe.New(pe.ValidateErrorType, errors.Wrap(err, "failed to encode payload for client"))
	}

	s.writeMessage(link, data)

	return nil

}

// handleLink is meant to be launched in a goroutine
func (s *service) handleLink(link *Link) {

	// On the offset, sleep for 500 milli just to let things settle
	time.Sleep(time.Millisecond * 500)
	s.writeBrokerURI(link)

	ticker := time.NewTicker(time.Second * 10)

	defer func() {
		defer link.mux.Unlock()
		link.mux.Lock()
		link.conn.Close()
		ticker.Stop()
	}()
	entry := s.logger.WithField("id", link.Hash)
	done := make(chan struct{})

	go func(link *Link, done chan struct{}) {
		defer close(done)
		for {
			// For POC/pre v1, we only care about reading from
			// the socket so that close messages from the clients
			// can be interpreted correct and we can clean up
			// connections. In the future, we may support
			// actually responding to requests via channels potentially
			_, _, err := link.conn.ReadMessage()
			if err != nil {
				var webErr = new(websocket.CloseError)
				if errors.As(err, &webErr) {
					if webErr.Code == 1000 {
						break
					}
				}
				s.logger.WithError(err).Error("Read Error")
				break
			}
		}

	}(link, done)

	for {
		select {
		case <-done:
			s.logger.Debug("Done channel closed")
			return
		case message := <-link.message:
			deadline := time.Now().Add(s.deadlineWait)
			err := link.conn.SetWriteDeadline(deadline)
			if err != nil {
				link.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := link.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				entry.WithError(err).Error("failed to fetch next writer")
				return
			}

			w.Write(message)
			err = w.Close()
			if err != nil {
				entry.WithError(err).Error("failed to close writer, closing conn")
				return
			}
		case <-ticker.C:
			link.conn.SetWriteDeadline(time.Now().Add(s.deadlineWait))

			ping := &fhttp.Payload{MessageType: fhttp.MTPing}

			data, err := json.Marshal(ping)
			if err != nil {
				entry.WithError(err).Error("failed to encode ping message")
			}

			if err := link.conn.WriteMessage(websocket.PingMessage, data); err != nil {
				entry.WithError(err).Error("failed to write ping message, closing conn")
				return
			}
		}
	}
}

func (s *service) writeBrokerURI(link *Link) {

	openURI, requestURI := link.URIsFromHash(s.broker)

	message := &fhttp.HelloMessage{
		Hash:       link.Hash,
		OpenURI:    openURI,
		RequestURI: requestURI,
	}

	data, err := json.Marshal(message)
	if err != nil {
		s.logger.WithError(err).Error("failed to encode hello message")
		return
	}

	payload := &fhttp.Payload{
		MessageType: fhttp.MTHello,
		Message:     data,
	}

	data, err = json.Marshal(payload)
	if err != nil {
		s.logger.WithError(err).Error("failed to encode hello payload")
		return
	}

	s.writeMessage(link, data)

}

// https://stackoverflow.com/questions/25657207/how-to-know-a-buffered-channel-is-full
func (s *service) writeMessage(link *Link, message []byte) {

	link.mux.Lock()
	defer link.mux.Unlock()

	link.lastMessage = time.Now()

	select {
	case link.message <- message:
	default:
		s.logger.WithField("id", link.Hash).Error("failed to write message to link, buffer is full")
	}

}
