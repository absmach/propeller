package api_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	managerapi "github.com/absmach/propeller/manager/api"
	"github.com/absmach/propeller/manager/mocks"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/sdf"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newServer(t *testing.T) (*httptest.Server, *mocks.MockService) {
	t.Helper()
	svc := new(mocks.MockService)
	handler := managerapi.MakeHandler(svc, slog.Default(), "test")

	return httptest.NewServer(handler), svc
}

func TestGetPropletSDF(t *testing.T) {
	t.Parallel()

	validID := uuid.NewString()
	validDoc := sdf.Document{
		Info: sdf.Info{Title: "test", Version: "1.0"},
	}

	cases := []struct {
		desc       string
		propletID  string
		svcDoc     sdf.Document
		svcErr     error
		wantStatus int
		wantTitle  string
	}{
		{
			desc:       "get SDF for existing proplet",
			propletID:  validID,
			svcDoc:     validDoc,
			svcErr:     nil,
			wantStatus: http.StatusOK,
			wantTitle:  "test",
		},
		{
			desc:       "get SDF for unknown proplet returns 404",
			propletID:  uuid.NewString(),
			svcDoc:     sdf.Document{},
			svcErr:     pkgerrors.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			desc:       "get SDF for non-existent proplet ID returns 404",
			propletID:  "not-a-valid-uuid",
			svcDoc:     sdf.Document{},
			svcErr:     pkgerrors.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			desc:       "get SDF with empty proplet ID returns 400",
			propletID:  "",
			svcDoc:     sdf.Document{},
			svcErr:     nil,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			ts, svc := newServer(t)
			defer ts.Close()

			if tc.propletID != "" {
				svc.On("GetPropletSDF", mock.Anything, tc.propletID).Return(tc.svcDoc, tc.svcErr)
			}

			reqURL := fmt.Sprintf("%s/proplets/%s/sdf", ts.URL, tc.propletID)
			res, err := http.Get(reqURL)
			require.NoError(t, err)
			defer res.Body.Close()

			assert.Equal(t, tc.wantStatus, res.StatusCode)

			if tc.wantStatus == http.StatusOK {
				body, err := io.ReadAll(res.Body)
				require.NoError(t, err)

				var doc sdf.Document
				require.NoError(t, json.Unmarshal(body, &doc))
				assert.Equal(t, tc.wantTitle, doc.Info.Title)
			}
		})
	}
}
