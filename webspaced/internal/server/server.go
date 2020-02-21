package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	lxd "github.com/lxc/lxd/client"
	lxdApi "github.com/lxc/lxd/shared/api"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

type key int

const (
	keyConfig key = iota
	keyPwGrProxy
	keyPcred
	keyUser
)

func recordConnUcred(ctx context.Context, c net.Conn) context.Context {
	if unixConn, isUnix := c.(*net.UnixConn); isUnix {
		f, _ := unixConn.File()
		pcred, _ := unix.GetsockoptUcred(int(f.Fd()), unix.SOL_SOCKET, unix.SO_PEERCRED)
		f.Close()

		return context.WithValue(ctx, keyPcred, pcred)
	}
	return ctx
}

// JSONResponse Sends a JSON payload in response to a HTTP request
func JSONResponse(w http.ResponseWriter, v interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		log.WithField("err", err).Error("Failed to serialize JSON payload")

		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Failed to serialize JSON payload")
	}
}

type jsonError struct {
	Message string `json:"message"`
}

// JSONErrResponse Sends an `error` as a JSON object with a `message` property
func JSONErrResponse(w http.ResponseWriter, err error, statusCode int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(statusCode)

	enc := json.NewEncoder(w)
	enc.Encode(jsonError{err.Error()})
}

// UserMiddleware is a middleware for resolving Unix socket peer credentials to a name
func UserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		config := r.Context().Value(keyConfig).(*Config)
		pwGrProxy := r.Context().Value(keyPwGrProxy).(*PwGrProxy)
		pcred := r.Context().Value(keyPcred).(*unix.Ucred)

		username, err := pwGrProxy.LookupUID(pcred.Uid)
		if err != nil {
			username = fmt.Sprintf("u%v", pcred.Uid)
			log.WithFields(log.Fields{
				"err":      err,
				"fallback": username,
			}).Warn("Coudln't find username for UID, using fallback")
		}

		isAdmin, err := pwGrProxy.UserIsMember(username, config.Webspaces.AdminGroup)
		if err != nil {
			log.WithFields(log.Fields{
				"err":   err,
				"user":  username,
				"group": config.Webspaces.AdminGroup,
			}).Warn("Failed to check if user is in admin group")
		}

		// TODO: check for membership of `webspace-admin` group
		if isAdmin || pcred.Uid == 0 {
			if reqUser := r.Header.Get("X-Webspace-User"); reqUser != "" {
				username = reqUser
			}
		}

		log.WithField("username", username).Trace("User authenticated")
		r = r.WithContext(context.WithValue(r.Context(), keyUser, username))
		next.ServeHTTP(w, r)
	})
}

// Server is the main webspaced server struct
type Server struct {
	Config Config
	lxd    lxd.InstanceServer
	http   *http.Server
}

// NewServer returns an initialized Server
func NewServer(config Config) *Server {
	r := mux.NewRouter()
	r.Use(UserMiddleware)
	httpSrv := &http.Server{
		Handler:     r,
		ConnContext: recordConnUcred,
	}

	s := &Server{
		Config: config,
		http:   httpSrv,
	}
	r.HandleFunc("/v1/images", s.apiImages).Methods("GET")
	r.NotFoundHandler = http.HandlerFunc(s.apiNotFound)

	return s
}

// Start begins listening
func (s *Server) Start() error {
	pwGrProxy := NewPwGrProxy(s.Config.PwGrProxySocket)
	s.http.BaseContext = func(_ net.Listener) context.Context {
		return context.WithValue(context.Background(), keyPwGrProxy, pwGrProxy)
	}

	var err error
	if s.lxd, err = lxd.ConnectLXDUnix(s.Config.LXD.Socket, nil); err != nil {
		return err
	}
	if _, _, err := s.lxd.GetNetwork(s.Config.LXD.Network); err != nil {
		return fmt.Errorf("LXD returned error looking for network %v: %w", s.Config.LXD.Network, err)
	}

	var l *lxd.EventListener
	l, err = s.lxd.GetEvents()
	if err != nil {
		return err
	}
	l.AddHandler([]string{"lifecycle"}, s.onLxdEvent)

	listener, err := net.Listen("unix", s.Config.BindSocket)
	if err != nil {
		return err
	}

	// Socket needs to be u=rw,g=rw,o=rw so anyone can access it (we'll do auth later)
	err = os.Chmod(s.Config.BindSocket, 0o666)
	if err != nil {
		return err
	}

	if err := s.http.Serve(listener); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Stop shuts down the server and listener
func (s *Server) Stop() error {
	return s.http.Close()
}

func (s *Server) onLxdEvent(e lxdApi.Event) {
	var details map[string]interface{}
	json.Unmarshal(e.Metadata, &details)
	log.WithFields(log.Fields{
		"type":    e.Type,
		"details": details,
	}).Debug("lxd event")
}

func (s *Server) apiNotFound(w http.ResponseWriter, r *http.Request) {
	JSONErrResponse(w, errors.New("API endpoint not found"), http.StatusNotFound)
}
