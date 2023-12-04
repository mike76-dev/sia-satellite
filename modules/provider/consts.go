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
	updateRevisionTime = 15 * time.Second

	// formContractTime defines the amount of time that the provider
	// has to form a single contract with a host.
	formContractTime = 1 * time.Minute

	// renewContractTime defines the amount of time that the provider
	// has to renew a contract with a host.
	renewContractTime = 1 * time.Minute

	// settingsTime defines the amount of time that the provider has to
	// send or receive renter's settings.
	settingsTime = 15 * time.Second

	// saveMetadataTime defines the amount of time that the provider has
	// to accept the file metadata and save it.
	saveMetadataTime = 1 * time.Minute

	// requestMetadataTime defines the amount of time that the provider has
	// to retrieve the file metadata and send it.
	requestMetadataTime = 1 * time.Minute

	// requestMetadataBatchThreshold is the number of slabs to consider
	// a batch.
	requestMetadataBatchThreshold = 256

	// updateSlabTime defines the amount of time that the provider has to
	// update a single slab.
	updateSlabTime = 15 * time.Second

	// requestSlabsTime defines the amount of time that the provider has to
	// retrieve modified slabs and send them.
	requestSlabsTime = 1 * time.Minute

	// shareContractsTime defines the amount of time that the provider has to
	// accept a set of contracts from the renter.
	shareContractsTime = 1 * time.Minute

	// registerMultipartTime defines the amount of time that the provider has to
	// register a multipart upload.
	registerMultipartTime = 15 * time.Second

	// deleteMultipartTime defines the amount of time that the provider has to
	// delete an aborted multipart upload.
	deleteMultipartTime = 1 * time.Minute

	// completeMultipartTime defines the amount of time that the provider has to
	// complete a multipart upload.
	completeMultipartTime = 1 * time.Minute

	// defaultConnectionDeadline is the default read and write deadline which is set
	// on a connection. This ensures it times out if I/O exceeds this deadline.
	defaultConnectionDeadline = 5 * time.Minute

	// defaultStreamDeadline is the default read and write deadline which is set
	// on a stream. The RPCs may extend that.
	defaultStreamDeadline = 30 * time.Second

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

	// saveMetadataSpecifier is used to save the file metadata.
	saveMetadataSpecifier = types.NewSpecifier("SaveMetadata")

	// requestMetadataSpecifier is used to retrieve the file metadata.
	requestMetadataSpecifier = types.NewSpecifier("RequestMetadata")

	// updateSlabSpecifier is used to update a single slab.
	updateSlabSpecifier = types.NewSpecifier("UpdateSlab")

	// requestSlabsSpecifier is used to retrieve modified slabs.
	requestSlabsSpecifier = types.NewSpecifier("RequestSlabs")

	// shareContractsSpecifier is used when a renter shares their contract set.
	shareContractsSpecifier = types.NewSpecifier("ShareContracts")

	// uploadFileSpecifier is used when a renter wants to upload a file.
	uploadFileSpecifier = types.NewSpecifier("UploadFile")

	// registerMultipartSpecifier is used when a new multipart upload is created.
	registerMultipartSpecifier = types.NewSpecifier("CreateMultipart")

	// deleteMultipartSpecifier is used when a multipart upload is aborted.
	deleteMultipartSpecifier = types.NewSpecifier("AbortMultipart")

	// uploadPartSpecifier is used when a renter wants to upload a part
	// of an S3 multipart upload.
	uploadPartSpecifier = types.NewSpecifier("UploadPart")

	// completeMultipartSpecifier is used when a multipart upload is completed.
	completeMultipartSpecifier = types.NewSpecifier("FinishMultipart")
)
