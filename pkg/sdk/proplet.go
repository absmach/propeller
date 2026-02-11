package sdk

import "net/http"

const propletsEndpoint = "/proplets"

func (sdk *propSDK) DeleteProplet(id string) error {
	url := sdk.managerURL + propletsEndpoint + "/" + id

	if _, err := sdk.processRequest(http.MethodDelete, url, nil, http.StatusNoContent); err != nil {
		return err
	}

	return nil
}
