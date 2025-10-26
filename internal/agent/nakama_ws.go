package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

var (
	ErrSessionQueueFull = errors.New("session outgoing queue full")
)

type NakamaWebSocketClient struct {
	logger       *zap.Logger
	httpURL      string
	socketURL    string
	serverKey    string
	httpKey      string
	username     string
	password     string
	httpClient   *http.Client
	conn         *websocket.Conn
	sessionToken string
	outgoingCh   chan []byte
	ctx          context.Context
	cancel       context.CancelFunc
}

type AuthenticateCustomRequest struct {
	Account  *api.AccountCustom `json:"account"`
	Create   *bool              `json:"create,omitempty"`
	Username string             `json:"username,omitempty"`
}

func NewStreamClient(logger *zap.Logger, httpURL, socketURL, httpKey, serverKey, username, password string) *NakamaWebSocketClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &NakamaWebSocketClient{
		logger: logger,

		httpURL:   httpURL,
		socketURL: socketURL,
		httpKey:   httpKey,
		serverKey: serverKey,
		username:  username,
		password:  password,

		httpClient: &http.Client{Timeout: 30 * time.Second},
		outgoingCh: make(chan []byte, 100),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (sc *NakamaWebSocketClient) Connect() error {
	// Step 1: Authenticate and get session token
	if err := sc.authenticate(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Step 2: Connect to websocket
	if err := sc.connectWebSocket(); err != nil {
		return fmt.Errorf("websocket connection failed: %w", err)
	}

	// Step 4: Start message handling goroutine
	go sc.processIncoming()

	return nil
}

func (sc *NakamaWebSocketClient) authenticate() error {
	request := map[string]any{
		"username": sc.username,
		"password": sc.password,
	}

	reqBody, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal auth request: %w", err)
	}

	// Build the URL
	url := fmt.Sprintf(sc.httpURL+"/v2/rpc/account/authenticate/password?unwrap&http_key=%s", sc.httpKey)

	req, err := http.NewRequestWithContext(sc.ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	//req.Header.Set("Authorization", "Bearer "+sc.serverKey)

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth request failed with status: %d", resp.StatusCode)
	}

	var session api.Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return fmt.Errorf("failed to decode session response: %w", err)
	}

	sc.sessionToken = session.Token
	sc.logger.Info("Successfully authenticated", zap.String("token_prefix", sc.sessionToken[:10]+"..."))

	return nil
}

func (sc *NakamaWebSocketClient) connectWebSocket() error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+sc.sessionToken)

	conn, _, err := websocket.DefaultDialer.DialContext(sc.ctx, sc.socketURL, header)
	if err != nil {
		return fmt.Errorf("failed to dial websocket: %w", err)
	}

	sc.conn = conn
	sc.logger.Info("WebSocket connected successfully")

	return nil
}

func (sc *NakamaWebSocketClient) joinTelemetryStream(matchUUID string) error {
	payload, err := json.Marshal(map[string]any{"match_id": matchUUID})
	if err != nil {
		return fmt.Errorf("failed to marshal join telemetry stream payload: %w", err)
	}

	rpcMsg := &api.Rpc{
		Id:      "/telemetry/stream/join",
		Payload: string(payload),
	}

	envelope := &rtapi.Envelope{
		Message: &rtapi.Envelope_Rpc{
			Rpc: rpcMsg,
		},
	}

	data, err := proto.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal RPC envelope: %w", err)
	}

	if err := sc.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to send RPC message: %w", err)
	}

	sc.logger.Info("Sent telemetry stream join RPC")
	return nil
}

func (sc *NakamaWebSocketClient) processIncoming() {
	defer sc.conn.Close()

	for {
		select {
		case <-sc.ctx.Done():
			return
		default:
		}

		_, message, err := sc.conn.ReadMessage()
		if err != nil {
			sc.logger.Error("Failed to read message", zap.Error(err))
			return
		}

		var envelope rtapi.Envelope
		if err := proto.Unmarshal(message, &envelope); err != nil {
			sc.logger.Error("Failed to unmarshal envelope", zap.Error(err))
			continue
		}

		switch msg := envelope.Message.(type) {
		case *rtapi.Envelope_StreamPresenceEvent:
			sc.logger.Info("Received StreamPresenceEvent", zap.Any("event", msg.StreamPresenceEvent))
			// Start data ingestion goroutine after receiving presence event
			go sc.processOutgoing()

		case *rtapi.Envelope_StreamData:
			sc.logger.Debug("Received StreamData", zap.Int("data_length", len(msg.StreamData.Data)))

		case *rtapi.Envelope_Rpc:
			sc.logger.Info("Received RPC response", zap.String("id", msg.Rpc.Id))

		default:
			sc.logger.Debug("Received unknown message type", zap.String("type", fmt.Sprintf("%T", msg)))
		}
	}
}

func (sc *NakamaWebSocketClient) processOutgoing() {
	sc.logger.Info("Starting data ingestion routine")
	for {
		select {
		case <-sc.ctx.Done():
			return
		case payload := <-sc.outgoingCh:
			if err := sc.conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				sc.logger.Warn("Failed to send stream data", zap.Error(err))
				continue
			}

			sc.logger.Debug("Sent stream data", zap.Int("data_length", len(payload)))
		}
	}
}

func (s *NakamaWebSocketClient) Send(envelope *rtapi.Envelope, reliable bool) error {
	payload, err := proto.Marshal(envelope)
	if err != nil {
		s.logger.Warn("Could not marshal envelope", zap.Error(err), zap.Any("envelope", envelope))
		return err
	}

	return s.SendBytes(payload, reliable)
}

func (s *NakamaWebSocketClient) SendBytes(payload []byte, reliable bool) error {
	// Attempt to queue messages and observe failures.
	select {
	case s.outgoingCh <- payload:
		return nil
	default:
		// The outgoing queue is full, likely because the remote client can't keep up.
		// Terminate the connection immediately because the only alternative that doesn't block the server is
		// to start dropping messages, which might cause unexpected behaviour.
		s.logger.Warn("Could not write message, session outgoing queue full")
		// Close in a goroutine as the method can block
		go s.Close()
		return ErrSessionQueueFull
	}
}

func (sc *NakamaWebSocketClient) Close() error {
	sc.cancel()
	if sc.conn != nil {
		return sc.conn.Close()
	}
	return nil
}
