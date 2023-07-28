package modules

var (
	// FormContractFee is how much the satellite charges for
	// forming a single contract.
	FormContractFee = 0.1 // 10% of the contract amount

	// SaveMetadataFee is the flat fee for saving a single slab
	// metadata.
	SaveMetadataFee = 0.1 // 100 mS / slab

	// StoreMetadataFee is the flat fee for storing a single slab
	// metadata for one month.
	StoreMetadataFee = 1 // 1 SC/slab/month

	// RetrieveMetadataFee is the flat fee for retrieving a single
	// slab metadata.
	RetrieveMetadataFee = 0.1 // 100 mS/slab
)
