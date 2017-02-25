package webtty

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"

	"github.com/pkg/errors"
)

type WebTTY struct {
	// PTY Master, which probably a connection to browser
	masterConn Master
	// PTY Slave
	slave Slave

	permitWrite bool
	width       uint16
	height      uint16

	bufferSize int
	writeMutex sync.Mutex
}

type Option func(*WebTTY)

func PermitWrite() Option {
	return func(wt *WebTTY) {
		wt.permitWrite = true
	}
}

func FixSize(width uint16, height uint16) Option {
	return func(wt *WebTTY) {
		wt.width = width
		wt.height = height
	}
}

func New(masterConn Master, slave Slave, options ...Option) (*WebTTY, error) {
	wt := &WebTTY{
		masterConn: masterConn,
		slave:      slave,

		permitWrite: false,
		width:       0,
		height:      0,

		bufferSize: 1024,
	}

	for _, option := range options {
		option(wt)
	}

	return wt, nil
}

func (wt *WebTTY) Run(ctx context.Context) error {
	errs := make(chan error, 2)

	go func() {
		errs <- func() error {
			buffer := make([]byte, wt.bufferSize)
			for {
				n, err := wt.slave.Read(buffer)
				if err != nil {
					return ErrSlaveClosed
				}

				err = wt.handleSlaveReadEvent(buffer[:n])
				if err != nil {
					return err
				}
			}
		}()
	}()

	go func() {
		errs <- func() error {
			for {
				typ, data, err := wt.masterConn.ReadMessage()
				if err != nil {
					return ErrMasterClosed
				}
				if typ != WSTextMessage {
					continue
				}

				err = wt.handleConnectionReadEvent(data)
				if err != nil {
					return err
				}
			}
		}()
	}()

	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-errs:
	}

	return err
}

func (wt *WebTTY) handleSlaveReadEvent(data []byte) error {
	safeMessage := base64.StdEncoding.EncodeToString(data)
	err := wt.connectionWrite(append([]byte{Output}, []byte(safeMessage)...))
	if err != nil {
		return errors.Wrapf(err, "failed to send message to connection")
	}

	return nil
}

func (wt *WebTTY) connectionWrite(data []byte) error {
	wt.writeMutex.Lock()
	defer wt.writeMutex.Unlock()

	err := wt.masterConn.WriteMessage(WSTextMessage, data)
	if err != nil {
		return errors.Wrapf(err, "failed to write to connection")
	}

	return nil
}

func (wt *WebTTY) handleConnectionReadEvent(data []byte) error {
	if len(data) == 0 {
		return errors.New("unexpected zero length read from connection")
	}

	switch data[0] {
	case Input:
		if wt.permitWrite {
			return nil
		}

		if len(data) <= 1 {
			return nil
		}

		_, err := wt.slave.Write(data[1:])
		if err != nil {
			return errors.Wrapf(err, "failed to write received data to tty")
		}

	case Ping:
		err := wt.connectionWrite([]byte{Pong})
		if err != nil {
			return errors.Wrapf(err, "failed to return Pong message to browser")
		}

	case ResizeTerminal:
		if len(data) <= 1 {
			return errors.New("received malformed remote command for terminal resize: empty payload")
		}

		var args argResizeTerminal
		err := json.Unmarshal(data[1:], &args)
		if err != nil {
			return errors.Wrapf(err, "received malformed remote command for terminal resize")
		}
		rows := wt.height
		if rows == 0 {
			rows = uint16(args.Rows)
		}

		columns := wt.width
		if columns == 0 {
			columns = uint16(args.Columns)
		}

		wt.slave.ResizeTerminal(columns, rows)
	default:
		return errors.Errorf("unknown message type `%c`", data[0])
	}

	return nil
}

type argResizeTerminal struct {
	Columns float64
	Rows    float64
}
