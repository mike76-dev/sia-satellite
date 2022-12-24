package portal

import (
	"net/http"
	"os"

	"github.com/julienschmidt/httprouter"
	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/paymentintent"
)

type item struct {
	ID string `json:"id"`
}

func (p *Portal) calculateOrderAmount(items []item) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If paymentAmount is zero, return a non-zero amount.
	if p.paymentAmount == 0 {
		return 1
	}

	return int64(p.paymentAmount * 100)
}

// paymentHandlerPOST handles the POST /stripe/create-payment-intent requests.
func (api *portalAPI) paymentHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
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

	// Create a PaymentIntent with amount and currency
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(api.portal.calculateOrderAmount(data.Items)),
		Currency: stripe.String(string(stripe.CurrencyUSD)),
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

func init() {
	stripe.Key = os.Getenv("SATD_STRIPE_KEY")
}
