package modules

import (
	"math/big"
	"time"

	"go.sia.tech/core/types"
)

var numGenesisSiacoins types.Currency

// BytesPerTerabyte is how many bytes are there in one TiB.
const BytesPerTerabyte = 1024 * 1024 * 1024 * 1024

const (
	// BlocksPerHour is the number of blocks expected to be mined per hour.
	BlocksPerHour = uint64(6)
	// BlocksPerDay is the number of blocks expected to be mined per day.
	BlocksPerDay = 24 * BlocksPerHour
	// BlocksPerWeek is the number of blocks expected to be mined per week.
	BlocksPerWeek = 7 * BlocksPerDay
	// BlocksPerMonth is the number of blocks expected to be mined per month.
	BlocksPerMonth = 30 * BlocksPerDay
	// BlocksPerYear is the number of blocks expected to be mined per year.
	BlocksPerYear = 365 * BlocksPerDay
)

// SectorSize defines how large a sector should be in bytes. The sector
// size needs to be a power of two to be compatible with package
// merkletree. 4MiB has been chosen for the live network because large
// sectors significantly reduce the tracking overhead experienced by the
// renter and the host.
const SectorSize = uint64(1 << 22)

// EstimatedFileContractTransactionSetSize is the estimated blockchain size
// of a transaction set between a renter and a host that contains a file
// contract. This transaction set will contain a setup transaction from each
// the host and the renter, and will also contain a file contract and file
// contract revision that have each been signed by all parties.
const EstimatedFileContractTransactionSetSize = 2048

const (
	// InitialCoinbase is the coinbase reward of the Genesis block.
	InitialCoinbase = uint64(300e3)

	// The minimum coinbase is set to 30,000. Because the coinbase
	// decreases by 1 every time, it means that Sia's coinbase will have an
	// increasingly potent dropoff for about 5 years, until inflation more
	// or less permanently settles around 2%.
	MinimumCoinbase = 30e3

	// SiafundCount is the total number of Siafunds.
	SiafundCount = 10000
)

const (
	// AcceptResponse is the response given to an RPC call to indicate
	// acceptance, i.e. that the sender wishes to continue communication.
	AcceptResponse = "accept"

	// StopResponse is the response given to an RPC call to indicate graceful
	// termination, i.e. that the sender wishes to cease communication, but
	// not due to an error.
	StopResponse = "stop"
)

var (
	// BlockBytesPerMonthTerabyte is the conversion rate between block-bytes and month-TB.
	BlockBytesPerMonthTerabyte = types.NewCurrency64(BytesPerTerabyte).Mul64(BlocksPerMonth)
)

const (
	// BlockSizeLimit is the maximum size of a binary-encoded Block
	// that is permitted by the consensus rules.
	BlockSizeLimit = uint64(2e6)

	// A block time of 1 block per 10 minutes is chosen to follow Bitcoin's
	// example. The security lost by lowering the block time is not
	// insignificant, and the convenience gained by lowering the blocktime
	// even down to 90 seconds is not significant. 10 minutes could even be
	// too short, but it has worked well for Bitcoin.
	BlockFrequency = 600

	// When the difficulty is adjusted, it is adjusted by looking at the
	// timestamp of the 1000th previous block. This minimizes the abilities
	// of miners to attack the network using rogue timestamps.
	TargetWindow = 1e3

	// ASICHardforkHeight is the height at which the hardfork targeting
	// selected ASICs was activated.
	ASICHardforkHeight = uint64(179000)

	// ASICHardforkFactor is the factor by which the hashrate of targeted
	// ASICs will be reduced.
	ASICHardforkFactor = uint64(1009)

	// A total time of 120,000 is chosen because that represents the total
	// time elapsed at a perfect equilibrium, indicating a visible average
	// block time that perfectly aligns with what is expected.
	ASICHardforkTotalTime = 120e3

	// DevAddrHardforkHeight is the height at which the DevAddr hardfork was
	// activated.
	DevAddrHardforkHeight = uint64(10000)

	// The oak difficulty adjustment hardfork is set to trigger at block
	// 135,000, which is just under 6 months after the hardfork was first
	// released as beta software to the network. This hopefully gives
	// everyone plenty of time to upgrade and adopt the hardfork, while also
	// being earlier than the most optimistic shipping dates for the miners
	// that would otherwise be very disruptive to the network.
	//
	// There was a bug in the original Oak hardfork that had to be quickly
	// followed up with another fix. The height of that fix is the
	// OakHardforkFixBlock.
	OakHardforkBlock = 135e3
	OakHardforkFixBlock = 139e3

	// OakHardforkTxnSizeLimit is the maximum size allowed for a transaction,
	// a change which was implemented simultaneously with the oak hardfork.
	OakHardforkTxnSizeLimit = uint64(64e3) // 64 KB

	// The decay is kept at 995/1000, or a decay of about 0.5% each block.
	// This puts the halflife of a block's relevance at about 1 day. This
	// allows the difficulty to adjust rapidly if the hashrate is adjusting
	// rapidly, while still keeping a relatively strong insulation against
	// random variance.
	OakDecayNum = 995
	OakDecayDenom = 1e3

	// The block shift determines the most that the difficulty adjustment
	// algorithm is allowed to shift the target block time. With a block
	// frequency of 600 seconds, the min target block time is 200 seconds,
	// and the max target block time is 1800 seconds.
	OakMaxBlockShift = 3

	// TaxHardforkHeight is the height at which the tax hardfork occurred.
	TaxHardforkHeight = uint64(21000)

	// FoundationHardforkHeight is the height at which the Foundation subsidy
	// hardfork was activated.
	FoundationHardforkHeight = uint64(298000)

	// The Foundation subsidy hardfork activates at approximately 11pm EST
	// on February 3, 2021.
	// Subsidies are paid out approximately once per month. Since actual
	// months vary in length, we instead divide the total number of blocks
	// per year by 12.
	FoundationSubsidyFrequency = BlocksPerYear / 12

	// Blocks will not be accepted if their timestamp is more than 3 hours
	// into the future, but will be accepted as soon as they are no longer
	// 3 hours into the future. Blocks that are greater than 5 hours into
	// the future are rejected outright, as it is assumed that by the time
	// 2 hours have passed, those blocks will no longer be on the longest
	// chain. Blocks cannot be kept forever because this opens a DoS vector.
	FutureThreshold = 3 * 60 * 60        // 3 hours.
	ExtremeFutureThreshold = 5 * 60 * 60 // 5 hours.

	// Payouts take 1 day to mature. This is to prevent a class of double
	// spending attacks where parties unintentionally spend coins that will
	// stop existing after a blockchain reorganization. There are multiple
	// classes of payouts in Sia that depend on a previous block - if that
	// block changes, then the output changes and the previously existing
	// output ceases to exist. This delay stops both unintentional double
	// spending and a small set of long-range mining attacks.
	MaturityDelay = 144

	// MedianTimestampWindow tells us how many blocks to look back when
	// calculating the median timestamp over the previous n blocks. The
	// timestamp of a block is not allowed to be less than or equal to the
	// median timestamp of the previous n blocks, where for Sia this number
	// is typically 11.
	MedianTimestampWindow = uint64(11)

	// StorageProofHardforkHeight is the height at which the storage proof
	// hardfork was activated.
	StorageProofHardforkHeight = uint64(100000)
)

var (
	GenesisSiacoinAllocation = []types.SiacoinOutput{}

	GenesisSiafundAllocation = []types.SiafundOutput{
		{
			Value:   2,
			Address: types.Address{4, 57, 229, 188, 127, 20, 204, 245, 211, 167, 232, 130, 208, 64, 146, 62, 69, 98, 81, 102, 221, 7, 123, 100, 70, 107, 199, 113, 121, 26, 198, 252},
		},
		{
			Value:   6,
			Address: types.Address{4, 158, 29, 42, 105, 119, 43, 5, 138, 72, 190, 190, 101, 114, 79, 243, 189, 248, 208, 151, 30, 187, 233, 148, 225, 233, 28, 159, 19, 232, 75, 244},
		},
		{
			Value:   7,
			Address: types.Address{8, 7, 66, 250, 25, 74, 247, 108, 162, 79, 220, 151, 202, 228, 241, 11, 130, 138, 13, 248, 193, 167, 136, 197, 65, 63, 234, 174, 205, 216, 71, 230},
		},
		{
			Value:   8,
			Address: types.Address{44, 106, 239, 51, 138, 102, 242, 19, 204, 197, 248, 178, 219, 122, 152, 251, 19, 20, 52, 32, 175, 32, 4, 156, 73, 33, 163, 165, 222, 184, 217, 218},
		},
		{
			Value:   3,
			Address: types.Address{44, 163, 31, 233, 74, 103, 55, 132, 230, 159, 97, 78, 149, 147, 65, 110, 164, 211, 105, 173, 158, 29, 202, 43, 85, 217, 85, 75, 83, 37, 205, 223},
		},
		{
			Value:   1,
			Address: types.Address{51, 151, 146, 84, 199, 7, 59, 89, 111, 172, 227, 200, 62, 55, 165, 253, 238, 186, 28, 145, 47, 137, 200, 15, 70, 199, 187, 125, 243, 104, 179, 240},
		},
		{
			Value:   10,
			Address: types.Address{53, 118, 253, 229, 254, 229, 28, 131, 233, 156, 108, 58, 197, 152, 17, 160, 74, 252, 11, 49, 112, 240, 66, 119, 40, 98, 114, 251, 5, 86, 233, 117},
		},
		{
			Value:   50,
			Address: types.Address{56, 219, 3, 50, 28, 3, 166, 95, 141, 163, 202, 35, 60, 199, 219, 10, 151, 176, 228, 97, 176, 133, 189, 33, 211, 202, 83, 197, 31, 208, 254, 193},
		},
		{
			Value:   75,
			Address: types.Address{68, 190, 140, 87, 96, 232, 150, 32, 161, 177, 204, 65, 228, 223, 87, 217, 134, 90, 25, 56, 51, 45, 72, 107, 129, 12, 29, 202, 6, 7, 50, 13},
		},
		{
			Value:   10,
			Address: types.Address{69, 14, 201, 200, 90, 73, 245, 45, 154, 94, 161, 19, 199, 241, 203, 56, 13, 63, 5, 220, 121, 245, 247, 52, 194, 181, 252, 76, 130, 6, 114, 36},
		},
		{
			Value:   10,
			Address: types.Address{72, 128, 253, 207, 169, 48, 1, 26, 237, 205, 169, 102, 196, 224, 42, 186, 95, 151, 59, 226, 203, 136, 251, 223, 165, 38, 88, 110, 47, 213, 121, 224},
		},
		{
			Value:   50,
			Address: types.Address{72, 130, 164, 227, 218, 28, 60, 15, 56, 151, 212, 242, 77, 131, 232, 131, 42, 57, 132, 173, 113, 118, 66, 183, 38, 79, 96, 178, 105, 108, 26, 247},
		},
		{
			Value:   10,
			Address: types.Address{74, 210, 58, 228, 111, 69, 253, 120, 53, 195, 110, 26, 115, 76, 211, 202, 199, 159, 204, 14, 78, 92, 14, 131, 250, 22, 141, 236, 154, 44, 39, 135},
		},
		{
			Value:   15,
			Address: types.Address{85, 198, 154, 41, 196, 116, 226, 114, 202, 94, 214, 147, 87, 84, 247, 164, 195, 79, 58, 123, 26, 33, 68, 65, 116, 79, 181, 241, 241, 208, 215, 184},
		},
		{
			Value:   121,
			Address: types.Address{87, 239, 83, 125, 152, 14, 19, 22, 203, 136, 46, 192, 203, 87, 224, 190, 77, 236, 125, 18, 142, 223, 146, 70, 16, 23, 252, 19, 100, 69, 91, 111},
		},
		{
			Value:   222,
			Address: types.Address{91, 201, 101, 11, 188, 40, 35, 111, 236, 133, 31, 124, 97, 246, 140, 136, 143, 245, 152, 174, 111, 245, 188, 124, 21, 125, 187, 192, 203, 92, 253, 57},
		},
		{
			Value:   10,
			Address: types.Address{110, 240, 238, 173, 78, 138, 185, 138, 179, 227, 135, 153, 54, 132, 46, 62, 226, 206, 204, 35, 174, 107, 156, 15, 142, 2, 93, 132, 163, 60, 50, 89},
		},
		{
			Value:   3,
			Address: types.Address{114, 58, 147, 44, 64, 69, 72, 184, 65, 178, 213, 94, 157, 44, 88, 106, 92, 31, 145, 193, 215, 200, 215, 233, 99, 116, 36, 197, 160, 70, 79, 153},
		},
		{
			Value:   1,
			Address: types.Address{123, 106, 229, 101, 220, 252, 50, 203, 38, 183, 133, 152, 250, 167, 210, 155, 252, 102, 150, 29, 187, 3, 178, 53, 11, 145, 143, 33, 166, 115, 250, 40},
		},
		{
			Value:   5,
			Address: types.Address{124, 101, 207, 175, 50, 119, 207, 26, 62, 15, 247, 141, 150, 174, 73, 247, 238, 28, 77, 255, 222, 104, 166, 244, 112, 86, 227, 80, 215, 45, 69, 143},
		},
		{
			Value:   10,
			Address: types.Address{130, 184, 72, 15, 227, 79, 217, 205, 120, 254, 67, 69, 10, 49, 76, 194, 222, 30, 242, 62, 88, 179, 51, 117, 27, 166, 140, 6, 7, 22, 222, 185},
		},
		{
			Value:   25,
			Address: types.Address{134, 137, 198, 172, 96, 54, 45, 10, 100, 128, 91, 225, 226, 134, 143, 108, 31, 70, 187, 228, 54, 212, 70, 229, 149, 57, 64, 166, 153, 123, 238, 180},
		},
		{
			Value:   1,
			Address: types.Address{143, 253, 118, 229, 109, 181, 141, 224, 91, 144, 123, 160, 203, 221, 119, 104, 172, 13, 105, 77, 171, 185, 122, 54, 229, 168, 6, 130, 160, 130, 182, 151},
		},
		{
			Value:   8,
			Address: types.Address{147, 108, 249, 16, 36, 249, 108, 184, 196, 212, 241, 120, 219, 63, 45, 184, 86, 53, 96, 207, 130, 96, 210, 251, 136, 9, 193, 160, 131, 198, 221, 185},
		},
		{
			Value:   58,
			Address: types.Address{155, 79, 89, 28, 69, 71, 239, 198, 246, 2, 198, 254, 92, 59, 192, 205, 229, 152, 36, 186, 110, 122, 233, 221, 76, 143, 3, 238, 89, 231, 192, 23},
		},
		{
			Value:   2,
			Address: types.Address{156, 32, 76, 105, 213, 46, 66, 50, 27, 85, 56, 9, 106, 193, 80, 145, 19, 101, 84, 177, 145, 4, 125, 28, 79, 252, 43, 83, 118, 110, 206, 247},
		},
		{
			Value:   23,
			Address: types.Address{157, 169, 134, 24, 254, 22, 58, 188, 119, 87, 201, 238, 55, 168, 194, 131, 88, 18, 39, 168, 37, 2, 198, 194, 93, 202, 116, 146, 189, 17, 108, 44},
		},
		{
			Value:   10,
			Address: types.Address{158, 51, 104, 36, 242, 114, 67, 16, 168, 230, 4, 111, 241, 72, 5, 14, 182, 102, 169, 156, 144, 220, 103, 117, 223, 8, 58, 187, 124, 102, 80, 44},
		},
		{
			Value:   1,
			Address: types.Address{160, 175, 59, 33, 223, 30, 82, 60, 34, 110, 28, 203, 249, 93, 3, 16, 218, 12, 250, 206, 138, 231, 85, 67, 69, 191, 68, 198, 160, 87, 154, 68},
		},
		{
			Value:   75,
			Address: types.Address{163, 94, 51, 220, 14, 144, 83, 112, 62, 10, 0, 173, 161, 234, 211, 176, 186, 84, 9, 189, 250, 111, 33, 231, 114, 87, 100, 75, 72, 217, 11, 26},
		},
		{
			Value:   3,
			Address: types.Address{170, 7, 138, 116, 205, 20, 132, 197, 166, 251, 75, 93, 69, 6, 109, 244, 212, 119, 173, 114, 34, 18, 25, 21, 111, 203, 203, 253, 138, 104, 27, 36},
		},
		{
			Value:   90,
			Address: types.Address{173, 120, 128, 104, 186, 86, 151, 140, 191, 23, 231, 193, 77, 245, 243, 104, 196, 55, 155, 243, 111, 15, 84, 139, 148, 187, 173, 47, 104, 69, 141, 39},
		},
		{
			Value:   20,
			Address: types.Address{179, 185, 228, 166, 139, 94, 13, 193, 255, 227, 174, 99, 120, 105, 109, 221, 247, 4, 155, 243, 229, 37, 26, 98, 222, 12, 91, 80, 223, 33, 61, 56},
		},
		{
			Value:   5,
			Address: types.Address{193, 49, 103, 20, 170, 135, 182, 85, 149, 18, 159, 194, 152, 120, 162, 208, 49, 158, 220, 188, 114, 79, 1, 131, 62, 27, 86, 57, 244, 46, 64, 66},
		},
		{
			Value:   1,
			Address: types.Address{196, 71, 45, 222, 0, 21, 12, 121, 197, 224, 101, 65, 40, 57, 19, 119, 112, 205, 166, 23, 2, 91, 75, 231, 69, 143, 221, 68, 245, 75, 7, 52},
		},
		{
			Value:   44,
			Address: types.Address{196, 214, 236, 211, 227, 216, 152, 127, 164, 2, 235, 14, 235, 46, 142, 231, 83, 38, 7, 131, 208, 29, 179, 189, 62, 88, 129, 180, 119, 158, 214, 97},
		},
		{
			Value:   23,
			Address: types.Address{206, 58, 114, 148, 131, 49, 87, 197, 86, 18, 216, 26, 62, 79, 152, 175, 33, 4, 132, 160, 108, 231, 53, 200, 48, 76, 125, 94, 156, 85, 32, 130},
		},
		{
			Value:   80,
			Address: types.Address{200, 103, 135, 126, 197, 2, 203, 63, 241, 6, 245, 195, 220, 102, 27, 74, 232, 249, 201, 86, 207, 34, 51, 26, 180, 151, 136, 108, 112, 56, 132, 72},
		},
		{
			Value:   2,
			Address: types.Address{200, 249, 245, 218, 58, 253, 76, 250, 88, 114, 70, 239, 14, 2, 250, 123, 10, 192, 198, 61, 187, 155, 247, 152, 165, 174, 198, 24, 142, 39, 177, 119},
		},
		{
			Value:   1,
			Address: types.Address{209, 1, 199, 184, 186, 57, 21, 137, 33, 252, 219, 184, 130, 38, 32, 98, 63, 252, 250, 79, 70, 146, 169, 78, 180, 161, 29, 93, 38, 45, 175, 176},
		},
		{
			Value:   2,
			Address: types.Address{212, 107, 233, 43, 185, 138, 79, 253, 12, 237, 214, 17, 219, 198, 151, 92, 81, 129, 17, 120, 139, 58, 66, 119, 126, 220, 132, 136, 3, 108, 57, 58},
		},
		{
			Value:   3,
			Address: types.Address{214, 244, 146, 173, 173, 80, 33, 185, 29, 133, 77, 167, 185, 1, 38, 23, 111, 179, 104, 150, 105, 162, 120, 26, 245, 63, 114, 119, 52, 1, 44, 222},
		},
		{
			Value:   1,
			Address: types.Address{217, 218, 172, 16, 53, 134, 160, 226, 44, 138, 93, 53, 181, 62, 4, 209, 190, 27, 0, 93, 105, 17, 169, 61, 98, 145, 131, 112, 121, 55, 97, 184},
		},
		{
			Value:   1,
			Address: types.Address{223, 162, 172, 55, 54, 193, 37, 142, 200, 213, 230, 48, 186, 145, 184, 206, 15, 225, 167, 19, 37, 70, 38, 48, 135, 87, 205, 81, 187, 237, 181, 180},
		},
		{
			Value:   1,
			Address: types.Address{241, 46, 139, 41, 40, 63, 47, 169, 131, 173, 124, 246, 228, 213, 102, 44, 100, 217, 62, 237, 133, 154, 248, 69, 228, 2, 36, 206, 47, 250, 249, 170},
		},
		{
			Value:   50,
			Address: types.Address{241, 50, 229, 211, 66, 32, 115, 241, 117, 87, 180, 239, 76, 246, 14, 129, 105, 181, 153, 105, 105, 203, 229, 237, 23, 130, 193, 170, 100, 201, 38, 71},
		},
		{
			Value:   8841,
			Address: types.Address{125, 12, 68, 247, 102, 78, 45, 52, 229, 62, 253, 224, 102, 26, 111, 98, 142, 201, 38, 71, 133, 174, 142, 60, 215, 201, 115, 232, 209, 144, 195, 201},
		},
	}

	// The genesis timestamp is set to June 6th, because that is when the
	// 100-block developer premine started. The trailing zeroes are a
	// bonus, and make the timestamp easier to memorize.
	GenesisTimestamp = time.Unix(1433600000, 0) // June 6th, 2015 @ 2:13pm UTC.

	// GenesisBlock is the first block of the block chain.
	GenesisBlock = types.Block{
		Timestamp: GenesisTimestamp,
		Transactions: []types.Transaction{
			{
				SiacoinOutputs: GenesisSiacoinAllocation,
				SiafundOutputs: GenesisSiafundAllocation,
			},
		},
	}

	// GenesisID is used in many places. Calculating it once saves lots of
	// redundant computation.
	GenesisID = GenesisBlock.ID()

	// RootDepth is the cumulative target of all blocks. The root depth is
	// essentially the maximum possible target, there have been no blocks yet,
	// so there is no cumulated difficulty yet.
	RootDepth = Target{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}

	// The RootTarget was set such that the developers could reasonable
	// premine 100 blocks in a day. It was known to the developers at launch
	// this this was at least one and perhaps two orders of magnitude too
	// small.
	RootTarget = Target{0, 0, 0, 0, 32}

	// The difficulty adjustment is clamped to 2.5x every 500 blocks. This
	// corresponds to 6.25x every 2 weeks, which can be compared to
	// Bitcoin's clamp of 4x every 2 weeks. The difficulty clamp is
	// primarily to stop difficulty raising attacks. Sia's safety margin is
	// similar to Bitcoin's despite the looser clamp because Sia's
	// difficulty is adjusted four times as often. This does result in
	// greater difficulty oscillation, a tradeoff that was chosen to be
	// acceptable due to Sia's more vulnerable position as an altcoin.
	MaxTargetAdjustmentUp = big.NewRat(25, 10)
	MaxTargetAdjustmentDown = big.NewRat(10, 25)

	// A total target of 67 leading zeroes is chosen because that aligns
	// with the amount of hashrate that we expect to be on the network
	// after the hardfork.
	ASICHardforkTotalTarget = Target{0, 0, 0, 0, 0, 0, 0, 0, 32}

	// The max rise and max drop for the difficulty is kept at 0.4% per
	// block, which means that in 1008 blocks the difficulty can move a
	// maximum of about 55x. This is significant, and means that dramatic
	// hashrate changes can be responded to quickly, while still forcing an
	// attacker to do a significant amount of work in order to execute a
	// difficulty raising attack, and minimizing the chance that an attacker
	// can get lucky and fake a ton of work.
	OakMaxRise = big.NewRat(1004, 1e3)
	OakMaxDrop = big.NewRat(1e3, 1004)

	// InitialFoundationSubsidy is the one-time subsidy sent to the Foundation
	// address upon activation of the hardfork, representing one year's worth of
	// block subsidies.
	InitialFoundationSubsidy = types.HastingsPerSiacoin.Mul64(30e3).Mul64(BlocksPerYear)

	// FoundationSubsidyPerBlock is the amount allocated to the Foundation
	// subsidy per block.
	FoundationSubsidyPerBlock = types.HastingsPerSiacoin.Mul64(30e3)

	// ASICHardforkReplayProtectionPrefix is a byte that prefixes
	// SiacoinInputs and SiafundInputs when calculating SigHashes to protect
	// against replay attacks.
	ASICHardforkReplayProtectionPrefix = []byte{0}

	// FoundationHardforkReplayProtectionPrefix is a byte that prefixes
	// SiacoinInputs and SiafundInputs when calculating SigHashes to protect
	// against replay attacks.
	FoundationHardforkReplayProtectionPrefix = []byte{1}

	// InitialFoundationUnlockHash is the primary Foundation subsidy address. It
	// receives the initial Foundation subsidy. The keys that this address was
	// derived from can also be used to set a new primary and failsafe address.
	InitialFoundationUnlockHash types.Address

	// InitialFoundationFailsafeUnlockHash is the "backup" Foundation address.
	// It does not receive the Foundation subsidy, but its keys can be used to
	// set a new primary and failsafe address. These UnlockConditions should
	// also be subject to a timelock that prevents the failsafe from being used
	// immediately.
	InitialFoundationFailsafeUnlockHash types.Address
)

func init() {
	// Calculate the initial coinbase.
	for _, tx := range GenesisBlock.Transactions {
		for _, sco := range tx.SiacoinOutputs {
			numGenesisSiacoins = numGenesisSiacoins.Add(sco.Value)
		}
	}

	// Parse the Foundation unlock hashes.
	InitialFoundationUnlockHash, _ = types.ParseAddress("053b2def3cbdd078c19d62ce2b4f0b1a3c5e0ffbeeff01280efb1f8969b2f5bb4fdc680f0807")
	InitialFoundationFailsafeUnlockHash, _ = types.ParseAddress("27c22a6c6e6645802a3b8fa0e5374657438ef12716d2205d3e866272de1b644dbabd53d6d560")
}
