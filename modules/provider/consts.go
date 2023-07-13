package provider

import (
	"time"

	"go.sia.tech/core/types"
)

const (
	// requestContractsTime defines the amount of time that the provider
	// has to send the list of the active contracts.
	requestContractsTime = 1 * time.Minute

	// formContractsTime defines the amount of time that the provider
	// has to form contracts with the hosts.
	formContractsTime = 10 * time.Minute

	// renewContractsTime defines the amount of time that the provider
	// has to renew a set of contracts.
	renewContractsTime = 10 * time.Minute

	// updateRevisionTime defines the amount of time that the provider
	// has to update a contract and send back a response.
	updateRevisionTime = 1 * time.Minute

	// formContractTime defines the amount of time that the provider
	// has to form a single contract with a host.
	formContractTime = 1 * time.Minute

	// renewContractTime defines the amount of time that the provider
	// has to renew a contract with a host.
	renewContractTime = 1 * time.Minute

	// settingsTime defines the amount of time that the provider has to
	// send or receive renter's settings.
	settingsTime = 1 * time.Minute

	// defaultConnectionDeadline is the default read and write deadline which is set
	// on a connection. This ensures it times out if I/O exceeds this deadline.
	defaultConnectionDeadline = 5 * time.Minute

	// rpcRatelimit prevents someone from spamming the provider connections,
	// causing it to spin up enough goroutines to crash.
	rpcRatelimit = time.Millisecond * 50
)

var (
	// requestContractsSpecifier is used when a renter requests the list of their
	// active contracts.
	requestContractsSpecifier = types.NewSpecifier("RequestContracts")

	// formContractsSpecifier is used when a renter requests to form a number of
	// contracts on their behalf.
	formContractsSpecifier = types.NewSpecifier("FormContracts")

	// renewContractsSpecifier is used when a renter requests to renew a set of
	// contracts.
	renewContractsSpecifier = types.NewSpecifier("RenewContracts")

	// updateRevisionSpecifier is used when a renter submits a new revision.
	updateRevisionSpecifier = types.NewSpecifier("UpdateRevision")

	// formContractSpecifier is used to form a single contract using the new
	// Renter-Satellite protocol.
	formContractSpecifier = types.NewSpecifier("FormContract")

	// renewContractSpecifier is used to renew a contract using the new
	// Renter-Satellite protocol.
	renewContractSpecifier = types.NewSpecifier("RenewContract")

	// getSettingsSpecifier is used to request the renter's opt-in settings.
	getSettingsSpecifier = types.NewSpecifier("GetSettings")

	// updateSettingsSpecifier is used to update the renter's opt-in settings.
	updateSettingsSpecifier = types.NewSpecifier("UpdateSettings")
)