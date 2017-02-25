package backend

import (
	"github.com/yudai/gotty/webtty"
)

type Factory interface {
	New(params map[string][]string) (webtty.Slave, error)
}
