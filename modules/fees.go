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

		// StorePartialData is the fee for storing one MiB of
		// partial slab data for one month.
		StorePartialData Price `json:"storepartialdata"`

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
		PrePayment: 0.1,  // 10% of the contract amount
		Invoicing:  0.15, // 15% of the contract amount
	},
	// Roughly corresponds to 1-1.5 USD for saving metadata of
	// a 1 TB file.
	SaveMetadata: Price{
		PrePayment: 0.01,  // 10 mS/slab
		Invoicing:  0.015, // 15 mS/slab
	},
	// Roughly corresponds to 1-1.5 USD/month for storing metadata
	// of a 1 TB file.
	StoreMetadata: Price{
		PrePayment: 0.01,  // 10 mS/slab/month
		Invoicing:  0.015, // 15 mS/slab/month
	},
	// Roughly corresponds to 20-30 USD for storing 1 TB of partial
	// slab data.
	// This storage is supposed to reside on a high-performance
	// drive (SSD or even NVMe), the same where the satellite OS
	// resides, and is thus much more expensive.
	// For comparison: partial slab data of any file, even as large
	// as 1 TB, usually doesn't exceed 40 MB.
	StorePartialData: Price{
		PrePayment: 0.005,  // 5000 uS/MiB/month
		Invoicing:  0.0075, // 7500 uS/MiB/month
	},
	// Roughly corresponds to 10-15 USD for restoring a 1 TB file.
	// Retrieving metadata is supposed to happen much less frequently,
	// if at all.
	RetrieveMetadata: Price{
		PrePayment: 0.1,  // 100 mS/slab
		Invoicing:  0.15, // 150 mS/slab
	},
	// Roughly corresponds to 1-1.5 USD for fully migrating a 1 TB file.
	MigrateSlab: Price{
		PrePayment: 0.01,  // 10 mS/slab
		Invoicing:  0.015, // 15 mS/slab
	},
}

// StaticPricing keeps the current prices.
var StaticPricing Pricing
