package contractor

import (
	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
)

const (
	// randomHostsBufferForScore defines how many extra hosts are queried when trying
	// to figure out an appropriate minimum score for the hosts that we have.
	randomHostsBufferForScore = 50
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

	// minContractFundUploadThreshold is the percentage of contract funds
	// remaining at which the contract gets marked !GoodForUpload. The number is
	// high so that there is plenty of money available for downloading, so that
	// urgent repairs can be performed and also so that user file access is not
	// interrupted until after uploading progress is interrupted. Structuring
	// things this way essentially allows the user to experience the failure
	// mode of 'can't store additional stuff' before the user experiences the
	// failure mode of 'can't retrieve stuff already uploaded'.
	minContractFundUploadThreshold = float64(0.05) // 5%

	// oosRetryInterval is the time we wait for a host that ran out of storage to
	// add more storage before trying to upload to it again.
	oosRetryInterval = modules.BlocksPerWeek // 7 days
)
