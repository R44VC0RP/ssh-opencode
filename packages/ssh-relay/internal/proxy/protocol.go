package proxy

import "encoding/json"

// MessageType represents the type of protocol message
type MessageType string

const (
	MsgInit   MessageType = "init"
	MsgData   MessageType = "data"
	MsgResize MessageType = "resize"
	MsgExit   MessageType = "exit"
	MsgPing   MessageType = "ping"
	MsgPong   MessageType = "pong"
	MsgError  MessageType = "error"
)

// Message is the base message structure
type Message struct {
	Type MessageType `json:"type"`
	// For init
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Repo string `json:"repo,omitempty"`
	// For data (base64 encoded)
	Data string `json:"data,omitempty"`
	// For exit
	Code int `json:"code,omitempty"`
	// For ping/pong
	Timestamp int64 `json:"timestamp,omitempty"`
	// For error
	Error string `json:"error,omitempty"`
}

// NewInitMessage creates an init message
func NewInitMessage(cols, rows int, repo string) *Message {
	return &Message{
		Type: MsgInit,
		Cols: cols,
		Rows: rows,
		Repo: repo,
	}
}

// NewDataMessage creates a data message
func NewDataMessage(data string) *Message {
	return &Message{
		Type: MsgData,
		Data: data,
	}
}

// NewResizeMessage creates a resize message
func NewResizeMessage(cols, rows int) *Message {
	return &Message{
		Type: MsgResize,
		Cols: cols,
		Rows: rows,
	}
}

// NewExitMessage creates an exit message
func NewExitMessage(code int) *Message {
	return &Message{
		Type: MsgExit,
		Code: code,
	}
}

// NewPingMessage creates a ping message
func NewPingMessage(timestamp int64) *Message {
	return &Message{
		Type:      MsgPing,
		Timestamp: timestamp,
	}
}

// Marshal converts the message to JSON
func (m *Message) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// ParseMessage parses a JSON message
func ParseMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
