package adapter

import (
	"net/http"

	"golang.org/x/net/context"
)

type ServerAdapter interface {
	Handler() http.Handler
	Shutdown(context.Context) error
	//ServeTLS() error
}
