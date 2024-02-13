package hostdb

import (
	"math"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/hostdb/hosttree"

	"go.sia.tech/core/types"
)

// encodeHostEntry encodes a modules.HostDBEntry.
func encodeHostEntry(entry *modules.HostDBEntry, e *types.Encoder) {
	// Settings.
	e.WriteBool(entry.Settings.AcceptingContracts)
	e.WriteUint64(entry.Settings.MaxDownloadBatchSize)
	e.WriteUint64(entry.Settings.MaxDuration)
	e.WriteUint64(entry.Settings.MaxReviseBatchSize)
	e.WriteString(entry.Settings.NetAddress)
	e.WriteUint64(entry.Settings.RemainingStorage)
	e.WriteUint64(entry.Settings.SectorSize)
	e.WriteUint64(entry.Settings.TotalStorage)
	entry.Settings.Address.EncodeTo(e)
	e.WriteUint64(entry.Settings.WindowSize)
	types.V1Currency(entry.Settings.Collateral).EncodeTo(e)
	types.V1Currency(entry.Settings.MaxCollateral).EncodeTo(e)
	types.V1Currency(entry.Settings.BaseRPCPrice).EncodeTo(e)
	types.V1Currency(entry.Settings.ContractPrice).EncodeTo(e)
	types.V1Currency(entry.Settings.DownloadBandwidthPrice).EncodeTo(e)
	types.V1Currency(entry.Settings.SectorAccessPrice).EncodeTo(e)
	types.V1Currency(entry.Settings.StoragePrice).EncodeTo(e)
	types.V1Currency(entry.Settings.UploadBandwidthPrice).EncodeTo(e)
	e.WriteUint64(uint64(entry.Settings.EphemeralAccountExpiry))
	types.V1Currency(entry.Settings.MaxEphemeralAccountBalance).EncodeTo(e)
	e.WriteUint64(entry.Settings.RevisionNumber)
	e.WriteString(entry.Settings.Version)
	e.WriteString(entry.Settings.SiaMuxPort)

	// Price table.
	e.Write(entry.PriceTable.UID[:])
	e.WriteUint64(uint64(entry.PriceTable.Validity))
	e.WriteUint64(entry.PriceTable.HostBlockHeight)
	types.V1Currency(entry.PriceTable.UpdatePriceTableCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.AccountBalanceCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.FundAccountCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.LatestRevisionCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.SubscriptionMemoryCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.SubscriptionNotificationCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.InitBaseCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.MemoryTimeCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.DownloadBandwidthCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.UploadBandwidthCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.DropSectorsBaseCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.DropSectorsUnitCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.HasSectorBaseCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.ReadBaseCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.ReadLengthCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.RenewContractCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.RevisionBaseCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.SwapSectorBaseCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.WriteBaseCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.WriteLengthCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.WriteStoreCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.TxnFeeMinRecommended).EncodeTo(e)
	types.V1Currency(entry.PriceTable.TxnFeeMaxRecommended).EncodeTo(e)
	types.V1Currency(entry.PriceTable.ContractPrice).EncodeTo(e)
	types.V1Currency(entry.PriceTable.CollateralCost).EncodeTo(e)
	types.V1Currency(entry.PriceTable.MaxCollateral).EncodeTo(e)
	e.WriteUint64(entry.PriceTable.MaxDuration)
	e.WriteUint64(entry.PriceTable.WindowSize)
	e.WriteUint64(entry.PriceTable.RegistryEntriesLeft)
	e.WriteUint64(entry.PriceTable.RegistryEntriesTotal)

	// Other fields.
	e.WriteUint64(entry.FirstSeen)
	e.WriteUint64(entry.LastAnnouncement)
	e.WriteUint64(uint64(entry.HistoricDowntime))
	e.WriteUint64(uint64(entry.HistoricUptime))
	e.WriteUint64(math.Float64bits(entry.HistoricFailedInteractions))
	e.WriteUint64(math.Float64bits(entry.HistoricSuccessfulInteractions))
	e.WriteUint64(math.Float64bits(entry.RecentFailedInteractions))
	e.WriteUint64(math.Float64bits(entry.RecentSuccessfulInteractions))
	e.WriteUint64(entry.LastHistoricUpdate)
	e.WriteUint64(uint64(entry.LastIPNetChange.Unix()))
}

// decodeHostEntry decodes a modules.HostDBEntry.
func decodeHostEntry(entry *modules.HostDBEntry, d *types.Decoder) {
	// Settings.
	entry.Settings.AcceptingContracts = d.ReadBool()
	entry.Settings.MaxDownloadBatchSize = d.ReadUint64()
	entry.Settings.MaxDuration = d.ReadUint64()
	entry.Settings.MaxReviseBatchSize = d.ReadUint64()
	entry.Settings.NetAddress = d.ReadString()
	entry.Settings.RemainingStorage = d.ReadUint64()
	entry.Settings.SectorSize = d.ReadUint64()
	entry.Settings.TotalStorage = d.ReadUint64()
	entry.Settings.Address.DecodeFrom(d)
	entry.Settings.WindowSize = d.ReadUint64()
	(*types.V1Currency)(&entry.Settings.Collateral).DecodeFrom(d)
	(*types.V1Currency)(&entry.Settings.MaxCollateral).DecodeFrom(d)
	(*types.V1Currency)(&entry.Settings.BaseRPCPrice).DecodeFrom(d)
	(*types.V1Currency)(&entry.Settings.ContractPrice).DecodeFrom(d)
	(*types.V1Currency)(&entry.Settings.DownloadBandwidthPrice).DecodeFrom(d)
	(*types.V1Currency)(&entry.Settings.SectorAccessPrice).DecodeFrom(d)
	(*types.V1Currency)(&entry.Settings.StoragePrice).DecodeFrom(d)
	(*types.V1Currency)(&entry.Settings.UploadBandwidthPrice).DecodeFrom(d)
	entry.Settings.EphemeralAccountExpiry = time.Duration(d.ReadUint64())
	(*types.V1Currency)(&entry.Settings.MaxEphemeralAccountBalance).DecodeFrom(d)
	entry.Settings.RevisionNumber = d.ReadUint64()
	entry.Settings.Version = d.ReadString()
	entry.Settings.SiaMuxPort = d.ReadString()

	// Price table.
	d.Read(entry.PriceTable.UID[:])
	entry.PriceTable.Validity = time.Duration(d.ReadUint64())
	entry.PriceTable.HostBlockHeight = d.ReadUint64()
	(*types.V1Currency)(&entry.PriceTable.UpdatePriceTableCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.AccountBalanceCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.FundAccountCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.LatestRevisionCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.SubscriptionMemoryCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.SubscriptionNotificationCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.InitBaseCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.MemoryTimeCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.DownloadBandwidthCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.UploadBandwidthCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.DropSectorsBaseCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.DropSectorsUnitCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.HasSectorBaseCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.ReadBaseCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.ReadLengthCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.RenewContractCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.RevisionBaseCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.SwapSectorBaseCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.WriteBaseCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.WriteLengthCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.WriteStoreCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.TxnFeeMinRecommended).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.TxnFeeMaxRecommended).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.ContractPrice).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.CollateralCost).DecodeFrom(d)
	(*types.V1Currency)(&entry.PriceTable.MaxCollateral).DecodeFrom(d)
	entry.PriceTable.MaxDuration = d.ReadUint64()
	entry.PriceTable.WindowSize = d.ReadUint64()
	entry.PriceTable.RegistryEntriesLeft = d.ReadUint64()
	entry.PriceTable.RegistryEntriesTotal = d.ReadUint64()

	// Other fields.
	entry.FirstSeen = d.ReadUint64()
	entry.LastAnnouncement = d.ReadUint64()
	entry.HistoricDowntime = time.Duration(d.ReadUint64())
	entry.HistoricUptime = time.Duration(d.ReadUint64())
	entry.HistoricFailedInteractions = math.Float64frombits(d.ReadUint64())
	entry.HistoricSuccessfulInteractions = math.Float64frombits(d.ReadUint64())
	entry.RecentFailedInteractions = math.Float64frombits(d.ReadUint64())
	entry.RecentSuccessfulInteractions = math.Float64frombits(d.ReadUint64())
	entry.LastHistoricUpdate = d.ReadUint64()
	entry.LastIPNetChange = time.Unix(int64(d.ReadUint64()), 0)
}

// load loads the hostdb persistence data from disk.
func (hdb *HostDB) load() error {
	// Load the HostDB data.
	err := hdb.loadDB()
	if err != nil {
		return err
	}

	if len(hdb.filteredHosts) > 0 {
		hdb.filteredTree = hosttree.New(hdb.scoreFunc, hdb.log)
	}

	// "Lazily" load the hosts into the host trees.
	go hdb.threadedLoadHosts()

	return nil
}
