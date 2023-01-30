package portal

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/customer"
	"github.com/stripe/stripe-go/v74/paymentintent"
)

// maxBodyBytes specifies the maximum body size for /webhook requests.
const maxBodyBytes = int64(65536)

type item struct {
	ID string `json:"id"`
}

// calculateOrderAmount returns the amount to charge the user from.
func (p *Portal) calculateOrderAmount(email string) int64 {
	// Retrieve the pending payment amount.
	up, err := p.getPendingPayment(email)
	
	// If an error occurs, return a non-zero amount.
	if err != nil {
		return 1
	}

	return int64(up.Amount * 100)
}

// orderCurrency returns the order currency.
func orderCurrency(items []item) string {
	return strings.ToLower(strings.TrimPrefix(items[0].ID, "storage/"))
}

// paymentHandlerPOST handles the POST /stripe/create-payment-intent requests.
func (api *portalAPI) paymentHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	prefix, email, expires, tErr := api.portal.decodeToken(token)
	if tErr != nil || prefix != cookiePrefix {
		writeError(w,
			Error{
				Code: httpErrorTokenInvalid,
				Message: "invalid token",
			}, http.StatusBadRequest)
		return
	}

	if expires.Before(time.Now()) {
		writeError(w,
		Error{
			Code: httpErrorTokenExpired,
			Message: "token already expired",
		}, http.StatusBadRequest)
		return
	}

	// Check if the user account exists.
	count, cErr := api.portal.countEmails(email)
	if cErr != nil {
		api.portal.log.Printf("ERROR: Error querying database: %v\n", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// No such account. Can only happen if it was deleted.
	if count == 0 {
		writeError(w,
			Error{
				Code: httpErrorNotFound,
				Message: "no such account",
			}, http.StatusBadRequest)
		return
	}

	// Retrieve account balance.
	ub, cErr := api.portal.getBalance(email)
	if cErr != nil {
		api.portal.log.Println("ERROR: Could not fetch account balance:", cErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}

	// Retrieve customer ID or create one if there is none.
	var cust *stripe.Customer
	if ub.IsUser && ub.StripeID != "" {
		cust, cErr = customer.Get(ub.StripeID, nil)
		if cErr != nil {
			api.portal.log.Println("ERROR: Could not get customer:", cErr)
			writeError(w,
				Error{
					Code: httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}
	} else {
		params := &stripe.CustomerParams{
			Email: stripe.String(email),
		}
		cust, cErr = customer.New(params)
		if cErr != nil {
			api.portal.log.Println("ERROR: Could not create customer:", cErr)
			writeError(w,
				Error{
					Code: httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}

		// Update the database record.
		ub.IsUser = true
		ub.StripeID = cust.ID
		cErr = api.portal.updateBalance(email, ub)
		if cErr != nil {
			api.portal.log.Println("ERROR: Could not update balance:", cErr)
			writeError(w,
				Error{
					Code: httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}
	}

	// Prepare the decoder and decode the parameters.
	dec, decErr := prepareDecoder(w, req)
	if decErr != nil {
		return
	}

	var data struct {
		Items []item `json:"items"`
	}
	err, code := api.handleDecodeError(w, dec.Decode(&data))
	if code != http.StatusOK {
		writeError(w, err, code)
		return
	}

	// Create a PaymentIntent with amount and currency.
	params := &stripe.PaymentIntentParams{
		Customer: stripe.String(cust.ID),
		Amount:   stripe.Int64(api.portal.calculateOrderAmount(email)),
		Currency: stripe.String(orderCurrency(data.Items)),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
	}

	pi, pErr := paymentintent.New(params)
	if pErr != nil {
		api.portal.log.Println("ERROR: pi.New:", pErr)
		writeError(w,
			Error{
				Code: httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}
	api.portal.log.Printf("pi.New: %v\n", pi.ClientSecret)

	writeJSON(w, struct {
		ClientSecret string `json:"clientSecret"`
	}{
		ClientSecret: pi.ClientSecret,
	})
}

// webhookHandlerPOST handles the POST /stripe/webhook requests.
func (api *portalAPI) webhookHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Read the request body.
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	payload, err := ioutil.ReadAll(req.Body)
	if err != nil {
		api.portal.log.Println("Error reading request body:", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	event := stripe.Event{}

	if err := json.Unmarshal(payload, &event); err != nil {
		api.portal.log.Println("Failed to parse webhook body json:", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Unmarshal the event data into an appropriate struct depending on
	// its Type.
	switch event.Type {
	case "payment_intent.succeeded":
		var paymentIntent stripe.PaymentIntent
		err := json.Unmarshal(event.Data.Raw, &paymentIntent)
		if err != nil {
			api.portal.log.Println("Error parsing webhook JSON:", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		api.portal.handlePaymentIntentSucceeded(paymentIntent)

	default:
		api.portal.log.Printf("Unhandled event type: %s\n", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

// handlePaymentIntentSucceeded handless a successful payment.
func (p *Portal) handlePaymentIntentSucceeded(pi stripe.PaymentIntent) {
	cust := pi.Customer
	err := p.addPayment(cust.ID, float64(pi.Amount / 100), strings.ToUpper(string(pi.Currency)))
	if err != nil {
		p.log.Println("ERROR: Could not add payment:", err)
	}
}

func init() {
	stripe.Key = os.Getenv("SATD_STRIPE_KEY")
}
