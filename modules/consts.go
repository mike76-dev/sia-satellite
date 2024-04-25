package modules

import (
	"go.sia.tech/core/types"
)

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

var (
	// BlockBytesPerMonthTerabyte is the conversion rate between block-bytes and month-TB.
	BlockBytesPerMonthTerabyte = types.NewCurrency64(BytesPerTerabyte).Mul64(BlocksPerMonth)
)
