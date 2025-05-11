package api

import (
	"time"

	"go.sia.tech/core/types"
)

// Error codes provided in an HTTP response.
const (
	HttpErrorNone       = 0
	HttpErrorInternal   = 1
	HttpErrorBadRequest = 2

	HttpErrorEmailInvalid = 10
	HttpErrorEmailUsed    = 11
	HttpErrorEmailTooLong = 12

	HttpErrorPasswordTooShort = 20
	HttpErrorPasswordTooLong  = 21

	HttpErrorWrongCredentials = 30
	HttpErrorTooManyRequests  = 31
	HttpErrorUnverified       = 32

	HttpErrorTokenInvalid = 40
	HttpErrorTokenExpired = 41

	HttpErrorNotFound = 50
)

// Error is a type that is encoded as JSON and returned in an API response in
// the event of an error.
type Error struct {
	// Code identifies the error and enables an easier client-side error handling.
	Code int `json:"code"`
	// Message describes the error. Typically it is set to `err.Error()`.
	Message string `json:"message"`
}

// Error implements the error interface for the Error type. It returns only the
// Message field.
func (err Error) Error() string {
	return err.Message
}

// Balance describes the user's balance.
type Balance struct {
	Total  float64 `json:"total"`
	Locked float64 `json:"locked"`
}

// Currency combines the name of a currency with its exchange rate.
type Currency struct {
	Code   string  `json:"code"`
	SCRate float64 `json:"scRate"`
}

// AccountResponse is the response type for the GET /account request.
type AccountResponse struct {
	Email       string    `json:"email"`
	CreatedAt   time.Time `json:"createdAt"`
	Verified    bool      `json:"verified"`
	PaymentPlan string    `json:"paymentPlan"`
	Balance     Balance   `json:"balance"`
	Currency    Currency  `json:"currency"`
	StripeID    string    `json:"stripeID"`
}

// AccountTokenResponse is the response type for the GET /account/token request.
type AccountTokenResponse struct {
	Token string `json:"token"`
}

// PaymentAddressResponse is the response type for the GET /payment/address request.
type PaymentAddressResponse struct {
	Address types.Address `json:"address"`
}

// init performs the API initialization.
func init() {
	initGoogle()
	initStripe()
}
