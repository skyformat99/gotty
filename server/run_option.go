package server

import (
	"context"
)

type RunOptions struct {
	gracefullCtx context.Context
}

type RunOption func(*RunOptions)

func WithGracefullContext(ctx context.Context) RunOption {
	return func(options *RunOptions) {
		options.gracefullCtx = ctx
	}
}
