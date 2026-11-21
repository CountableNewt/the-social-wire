package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(rt http.Handler, method, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	rt.ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}

func TestGroupMiddlewareOrderAndErrors(t *testing.T) {
	rt := NewRouter()
	var order []string
	tag := func(name string) Middleware {
		return func(next Handler) Handler {
			return func(w http.ResponseWriter, r *http.Request) error {
				order = append(order, name)
				return next(w, r)
			}
		}
	}
	rt.Use(tag("router"))
	group := rt.Group(tag("outer")).Group(tag("inner"))
	group.Get("/ok", func(w http.ResponseWriter, r *http.Request) error {
		return OK(w, map[string]string{"status": "ok"})
	})
	group.Get("/fail", func(w http.ResponseWriter, r *http.Request) error {
		return Errorf(http.StatusBadRequest, "bad cursor")
	})
	response := serve(rt, "GET", "/ok")
	if response.Code != 200 || response.Body.String() != `{"status":"ok"}` {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
	if len(order) != 3 || order[0] != "router" || order[1] != "outer" || order[2] != "inner" {
		t.Fatalf("order %v", order)
	}
	response = serve(rt, "GET", "/fail")
	if response.Code != 400 || response.Body.String() != `{"error":{"message":"bad cursor"}}` {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
}

func TestXRPCErrorsEnvelope(t *testing.T) {
	rt := NewRouter()
	group := rt.Group(XRPCErrors())
	group.Get("/xrpc/app.test.fail", func(w http.ResponseWriter, r *http.Request) error {
		return Errorf(http.StatusUnauthorized, "missing token")
	})
	group.Get("/xrpc/app.test.crash", func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("database exploded")
	})
	group.Get("/v1/fail", func(w http.ResponseWriter, r *http.Request) error {
		return Status(http.StatusNotFound)
	})
	response := serve(rt, "GET", "/xrpc/app.test.fail")
	if response.Code != 401 || response.Body.String() != `{"error":"AuthRequired","message":"missing token"}` {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
	response = serve(rt, "GET", "/xrpc/app.test.crash")
	if response.Code != 500 || response.Body.String() != `{"error":"InternalServerError","message":"The request could not be completed."}` {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
	response = serve(rt, "GET", "/v1/fail")
	if response.Code != 404 || response.Body.Len() != 0 {
		t.Fatalf("%d %s", response.Code, response.Body)
	}
}

func TestRouterMiddlewareSeesHandlerErrors(t *testing.T) {
	rt := NewRouter()
	var seen int
	rt.Use(func(next Handler) Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			err := next(w, r)
			seen = StatusOf(err)
			return err
		}
	})
	rt.Get("/teapot", func(w http.ResponseWriter, r *http.Request) error {
		return Status(http.StatusTeapot)
	})
	if response := serve(rt, "GET", "/teapot"); response.Code != 418 || seen != 418 {
		t.Fatalf("%d %d", response.Code, seen)
	}
}
