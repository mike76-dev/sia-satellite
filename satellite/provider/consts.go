package provider

import (
	"time"
)

// reauestContractsTime defines the amount of time that the provider
// has to send the list of the active contracts.
const requestContractsTime = 1 * time.Minute

// formContractsTime defines the amount of time that the provider
// has to form contracts with the hosts.
const formContractsTime = 10 * time.Minute

// renewContractsTime defines the amount of time that the provider
// has to renew a set of contracts.
const renewContractsTime = 10 * time.Minute

// updateRevisionTime defines the amount of time that the provider
// has to update a contract and send back a response.
const updateRevisionTime = 1 * time.Minute

// formContractTime defines the amount of time that the provider
// has to form a single contract with a host.
const formContractTime = 1 * time.Minute

// renewContractTime defines the amount of time that the provider
// has to renew a contract with a host.
const renewContractTime = 1 * time.Minute
