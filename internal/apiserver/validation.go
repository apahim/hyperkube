package apiserver

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
)

const maxRequestBodyBytes = 1 << 20 // 1MB

type ValidationMiddleware struct {
	router routers.Router
}

func NewValidationMiddleware(specData []byte) (*ValidationMiddleware, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specData)
	if err != nil {
		return nil, err
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, err
	}
	router, err := legacy.NewRouter(doc)
	if err != nil {
		return nil, err
	}
	return &ValidationMiddleware{router: router}, nil
}

func (v *ValidationMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, pathParams, err := v.router.FindRoute(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		var body []byte
		if r.Body != nil && r.Body != http.NoBody {
			body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
			if err != nil {
				writeErrorMsg(w, http.StatusRequestEntityTooLarge, "Request body too large")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}

		input := &openapi3filter.RequestValidationInput{
			Request:    r,
			PathParams: pathParams,
			Route:      route,
			Options: &openapi3filter.Options{
				AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			},
		}

		if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
			writeErrorMsg(w, http.StatusBadRequest, err.Error())
			return
		}

		if body != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
		}

		next.ServeHTTP(w, r)
	})
}
