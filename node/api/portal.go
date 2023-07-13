package api

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"
)

// portalCreditsHandlerGET handles the API call to /portal/credits.
func (api *API) portalCreditsHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	c := api.portal.GetCredits()
	WriteJSON(w, c)
}

// portalCreditsHandlerPOST handles the API call to /portal/credits.
func (api *API) portalCreditsHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Parse parameters.
	var params modules.CreditData
	err := json.NewDecoder(req.Body).Decode(&params)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}

	// Update the credit data.
	api.portal.SetCredits(params)

	WriteSuccess(w)
}
