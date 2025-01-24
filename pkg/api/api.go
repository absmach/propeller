package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/absmach/supermq"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
)

const (
	OffsetKey = "offset"
	LimitKey  = "limit"
	DefOffset = 0
	DefLimit  = 100

	ContentType = "application/json"

	MaxLimitSize = 100
)

func EncodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	if ar, ok := response.(supermq.Response); ok {
		for k, v := range ar.Headers() {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", ContentType)
		w.WriteHeader(ar.Code())

		if ar.Empty() {
			return nil
		}
	}

	return json.NewEncoder(w).Encode(response)
}

func EncodeError(_ context.Context, err error, w http.ResponseWriter) {
	w.Header().Set("Content-Type", ContentType)
	switch {
	case errors.Is(err, pkgerrors.ErrEmptyKey):
		w.WriteHeader(http.StatusBadRequest)
	case errors.Is(err, pkgerrors.ErrNotFound):
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}

	if err := json.NewEncoder(w).Encode(err); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
