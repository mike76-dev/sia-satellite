package contractor

import (
	"errors"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

const (
	// randomHostsBufferForScore defines how many extra hosts are queried when trying
	// to figure out an appropriate minimum score for the hosts that we have.
	randomHostsBufferForScore = 50

	// formContractTimeout is the amount of time we wait to form a contract
	// with the host.
	formContractTimeout = 1 * time.Minute

	// renewContractTimeout is the amount of time we wait to renew a contract.
	renewContractTimeout = 1 * time.Minute

	// fundAccountTimeout is the amount of time we wait to fund an ephemeral account.
	fundAccountTimeout = 10 * time.Second
)

// Constants related to the contractor's alerts.
const (
	// AlertCauseInsufficientAllowanceFunds indicates that the cause for the
	// alert was insufficient allowance funds remaining.
	AlertCauseInsufficientAllowanceFunds = "Insufficient allowance funds remaining"

	// AlertMSGAllowanceLowFunds indicates that forming/renewing a contract during
	// contract maintenance isn't possible due to the allowance being low on
	// funds.
	AlertMSGAllowanceLowFunds = "At least one contract formation/renewal failed due to the allowance being low on funds"

	// AlertMSGFailedContractRenewal indicates that the contract renewal failed.
	AlertMSGFailedContractRenewal = "Contractor is attempting to renew/refresh contracts but failed"

	// AlertMSGWalletLockedDuringMaintenance indicates that forming/renewing a
	// contract during contract maintenance isn't possible due to a locked wallet.
	AlertMSGWalletLockedDuringMaintenance = "At least one contract failed to form/renew due to the wallet being locked"
)

// Constants related to contract formation parameters.
const (
	// ContractFeeFundingMulFactor is the multiplying factor for contract fees
	// to determine the funding for a new contract.
	ContractFeeFundingMulFactor = uint64(10)

	// MaxInitialContractFundingDivFactor is the dividing factor for determining
	// the maximum amount of funds to put into a new contract.
	MaxInitialContractFundingDivFactor = uint64(3)

	// MaxInitialContractFundingMulFactor is the multiplying factor for
	// determining the maximum amount of funds to put into a new contract.
	MaxInitialContractFundingMulFactor = uint64(2)

	// MinInitialContractFundingDivFactor is the dividing factor for determining
	// the minimum amount of funds to put into a new contract.
	MinInitialContractFundingDivFactor = uint64(20)

	// fileContractMinimumFunding is the lowest percentage of an allowace (on a
	// per-contract basis) that is allowed to go into funding a contract. If the
	// allowance is 100 SC per contract (5,000 SC total for 50 contracts, or
	// 2,000 SC total for 20 contracts, etc.), then the minimum amount of funds
	// that a contract would be allowed to have is fileContractMinimumFunding *
	// 100SC.
	fileContractMinimumFunding = float64(0.15)

	// minimumSupportedRenterHostProtocolVersion is the minimum version of Sia
	// that supports the currently used version of the renter-host protocol.
	minimumSupportedRenterHostProtocolVersion = "1.4.1"

	// hostBufferForRenewals is how many more hosts may be in the renter's
	// contract set than required by the allowance. If there are more hosts,
	// extra contracts stop being renewed.
	hostBufferForRenewals = 3
)

var (
	// scoreLeewayGoodForRenew defines the factor by which a host can miss the
	// goal score for a set of hosts and still be GoodForRenew. To determine the
	// goal score, a new set of hosts is queried from the hostdb and the lowest
	// scoring among them is selected.  That score is then divided by
	// scoreLeewayGoodForRenew to get the minimum score that a host is allowed
	// to have before being marked as !GoodForRenew.
	//
	// TODO: At this point in time, this value is somewhat arbitrary and could
	// be getting set in a lot more scientific way.
	scoreLeewayGoodForRenew = types.NewCurrency64(500)

	// scoreLeewayGoodForUpload defines the factor by which a host can miss the
	// goal score for a set of hosts and still be GoodForUpload. To determine the
	// goal score, a new set of hosts is queried from the hostdb and the lowest
	// scoring among them is selected.  That score is then divided by
	// scoreLeewayGoodForUpload to get the minimum score that a host is allowed
	// to have before being marked as !GoodForUpload.
	//
	// Hosts are marked !GoodForUpload before they are marked !GoodForRenew
	// because churn can harm the health and scalability of a user's filesystem.
	// Switching away from adding new files to a host can minimize the damage of
	// using a bad host without incurring data churn.
	//
	// TODO: At this point in time, this value is somewhat arbitrary and could
	// be getting set in a lot more scientific way.
	scoreLeewayGoodForUpload = types.NewCurrency64(40)

	// consecutiveRenewalsBeforeReplacement is the number of times a contract
	// attempt to be renewed before it is marked as !goodForRenew.
	consecutiveRenewalsBeforeReplacement = uint64(12) // ~2h

	// minContractFundUploadThreshold is the percentage of contract funds
	// remaining at which the contract gets marked !GoodForUpload. The number is
	// high so that there is plenty of money available for downloading, so that
	// urgent repairs can be performed and also so that user file access is not
	// interrupted until after uploading progress is interrupted. Structuring
	// things this way essentially allows the user to experience the failure
	// mode of 'can't store additional stuff' before the user experiences the
	// failure mode of 'can't retrieve stuff already uploaded'.
	minContractFundUploadThreshold = float64(0.05) // 5%

	// minContractFundRenewalThreshold defines the ratio of remaining funds to
	// total contract cost below which the contractor will prematurely renew a
	// contract.
	//
	// This number is deliberately a little higher than the
	// minContractFundUploadThreshold because we want to make sure that renewals
	// will kick in before uploading stops.
	minContractFundRenewalThreshold = float64(0.06) // 6%

	// oosRetryInterval is the time we wait for a host that ran out of storage to
	// add more storage before trying to upload to it again.
	oosRetryInterval = modules.BlocksPerWeek // 7 days

	// errHostFault indicates if an error is the host's fault.
	errHostFault = errors.New("host has returned an error")

	// errDuplicateTransactionSet is the error that gets returned if a
	// duplicate transaction set is given to the transaction pool.
	errDuplicateTransactionSet = errors.New("transaction set contains only duplicate transactions")
)
