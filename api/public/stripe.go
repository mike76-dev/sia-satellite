package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/account"
	"github.com/stripe/stripe-go/v75"
	"github.com/stripe/stripe-go/v75/customer"
	"github.com/stripe/stripe-go/v75/paymentintent"
	"go.uber.org/zap"
)

type item struct {
	ID string `json:"id"`
}

type (
	// PaymentCurrency lists the properties of a payment currency.
	PaymentCurrency struct {
		Name          string  `json:"name"`
		MinimumAmount float32 `json:"minimum"`
		ZeroDecimal   bool    `json:"zeroDecimal"`
	}
)

// allowedCurrencies lists all available currencies.
var allowedCurrencies = []PaymentCurrency{
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
}

// isZeroDecimal is a helper function that returns if the specified
// currency is zero-decimal.
func isZeroDecimal(currency string) bool {
	for _, cur := range allowedCurrencies {
		if cur.Name == currency {
			return cur.ZeroDecimal
		}
	}
	return false
}

// minimumChargeableAmount is a helper function that returns the
// minimum amount chargeable by Stripe for the given currency.
func minimumChargeableAmount(currency string) float64 {
	for _, cur := range allowedCurrencies {
		if cur.Name == currency {
			return float64(cur.MinimumAmount)
		}
	}
	return 0
}

// stripeCurrenciesHandlerGET handles the GET /stripe/currencies requests.
func (s *Server) stripeCurrenciesHandlerGET(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	s.writeJSON(w, allowedCurrencies)
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

// stripeCreatePaymentIntentHandlerPOST handles the POST /stripe/create-payment-intent requests.
func (s *Server) stripeCreatePaymentIntentHandlerPOST(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Check for abuse.
	if err := s.checkAbuse(w, req); err != nil {
		return
	}

	// Extract the authentication token.
	token := getCookie(req, "X-Satellite-Token")
	if token == "" {
		s.writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "no token provided",
			}, http.StatusUnauthorized)
		return
	}

	// Decode the token.
	prefix, email, expires, err := s.accounts.DecodeToken(token)
	if err != nil {
		// Check and update login stats.
		if err := s.checkInvalidTokens(w, req); err != nil {
			return
		}
		s.log.Error("failed to decode token", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "unable to decode token",
			}, http.StatusUnauthorized)
		return
	}

	// Check the token type.
	if prefix != account.CookiePrefix {
		s.writeError(w,
			Error{
				Code:    httpErrorTokenInvalid,
				Message: "wrong token type",
			}, http.StatusUnauthorized)
		return
	}

	// Check the token validity.
	if expires.Before(time.Now()) {
		s.writeError(w,
			Error{
				Code:    httpErrorTokenExpired,
				Message: "token already expired",
			}, http.StatusUnauthorized)
		return
	}

	// Retrieve account.
	acc, err := s.accounts.FindAccount(email)
	if err != nil && errors.Is(err, account.ErrUserNotFound) {
		s.writeError(w,
			Error{
				Code:    httpErrorNotFound,
				Message: "email address not found",
			}, http.StatusBadRequest)
		return
	}

	// Retrieve customer ID or create one if there is none.
	var cust *stripe.Customer
	if acc.StripeID != "" {
		cust, err = customer.Get(acc.StripeID, nil)
		if err != nil {
			s.log.Error("could not get customer", zap.Error(err))
			s.writeError(w,
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
		cust, err = customer.New(params)
		if err != nil {
			s.log.Error("could not create customer", zap.Error(err))
			s.writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}

		// Update the database record.
		acc.StripeID = cust.ID
		err = s.accounts.SaveAccount(acc)
		if err != nil {
			s.log.Error("could not update account", zap.Error(err))
			s.writeError(w,
				Error{
					Code:    httpErrorInternal,
					Message: "internal error",
				}, http.StatusInternalServerError)
			return
		}
	}

	// Prepare the decoder and decode the parameters.
	dec, err := s.prepareDecoder(w, req)
	if err != nil {
		return
	}

	var data struct {
		Items []item `json:"items"`
	}

	httpError, code := s.handleDecodeError(dec.Decode(&data))
	if code != http.StatusOK {
		s.writeError(w, httpError, code)
		return
	}

	// Create a PaymentIntent with amount and currency.
	var sfu, cm *string
	id := data.Items[0].ID
	if strings.HasPrefix(id, "default:") {
		// An indication that this is the default payment method.
		sfu = stripe.String("off_session")
		id = strings.TrimPrefix(id, "default:")
		// An indication to authorize the amount only.
		cm = stripe.String(string(stripe.PaymentIntentCaptureMethodManual))
	}
	amount, currency, err := calculateOrderAmount(id)
	if err != nil {
		s.log.Error("couldn't read pending payment", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorBadRequest,
				Message: "internal error",
			}, http.StatusBadRequest)
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
		CaptureMethod:    cm,
	}

	pi, err := paymentintent.New(params)
	if err != nil {
		s.log.Error("pi.New", zap.Error(err))
		s.writeError(w,
			Error{
				Code:    httpErrorInternal,
				Message: "internal error",
			}, http.StatusInternalServerError)
		return
	}
	s.log.Info("pi.New", zap.String("clientSecret", pi.ClientSecret))

	s.writeJSON(w, struct {
		ClientSecret string `json:"clientSecret"`
	}{
		ClientSecret: pi.ClientSecret,
	})
}
