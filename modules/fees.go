package modules

type (
	// Price provides the fees depending on the payment plan.
	Price struct {
		PrePayment float64 `json:"prepayment"`
		Invoicing  float64 `json:"invoicing"`
	}

	// Pricing combines the individual prices.
	Pricing struct {
		// FormContract is how much the satellite charges for
		// forming or renewing a single contract.
		FormContract Price `json:"formcontract"`

		// SaveMetadata is the fee for saving a single slab
		// metadata.
		SaveMetadata Price `json:"savemetadata"`

		// StoreMetadata is the fee for storing a single slab
		// metadata for one month.
		StoreMetadata Price `json:"storemetadata"`

		// RetrieveMetadata is the fee for retrieving a single
		// slab metadata.
		RetrieveMetadata Price `json:"retrievemetadata"`

		// MigrateSlab is the fee for migrating a single slab.
		MigrateSlab Price `json:"migrateslab"`
	}
)

// DefaultPricing provides some default prices if none are set.
var DefaultPricing = Pricing{
	FormContract: Price{
		PrePayment: 0.1, // 10% of the contract amount
		Invoicing:  0.2, // 20% of the contract amount
	},
	SaveMetadata: Price{
		PrePayment: 0.1, // 100 mS/slab
		Invoicing:  0.2, // 200 mS/slab
	},
	StoreMetadata: Price{
		PrePayment: 1, // 1 SC/slab/month
		Invoicing:  2, // 2 SC/slab/month
	},
	RetrieveMetadata: Price{
		PrePayment: 0.1, // 100 mS/slab
		Invoicing:  0.2, // 200 mS/slab
	},
	MigrateSlab: Price{
		PrePayment: 0.01, // 10 mS/slab
		Invoicing:  0.02, // 20 mS/slab
	},
}

// StaticPricing keeps the current prices.
var StaticPricing Pricing
