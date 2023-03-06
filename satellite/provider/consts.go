package provider

import (
	"time"
)

// formContractsTime defines the amount of time that the provider
// has to form contracts with the hosts.
const formContractsTime = 10 * time.Minute

// renewContractsTime defines the amount of time that the provider
// has to renew a set of contracts.
const renewContractsTime = 10 * time.Minute
