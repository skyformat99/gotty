package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"

	"github.com/yudai/gotty/pkg/homedir"
	"github.com/yudai/gotty/webtty"
)

func (server *Server) generateHandleWS(ctx context.Context, cancel context.CancelFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		server.wsWG.Add(1)
		server.stopTimer()
		if server.options.Once {
			if atomic.LoadInt64(server.once) > 0 {
				http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
				return
			}
			atomic.AddInt64(server.once, 1)
		}
		connections := atomic.AddInt64(server.connections, 1)

		defer func() {
			connections := atomic.AddInt64(server.connections, -1)
			if connections == 0 {
				server.resetTimer()
			}

			server.wsWG.Done()
			if server.options.Once {
				cancel()
			}
		}()

		log.Printf("New client connected: %s", r.RemoteAddr)
		if int64(server.options.MaxConnection) != 0 {
			if connections >= int64(server.options.MaxConnection) {
				log.Printf("Reached max connection: %d", server.options.MaxConnection)
				return
			}
		}

		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}

		conn, err := server.upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, "Failed to upgrade connection: "+err.Error(), 500)
			return
		}
		defer conn.Close()

		err = server.processWSConn(ctx, conn)
		if err != nil {
			log.Printf(err.Error())
			return
		}

		log.Printf(
			"Connection closed: %s, connections: %d/%d",
			r.RemoteAddr, connections, server.options.MaxConnection,
		)
	}
}

func (server *Server) processWSConn(ctx context.Context, conn *websocket.Conn) error {
	typ, initLine, err := conn.ReadMessage()
	if err != nil {
		return errors.Wrapf(err, "failed to authenticate websocket connection")
	}
	if typ != websocket.TextMessage {
		return errors.New("failed to authenticate websocket connection: invalid message type")
	}

	var init InitMessage
	err = json.Unmarshal(initLine, &init)
	if err != nil {
		return errors.Wrapf(err, "failed to authenticate websocket connection")
	}
	if init.AuthToken != server.options.Credential {
		return errors.New("failed to authenticate websocket connection")
	}

	var queryPath string
	if server.options.PermitArguments && init.Arguments != "" {
		queryPath = init.Arguments
	} else {
		queryPath = "?"
	}

	query, err := url.Parse(queryPath)
	if err != nil {
		return errors.Wrapf(err, "failed to parse arguments")
	}
	params := query.Query()
	slave, err := server.factory.New(params)
	if err != nil {
		return errors.Wrapf(err, "failed to create backend")
	}
	defer slave.Close()

	tty, err := webtty.New(conn, slave)
	if err != nil {
		return errors.Wrapf(err, "failed to create tty")
	}

	err = tty.Run(ctx)
	log.Print(err.Error())
	return err
}

func (server *Server) handleCustomIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, homedir.Expand(server.options.IndexFile))
}

func (server *Server) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Write([]byte("var gotty_auth_token = '" + server.options.Credential + "';"))
}
