package js

import (
	"fmt"
	"net"
	"sync"
	"syscall/js"
	"time"

	"github.com/Quartinal/wisp-server-go/filter"
	"github.com/Quartinal/wisp-server-go/logging"
	"github.com/Quartinal/wisp-server-go/options"
	"github.com/Quartinal/wisp-server-go/packet"
	"github.com/Quartinal/wisp-server-go/websocket"
	gowebsocket "github.com/gorilla/websocket"
)

type ServerStreamJS struct {
	StreamID    uint32
	Conn        *ServerConnectionJS
	Socket      net.Conn
	SendBuffer  *websocket.AsyncQueue
	PacketsSent int
	sync.Mutex
}

func NewServerStreamJS(streamID uint32, conn *ServerConnectionJS, socket net.Conn) *ServerStreamJS {
	return &ServerStreamJS{
		StreamID:    streamID,
		Conn:        conn,
		Socket:      socket,
		SendBuffer:  websocket.NewAsyncQueue(128),
		PacketsSent: 0,
	}
}

func (ssjs *ServerStreamJS) PutData(data []byte) error {
	ssjs.SendBuffer.Put(data)
	return nil
}

func (ss *ServerStreamJS) Close() error {
	ss.SendBuffer.Close()
	return ss.Socket.Close()
}

func (ss *ServerStreamJS) Setup() error {
	go ss.tcpToWS()
	go ss.wsToTCP()
	return nil
}

// tcpToWS reads data from the TCP/UDP socket and sends it to the WebSocket.
func (ss *ServerStreamJS) tcpToWS() {
	defer ss.Close()

	for {
		buffer := make([]byte, 1024)
		n, err := ss.Socket.Read(buffer)
		if err != nil {
			logging.Error(fmt.Sprintf("(%s) Error reading from TCP/UDP socket: %v", ss.Conn.ConnID, err))
			return
		}

		dataPacket := &packet.WispPacket{
			Type:     packet.TypeData,
			StreamID: ss.StreamID,
			Payload: &packet.DataPayload{
				Data: packet.NewWispBuffer(buffer[:n]),
			},
		}

		packetData := dataPacket.Serialize().Bytes()
		err = ss.Conn.WS.WriteMessage(packetData)
		if err != nil {
			logging.Error(fmt.Sprintf("(%s) Error sending data packet: %v", ss.Conn.ConnID, err))
			return
		}
	}
}

// wsToTCP reads data from the WebSocket and writes it to the TCP/UDP socket.
func (ss *ServerStreamJS) wsToTCP() {
	defer ss.Close()

	for {
		data, err := ss.SendBuffer.Get()
		if err != nil {
			logging.Error(fmt.Sprintf("(%s) Error getting data from send buffer: %v", ss.Conn.ConnID, err))
			return
		}

		_, err = ss.Socket.Write(data)
		if err != nil {
			logging.Error(fmt.Sprintf("(%s) Error writing to TCP/UDP socket: %v", ss.Conn.ConnID, err))
			return
		}

		ss.PacketsSent++
		if ss.PacketsSent%64 == 0 {
			continuePacket := &packet.WispPacket{
				Type:     packet.TypeContinue,
				StreamID: ss.StreamID,
				Payload: &packet.ContinuePayload{
					BufferRemaining: uint32(ss.SendBuffer.Capacity() - ss.SendBuffer.Size()),
				},
			}

			packetData := continuePacket.Serialize().Bytes()
			err = ss.Conn.WS.WriteMessage(packetData)
			if err != nil {
				logging.Error(fmt.Sprintf("(%s) Error sending continue packet: %v", ss.Conn.ConnID, err))
				return
			}
		}
	}
}

type WebSocketReadWriter struct {
	conn     *gowebsocket.Conn
	send     chan []byte
	receive  chan []byte
	close    chan struct{}
	once     sync.Once
	JsWsConn js.Value
}

func (wsrw *WebSocketReadWriter) WriteMessage(data []byte) error {
	select {
	case wsrw.send <- data:
		return nil
	case <-wsrw.close:
		return fmt.Errorf("websocket closed")
	}
}

func (wsrw *WebSocketReadWriter) Receive() ([]byte, error) {
	select {
	case data := <-wsrw.receive:
		return data, nil
	case <-wsrw.close:
		return nil, fmt.Errorf("websocket closed")
	}
}

type ServerConnectionJS struct {
	WS      *WebSocketReadWriter
	Path    string
	Streams map[uint32]*ServerStreamJS
	ConnID  string
	Options *options.OptionsStruct
	sync.Mutex
}

// NewServerConnectionJS creates a new ServerConnectionJS.
func NewServerConnectionJS(ws *WebSocketReadWriter, path string, opt *options.OptionsStruct) *ServerConnectionJS {
	return &ServerConnectionJS{
		WS:      ws,
		Path:    path,
		Streams: make(map[uint32]*ServerStreamJS),
		ConnID:  websocket.GetConnID(),
		Options: opt,
	}
}

func (sc *ServerConnectionJS) Setup() error {
	logging.Info(fmt.Sprintf("Setting up new WISP connection with ID %s", sc.ConnID))

	initialContinuePacket := &packet.WispPacket{
		Type:     packet.TypeContinue,
		StreamID: 0,
		Payload: &packet.ContinuePayload{
			BufferRemaining: 128,
		},
	}

	packetData := initialContinuePacket.Serialize().Bytes()
	err := sc.WS.WriteMessage(packetData)
	if err != nil {
		return fmt.Errorf("failed to send initial continue packet: %w", err)
	}

	return nil
}

func (sc *ServerConnectionJS) Run() error {
	defer sc.WS.conn.Close()

	// Heartbeat to keep the connection alive
	go func() {
		for {
			time.Sleep(30 * time.Second)
			err := sc.WS.WriteMessage([]byte{})
			if err != nil {
				logging.Error(fmt.Sprintf("(%s) Error sending heartbeat: %v", sc.ConnID, err))
				return
			}
		}
	}()

	for {
		data, err := sc.WS.Receive()
		if err != nil {
			logging.Error(fmt.Sprintf("(%s) Error receiving data: %v", sc.ConnID, err))
			return err
		}

		err = sc.RoutePacket(data)
		if err != nil {
			logging.Warn(fmt.Sprintf("(%s) Error routing packet: %v", sc.ConnID, err))
		}
	}
}

func (sc *ServerConnectionJS) RoutePacket(data []byte) error {
	packets, err := packet.ParseAllPackets(data)
	if err != nil {
		return fmt.Errorf("failed to parse packet: %w", err)
	}

	for _, p := range packets {
		switch p.Type {
		case packet.TypeConnect:
			payload, ok := p.Payload.(*packet.ConnectPayload)
			if !ok {
				return fmt.Errorf("invalid payload type for CONNECT packet")
			}

			logging.Info(fmt.Sprintf("(%s) Opening new stream to %s:%d", sc.ConnID, payload.Hostname, payload.Port))
			err := sc.CreateStream(p.StreamID, payload.StreamType, payload.Hostname, payload.Port)
			if err != nil {
				logging.Error(fmt.Sprintf("(%s) Error creating stream: %v", sc.ConnID, err))
				sc.CloseStream(p.StreamID, packet.ReasonNetworkError)
			}

		case packet.TypeData:
			sc.Lock()
			stream, exists := sc.Streams[p.StreamID]
			sc.Unlock()
			if !exists {
				logging.Warn(fmt.Sprintf("(%s) Received a DATA packet for a stream which doesn't exist", sc.ConnID))
				continue
			}
			payload, ok := p.Payload.(*packet.DataPayload)
			if !ok {
				return fmt.Errorf("invalid payload type for DATA packet")
			}
			stream.PutData(payload.Data.Bytes())

		case packet.TypeContinue:
			logging.Warn(fmt.Sprintf("(%s) Client sent a CONTINUE packet, this should never be possible", sc.ConnID))

		case packet.TypeClose:
			payload, ok := p.Payload.(*packet.ClosePayload)
			if !ok {
				return fmt.Errorf("invalid payload type for CLOSE packet")
			}
			sc.CloseStream(p.StreamID, payload.Reason)

		default:
			logging.Warn(fmt.Sprintf("(%s) Unknown packet type: %d", sc.ConnID, p.Type))
		}
	}

	return nil
}

func (sc *ServerConnectionJS) CreateStream(streamID uint32, streamType packet.StreamType, hostname string, port uint32) error {
	sc.Lock()
	defer sc.Unlock()

	if _, exists := sc.Streams[streamID]; exists {
		return fmt.Errorf("stream with ID %d already exists", streamID)
	}

	// Create StreamInfo and populate it
	streamInfo := filter.StreamInfo{
		StreamType:  streamType,
		Hostname:    hostname,
		Port:        port,
		StreamCount: len(sc.Streams),
	}

	closeReason := filter.IsStreamAllowed(streamInfo, sc.Options)
	if closeReason != 0 {
		logging.Warn(fmt.Sprintf("(%s) Refusing to create a stream to %s:%d", sc.ConnID, hostname, port))
		closePacket := &packet.WispPacket{
			Type:     packet.TypeClose,
			StreamID: streamID,
			Payload: &packet.ClosePayload{
				Reason: closeReason,
			},
		}

		packetData := closePacket.Serialize().Bytes()
		if err := sc.WS.WriteMessage(packetData); err != nil {
			return fmt.Errorf("failed to send close packet: %w", err)
		}
		return nil
	}

	var socket net.Conn
	var err error

	if streamType == packet.StreamTypeTCP {
		socket, err = net.Dial("tcp", fmt.Sprintf("%s:%d", hostname, port))
	} else if streamType == packet.StreamTypeUDP {
		socket, err = net.Dial("udp", fmt.Sprintf("%s:%d", hostname, port))
	} else {
		return fmt.Errorf("invalid stream type: %d", streamType)
	}

	if err != nil {
		return fmt.Errorf("failed to dial %s:%d: %w", hostname, port, err)
	}

	stream := NewServerStreamJS(streamID, sc, socket)
	sc.Streams[streamID] = stream

	go func() {
		err := stream.Setup()
		if err != nil {
			logging.Error(fmt.Sprintf("(%s) Error setting up stream: %v", sc.ConnID, err))
			sc.CloseStream(streamID, packet.ReasonNetworkError)
		}
	}()

	return nil
}

// CloseStream closes a stream.
func (sc *ServerConnectionJS) CloseStream(streamID uint32, reason packet.CloseReason) error {
	sc.Lock()
	defer sc.Unlock()

	stream, exists := sc.Streams[streamID]
	if !exists {
		return fmt.Errorf("stream with ID %d does not exist", streamID)
	}

	if reason != 0 {
		logging.Info(fmt.Sprintf("(%s) Closing stream to %s for reason %d", sc.ConnID, stream.Socket.RemoteAddr(), reason))
	}

	delete(sc.Streams, streamID)
	return stream.Close()
}
