package manager

import (
	"fmt"
	"strings"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/portal"
	"github.com/stripe/stripe-go/v75"
	"github.com/stripe/stripe-go/v75/invoice"
	"github.com/stripe/stripe-go/v75/invoiceitem"
	"github.com/stripe/stripe-go/v75/price"
	"github.com/stripe/stripe-go/v75/product"
	"go.uber.org/zap"
)

// threadedSettleAccounts tries to settle the outstanding balances.
func (m *Manager) threadedSettleAccounts() {
	err := m.tg.Add()
	if err != nil {
		return
	}
	defer m.tg.Done()

	for _, renter := range m.Renters() {
		// Get the account balance.
		ub, err := m.GetBalance(renter.Email)
		if err != nil {
			m.log.Error(fmt.Sprintf("couldn't retrieve account balance of %v", renter.Email), zap.Error(err))
			continue
		}

		// Skip if the balance is not negative.
		if ub.Balance+ub.Locked >= 0 {
			continue
		}

		// Skip if the amount to pay is below the minimum chargeable
		// amount.
		if -ub.Balance*ub.SCRate < portal.MinimumChargeableAmount(ub.Currency) {
			continue
		}

		// Sanity check: ub.StripeID shouldn't be empty.
		if ub.StripeID == "" {
			m.log.Error(fmt.Sprintf("Stripe ID not found at %s", renter.Email))
			continue
		}

		// Issue an invoice.
		err = m.managedCreateInvoice(ub.StripeID, ub.Currency, -ub.Balance*ub.SCRate)
		if err != nil {
			m.log.Error(fmt.Sprintf("couldn't create invoice for %v", renter.Email), zap.Error(err))
		}
	}
}

// managedCreateInvoice creates an invoice that the user should pay.
func (m *Manager) managedCreateInvoice(id string, currency string, amount float64) error {
	// Create a product.
	productParams := &stripe.ProductParams{
		Name: stripe.String("Sia Satellite services"),
	}
	product, err := product.New(productParams)
	if err != nil {
		return modules.AddContext(err, "unable to create product")
	}

	// Create a price.
	priceParams := &stripe.PriceParams{
		Currency:   stripe.String(strings.ToLower(currency)),
		Product:    stripe.String(product.ID),
		UnitAmount: stripe.Int64(int64(amount * 100)),
	}
	price, err := price.New(priceParams)
	if err != nil {
		return modules.AddContext(err, "unable to create price")
	}

	// Create an invoice.
	invoiceParams := &stripe.InvoiceParams{
		Customer:         stripe.String(id),
		CollectionMethod: stripe.String("charge_automatically"),
		AutoAdvance:      stripe.Bool(true),
	}
	in, err := invoice.New(invoiceParams)
	if err != nil {
		return modules.AddContext(err, "unable to create invoice")
	}

	// Create an invoice item.
	iiParams := &stripe.InvoiceItemParams{
		Customer: stripe.String(id),
		Price:    stripe.String(price.ID),
		Invoice:  stripe.String(in.ID),
	}
	_, err = invoiceitem.New(iiParams)
	if err != nil {
		return modules.AddContext(err, "unable to create invoice item")
	}

	// Save the invoice ID for tracking.
	err = m.putInvoice(id, in.ID)
	if err != nil {
		return modules.AddContext(err, "unable to save invoice ID")
	}

	return nil
}
