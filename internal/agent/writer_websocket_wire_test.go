package agent

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// received is one message as the server saw it: the WebSocket opcode and the raw
// payload, both kept, because "was it binary" is half of what is under test.
type received struct {
	messageType int
	data        []byte
}

// wireServer is a stand-in for nevr-stream's ingest endpoint. It records every
// message verbatim and never interprets it, so the assertions run against bytes
// that actually crossed a socket rather than against an in-process value.
type wireServer struct {
	*httptest.Server
	mu   sync.Mutex
	msgs []received
	// drop, when set, closes the first connection after this many messages, to
	// exercise the reconnect path.
	drop int
}

func newWireServer(t *testing.T, dropAfter int) *wireServer {
	t.Helper()
	ws := &wireServer{drop: dropAfter}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ws.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		n := 0
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			ws.mu.Lock()
			ws.msgs = append(ws.msgs, received{messageType: mt, data: data})
			ws.mu.Unlock()
			n++
			if ws.drop > 0 && n >= ws.drop {
				return // hang up mid-capture
			}
		}
	}))
	t.Cleanup(ws.Close)
	return ws
}

func (s *wireServer) messages() []received {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]received, len(s.msgs))
	copy(out, s.msgs)
	return out
}

func (s *wireServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

// parseIngestEnvelope is an independent re-implementation of the contract
// nevr-stream's ParseEnvelope enforces (internal/api/envelope.go): a 4-byte
// little-endian length prefix, then a telemetry v2 Envelope, and the prefix must
// account for exactly the remaining bytes. It is written out here rather than
// imported so the test cannot pass by sharing a bug with the encoder.
func parseIngestEnvelope(t *testing.T, data []byte) *capturepb.Envelope {
	t.Helper()
	if len(data) < 4 {
		t.Fatalf("frame too short: %d bytes, need at least 4", len(data))
	}
	length := binary.LittleEndian.Uint32(data[:4])
	if want := 4 + int(length); len(data) != want {
		t.Fatalf("frame size mismatch: got %d bytes, prefix declares %d (4 + %d)", len(data), want, length)
	}
	env := &capturepb.Envelope{}
	if err := proto.Unmarshal(data[4:], env); err != nil {
		t.Fatalf("payload is not a telemetry v2 Envelope: %v", err)
	}
	return env
}

func makeRosterFrame(sessionID string, idx uint32, ts time.Time) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		FrameIndex: idx,
		Timestamp:  timestamppb.New(ts),
		Session: &enginev1.SessionResponse{
			SessionId:  sessionID,
			GameStatus: "playing",
			MatchType:  "Echo_Arena",
			MapName:    "mpl_arena_a",
			Teams: []*enginev1.Team{
				{
					TeamName: "Raptured and Friend",
					Players: []*enginev1.TeamMember{
						{
							DisplayName:   "sprocket",
							AccountNumber: 4242,
							SlotNumber:    0,
							JerseyNumber:  7,
							Level:         50,
						},
					},
				},
				{TeamName: "orange"},
			},
		},
	}
}

// writeAndClose drives the writer through a whole capture and waits for the
// server to have seen everything.
func writeAndClose(t *testing.T, w *WebSocketWriter, frames int, sessionID string) {
	t.Helper()
	base := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < frames; i++ {
		f := makeRosterFrame(sessionID, uint32(i), base.Add(time.Duration(i)*100*time.Millisecond))
		if err := w.WriteFrame(f); err != nil {
			t.Fatalf("WriteFrame(%d): %v", i, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	w.Close()
	time.Sleep(150 * time.Millisecond)
}

// TestWebSocketWriter_SpeaksTheIngestWire is the acceptance test for U-2. Before
// this change the writer sent a telemetry V1 Envelope marshalled with protojson
// as a TEXT message; nevr-stream parses a 4-byte-LE-prefixed telemetry V2
// Envelope and rejects non-binary messages outright, so the two could not talk.
func TestWebSocketWriter_SpeaksTheIngestWire(t *testing.T) {
	srv := newWireServer(t, 0)
	w := NewWebSocketWriter(testLogger(t), srv.wsURL(), "")
	if err := w.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	const frames = 3
	writeAndClose(t, w, frames, "8a0c0b6e-0000-4000-8000-000000000001")

	msgs := srv.messages()
	if len(msgs) != frames+2 { // header + frames + footer
		t.Fatalf("server received %d messages, want %d (1 header + %d frames + 1 footer)",
			len(msgs), frames+2, frames)
	}

	// Every message must be BINARY. A single TextMessage here is the old wire.
	for i, m := range msgs {
		if m.messageType != websocket.BinaryMessage {
			t.Fatalf("message %d has opcode %d, want BinaryMessage (%d) — this is the v1 protojson wire",
				i, m.messageType, websocket.BinaryMessage)
		}
	}

	// Order is the capture contract the server's session state machine enforces:
	// exactly one header, then frames, then exactly one footer.
	head := parseIngestEnvelope(t, msgs[0].data)
	hdr := head.GetHeader()
	if hdr == nil {
		t.Fatalf("first message is not a header: %v", head)
	}
	for i := 1; i <= frames; i++ {
		env := parseIngestEnvelope(t, msgs[i].data)
		f := env.GetFrame()
		if f == nil {
			t.Fatalf("message %d is not a frame: %v", i, env)
		}
		if f.GetFrameIndex() != uint32(i-1) {
			t.Errorf("message %d has frame_index %d, want %d", i, f.GetFrameIndex(), i-1)
		}
	}
	tail := parseIngestEnvelope(t, msgs[len(msgs)-1].data)
	footer := tail.GetFooter()
	if footer == nil {
		t.Fatalf("last message is not a footer: %v", tail)
	}
	if footer.GetFrameCount() != frames {
		t.Errorf("footer.frame_count = %d, want %d", footer.GetFrameCount(), frames)
	}

	// The header must carry the roster, because per-frame PlayerState is
	// anonymous (slot only) and this is the only place identity travels.
	ea := hdr.GetEchoArena()
	if ea.GetSessionId() != "8a0c0b6e-0000-4000-8000-000000000001" {
		t.Errorf("header session_id = %q", ea.GetSessionId())
	}
	if ea.GetMapName() != "mpl_arena_a" {
		t.Errorf("header map_name = %q, want mpl_arena_a", ea.GetMapName())
	}
	roster := ea.GetInitialRoster()
	if len(roster) != 1 {
		t.Fatalf("header initial_roster has %d entries, want 1", len(roster))
	}
	if roster[0].GetDisplayName() != "sprocket" {
		t.Errorf("roster[0].display_name = %q, want %q — identity did not reach the wire",
			roster[0].GetDisplayName(), "sprocket")
	}
	if names := ea.GetTeamNames(); len(names) != 2 || names[0] != "Raptured and Friend" {
		t.Errorf("header team_names = %v, want the custom names preserved", names)
	}
}

// TestWebSocketWriter_ResendsHeaderAfterReconnect covers the failure that a
// single-connection test cannot see. nevr-stream builds a FRESH session per
// connection and rejects a frame that arrives before a header, so a writer that
// sends the header once per process produces a capture that works until the first
// blip and silently fails every reconnect after it.
func TestWebSocketWriter_ResendsHeaderAfterReconnect(t *testing.T) {
	// Hang up after the header plus one frame.
	srv := newWireServer(t, 2)
	w := NewWebSocketWriter(testLogger(t), srv.wsURL(), "")
	if err := w.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer w.Close()

	base := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		f := makeRosterFrame("sess-reconnect", uint32(i), base.Add(time.Duration(i)*100*time.Millisecond))
		_ = w.WriteFrame(f)
		time.Sleep(120 * time.Millisecond)
	}
	// Reconnect backoff starts at 1s; give it room to land and send again.
	time.Sleep(3 * time.Second)

	headers := 0
	for i, m := range srv.messages() {
		if m.messageType != websocket.BinaryMessage {
			t.Fatalf("message %d is not binary", i)
		}
		if parseIngestEnvelope(t, m.data).GetHeader() != nil {
			headers++
		}
	}
	if headers < 2 {
		t.Errorf("saw %d headers across a disconnect; want at least 2 — "+
			"a header sent once per process leaves every reconnected capture headerless", headers)
	}
}
