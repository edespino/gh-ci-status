// SPDX-License-Identifier: Apache-2.0

// Package fakegithub is a stand-in for the GitHub REST API, for tests. Routes
// are keyed by path plus raw query exactly as received ("/repos/o/r?x=1");
// anything else answers 404 unless a fallback is set.
package fakegithub

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Server records every request it answers.
type Server struct {
	srv *httptest.Server

	mu          sync.Mutex
	routes      map[string]response
	reqs        []string
	headers     []http.Header
	delay       time.Duration
	fallback    *response
	inFlight    int
	maxInFlight int
}

type response struct {
	status int
	body   string
	header http.Header
}

// New starts a fake API that stops when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{routes: map[string]response{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, key)
	s.headers = append(s.headers, r.Header.Clone())
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	resp, ok := s.routes[key]
	if !ok && s.fallback != nil {
		resp, ok = *s.fallback, true
	}
	delay := s.delay
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()

	if delay > 0 {
		time.Sleep(delay)
	}
	if !ok {
		resp = response{status: http.StatusNotFound, body: `{"message":"Not Found"}`}
	}
	for k, vs := range resp.header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.status)
	_, _ = w.Write([]byte(resp.body))
}

// Handle answers pathAndQuery with status and body.
func (s *Server) Handle(pathAndQuery string, status int, body string) {
	s.HandleWithHeader(pathAndQuery, status, body, nil)
}

// HandleWithHeader is Handle with response headers, such as Link.
func (s *Server) HandleWithHeader(pathAndQuery string, status int, body string, h http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[pathAndQuery] = response{status: status, body: body, header: h}
}

// Fallback answers every unregistered route with status and body.
func (s *Server) Fallback(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fallback = &response{status: status, body: body}
}

// SetDelay makes every answer wait d.
func (s *Server) SetDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

// Requests returns the path and query of every request so far, in order.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

// Headers returns the headers of every request so far, in order.
func (s *Server) Headers() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]http.Header(nil), s.headers...)
}

// MaxInFlight is the most requests that were being answered at once.
func (s *Server) MaxInFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInFlight
}

// URL is the fake API's base URL.
func (s *Server) URL() string { return s.srv.URL }
