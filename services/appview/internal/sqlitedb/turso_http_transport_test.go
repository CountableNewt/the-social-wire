package sqlitedb

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type tursoCustomTransport struct{ http.RoundTripper }

func TestTursoDefaultTransportIsPrivateCloneWithMatchingIdleCapacity(t *testing.T) {
	original := http.DefaultTransport.(*http.Transport)
	before := original.MaxIdleConnsPerHost
	first := defaultTursoTransport().(*http.Transport)
	defer first.CloseIdleConnections()
	second := defaultTursoTransport().(*http.Transport)
	defer second.CloseIdleConnections()
	if first == original || second == original || first == second {
		t.Fatal("stores share or mutate the global transport")
	}
	if first.MaxIdleConnsPerHost < tursoPoolSize || original.MaxIdleConnsPerHost != before {
		t.Fatal("idle capacity mismatch or global transport mutation")
	}
	if first.IdleConnTimeout != original.IdleConnTimeout || first.TLSHandshakeTimeout != original.TLSHandshakeTimeout || first.ForceAttemptHTTP2 != original.ForceAttemptHTTP2 {
		t.Fatal("default transport behavior changed")
	}
}

func TestTursoHTTP11ParallelBurstsReuseAllConnections(t *testing.T) {
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	arrived := make(chan struct{}, tursoPoolSize)
	var opened atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 1 {
			t.Error("fixture must exercise HTTP/1.1")
		}
		arrived <- struct{}{}
		if r.URL.Path == "/first" {
			<-firstRelease
		} else {
			<-secondRelease
		}
		_, _ = io.WriteString(w, "complete response")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	transport := defaultTursoTransport()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	burst := func(path string, release chan struct{}) {
		t.Helper()
		done := make(chan error, tursoPoolSize)
		for range tursoPoolSize {
			go func() {
				resp, err := client.Get(server.URL + path)
				if err == nil {
					_, err = io.Copy(io.Discard, resp.Body)
					closeErr := resp.Body.Close()
					if err == nil {
						err = closeErr
					}
				}
				done <- err
			}()
		}
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for range tursoPoolSize {
			select {
			case <-arrived:
			case <-timer.C:
				close(release)
				t.Fatal("parallel HTTP burst did not reach the server")
			}
		}
		close(release)
		for range tursoPoolSize {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
	}
	burst("/first", firstRelease)
	afterFirst := opened.Load()
	if afterFirst != int64(tursoPoolSize) {
		t.Fatalf("expected eight simultaneous TCP connections, got %d", afterFirst)
	}
	burst("/second", secondRelease)
	if opened.Load() != afterFirst {
		t.Fatalf("warm burst opened new TCP connections: first=%d total=%d", afterFirst, opened.Load())
	}
}
