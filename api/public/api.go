package api

// Error codes provided in an HTTP response.
const (
	httpErrorNone       = 0
	httpErrorInternal   = 1
	httpErrorBadRequest = 2

	httpErrorEmailInvalid = 10
	httpErrorEmailUsed    = 11
	httpErrorEmailTooLong = 12

	httpErrorPasswordTooShort = 20
	httpErrorPasswordTooLong  = 21

	httpErrorWrongCredentials = 30
	httpErrorTooManyRequests  = 31

	httpErrorTokenInvalid = 40
	httpErrorTokenExpired = 41

	httpErrorNotFound = 50
)

// Error is a type that is encoded as JSON and returned in an API response in
// the event of an error.
type Error struct {
	// Code identifies the error and enables an easier client-side error handling.
	Code int `json:"code"`
	// Message describes the error in English. Typically it is set to
	// `err.Error()`. This field is required.
	Message string `json:"message"`
}

// Error implements the error interface for the Error type. It returns only the
// Message field.
func (err Error) Error() string {
	return err.Message
}
