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

// portalAnnouncementHandlerGET handles the API call to /portal/announcement.
func (api *API) portalAnnouncementHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	a, err := api.portal.GetAnnouncement()
	if err != nil {
		WriteError(w, Error{"internal error: " + err.Error()}, http.StatusInternalServerError)
		return
	}
	WriteJSON(w, struct {
		Text string `json:"text"`
	}{Text: a})
}

// portalAnnouncementHandlerPOST handles the API call to /portal/announcement.
func (api *API) portalAnnouncementHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Parse parameters.
	var params struct {
		Text string `json:"text"`
	}
	err := json.NewDecoder(req.Body).Decode(&params)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}

	// Set the announcement.
	err = api.portal.SetAnnouncement(params.Text)
	if err != nil {
		WriteError(w, Error{"internal error: " + err.Error()}, http.StatusInternalServerError)
		return
	}

	WriteSuccess(w)
}
