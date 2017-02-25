package webtty

import (
	"io"
)

type Slave interface {
	io.ReadWriteCloser

	WindowTitle() (string, error)
	ResizeTerminal(columns uint16, rows uint16) error
}
