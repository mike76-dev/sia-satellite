package portal

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/stripe/stripe-go/v75"
	"github.com/stripe/stripe-go/v75/customer"
	"github.com/stripe/stripe-go/v75/paymentintent"
	"github.com/stripe/stripe-go/v75/webhook"
)

// maxBodyBytes specifies the maximum body size for /webhook requests.
const maxBodyBytes = int64(65536)

type item struct {
	ID string `json:"id"`
}

type (
	// stripeCurrency lists the properties of a Stripe currency.
	stripeCurrency struct {
		Name          string  `json:"name"`
		MinimumAmount float32 `json:"minimum"`
		ZeroDecimal   bool    `json:"zerodecimal"`
	}

	// stripeCurrencies contains a list of currencies.
	stripeCurrencies struct {
		Currencies []stripeCurrency `json:"currencies"`
	}
)

// allowedCurrencies lists all available currencies.
var allowedCurrencies = stripeCurrencies{
	Currencies: []stripeCurrency{
		{
			Name:          "USD",
			MinimumAmount: 0.5,
			ZeroDecimal:   false,
		},
		{
			Name:          "EUR",
			MinimumAmount: 0.5,
			ZeroDecimal:   false,
		},
		{
			Name:          "GBP",
			MinimumAmount: 0.3,
			ZeroDecimal:   false,
		},
		{
			Name:          "CAD",
			MinimumAmount: 0.5,
			ZeroDecimal:   false,
		},
	},
}

// currenciesHandlerGET handles the GET /stripe/currencies requests.
func (api *portalAPI) currenciesHandlerGET(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	writeJSON(w, allowedCurrencies)
}

// isZeroDecimal is a helper function that returns if the specified
// currency is zero-decimal.
func isZeroDecimal(currency string) bool {
	for _, cur := range allowedCurrencies.Currencies {
		if cur.Name == currency {
			return cur.ZeroDecimal
		}
	}
	return false
}

// MinimumChargeableAmount is a helper function that returns the
// minimum amount chargeable by Stripe for the given currency.
func MinimumChargeableAmount(currency string) float64 {
	for _, cur := range allowedCurrencies.Currencies {
		if cur.Name == currency {
			return float64(cur.MinimumAmount)
		}
	}
	return 0
}

// calculateOrderAmount returns the amount to charge the user from.
func calculateOrderAmount(id string) (int64, string, error) {
	if len(id) < 4 {
		return 0, "", errors.New("wrong item length")
	}
	amt := id[:len(id)-3]
	amount, err := strconv.ParseFloat(amt, 64)
	if err != nil {
		return 0, "", err
	}
	currency := id[len(id)-3:]
	if !isZeroDecimal(currency) {
		amount = amount * 100
	}

	return int64(amount), strings.ToLower(currency), nil
}

// paymentHandlerPOST handles the POST /stripe/create-payment-intent requests.
func (api *portalAPI) paymentHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Decode and verify the token.
	token := getCookie(req, "satellite")
	email, cErr := api.verifyCookie(w, token)
	if cErr != nil {
		return
	}

	// Retrieve account balance.
	ub, cErr := api.portal.manager.GetBalance(email)
	if cErr != nil {
		api.portal.log.Println("ERROR: Could not fetch account balance:", cErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
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
					Code:    httpErrorInternal,
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
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}

		// Update the database record.
		ub.IsUser = true
		ub.StripeID = cust.ID
		cErr = api.portal.manager.UpdateBalance(email, ub)
		if cErr != nil {
			api.portal.log.Println("ERROR: Could not update balance:", cErr)
			writeError(w,
				Error{
					Code:    httpErrorInternal,
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
	var sfu *string
	id := data.Items[0].ID
	if strings.HasPrefix(id, "default:") {
		// An indication that this is the default payment method.
		sfu = stripe.String("off_session")
		id = strings.TrimPrefix(id, "default:")
	}
	amount, currency, pErr := calculateOrderAmount(id)
	if pErr != nil {
		api.portal.log.Println("ERROR: couldn't read pending payment:", pErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}
	params := &stripe.PaymentIntentParams{
		Customer: stripe.String(cust.ID),
		Amount:   stripe.Int64(amount),
		Currency: stripe.String(currency),
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
		SetupFutureUsage: sfu,
	}

	pi, pErr := paymentintent.New(params)
	if pErr != nil {
		api.portal.log.Println("ERROR: pi.New:", pErr)
		writeError(w,
			Error{
				Code:    httpErrorInternal,
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
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		api.portal.log.Println("Error reading request body:", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Verify the Stripe signature.
	endpointSecret := os.Getenv("SATD_STRIPE_WEBHOOK_KEY")
	event, err := webhook.ConstructEvent(payload, req.Header.Get("Stripe-Signature"), endpointSecret)
	if err != nil {
		api.portal.log.Println("Error verifying webhook signature:", err)
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
		w.WriteHeader(http.StatusOK)
		api.portal.handlePaymentIntentSucceeded(paymentIntent)
		return

	default:
		api.portal.log.Printf("Unhandled event type: %s\n", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

// handlePaymentIntentSucceeded handless a successful payment.
func (p *Portal) handlePaymentIntentSucceeded(pi stripe.PaymentIntent) {
	cust := pi.Customer
	def := pi.SetupFutureUsage == "off_session"
	currency := strings.ToUpper(string(pi.Currency))
	amount := float64(pi.Amount)
	if !isZeroDecimal(currency) {
		amount = amount / 100
	}
	err := p.addPayment(cust.ID, amount, currency, def)
	if err != nil {
		p.log.Println("ERROR: Could not add payment:", err)
	}

	// If a default payment method was specified, update the customer.
	if def {
		params := &stripe.CustomerParams{
			InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{
				DefaultPaymentMethod: stripe.String(pi.PaymentMethod.ID),
			},
		}
		_, err = customer.Update(pi.Customer.ID, params)
		if err != nil {
			p.log.Println("ERROR: couldn't update customer:", err)
		}
	}
}

// isDefaultPaymentMethodSet returns true if the Stripe customer
// has a default payment method set.
func isDefaultPaymentMethodSet(id string) (bool, error) {
	cust, err := customer.Get(id, nil)
	if err != nil {
		return false, err
	}

	return cust.InvoiceSettings.DefaultPaymentMethod != nil, nil
}

func init() {
	stripe.Key = os.Getenv("SATD_STRIPE_KEY")
}
