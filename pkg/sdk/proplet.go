package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/sdf"
)

const propletsEndpoint = "/proplets"

// Proplet is the SDK-facing shape of a proplet. It intentionally differs from
// proplet.Proplet: it exposes CreatedAt (surfaced by the list endpoint) and
// omits AliveHistory and Metadata, which are internal fields not needed by SDK
// callers. Keep in sync with the manager list response when fields change.
type Proplet struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TaskCount   uint64     `json:"task_count"`
	Alive       bool       `json:"alive"`
	LastAliveAt *time.Time `json:"last_alive_at,omitempty"`
}

// PropletPage mirrors the manager list response. Uses the SDK Proplet type
// rather than proplet.PropletPage to match the fields declared above.
type PropletPage struct {
	Offset   uint64    `json:"offset"`
	Limit    uint64    `json:"limit"`
	Total    uint64    `json:"total"`
	Proplets []Proplet `json:"proplets"`
}

func (sdk *propSDK) GetPropletAliveHistory(id string, offset, limit uint64) (proplet.PropletAliveHistoryPage, error) {
	reqURL := fmt.Sprintf("%s%s/%s/alive-history?offset=%d&limit=%d", sdk.managerURL, propletsEndpoint, id, offset, limit)

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return proplet.PropletAliveHistoryPage{}, err
	}

	var page proplet.PropletAliveHistoryPage
	if err := json.Unmarshal(body, &page); err != nil {
		return proplet.PropletAliveHistoryPage{}, err
	}

	return page, nil
}

func (sdk *propSDK) GetPropletSDF(id string) (sdf.Document, error) {
	reqURL := sdk.managerURL + propletsEndpoint + "/" + id + "/sdf"

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return sdf.Document{}, err
	}

	var doc sdf.Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return sdf.Document{}, err
	}

	return doc, nil
}

func (sdk *propSDK) ListProplets(offset, limit uint64, status string) (PropletPage, error) {
	params := make([]string, 0)
	if offset > 0 {
		params = append(params, fmt.Sprintf("offset=%d", offset))
	}
	if limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", limit))
	}
	if status != "" {
		params = append(params, "status="+url.QueryEscape(status))
	}
	query := ""
	if len(params) > 0 {
		query = "?" + strings.Join(params, "&")
	}
	reqURL := sdk.managerURL + propletsEndpoint + query

	body, err := sdk.processRequest(http.MethodGet, reqURL, nil, http.StatusOK)
	if err != nil {
		return PropletPage{}, err
	}

	var pp PropletPage
	if err := json.Unmarshal(body, &pp); err != nil {
		return PropletPage{}, err
	}

	return pp, nil
}

func (sdk *propSDK) DeleteProplet(id string) error {
	reqURL := sdk.managerURL + propletsEndpoint + "/" + id

	if _, err := sdk.processRequest(http.MethodDelete, reqURL, nil, http.StatusNoContent); err != nil {
		return err
	}

	return nil
}
