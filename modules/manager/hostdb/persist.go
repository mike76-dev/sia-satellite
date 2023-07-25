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
	entry.Settings.Collateral.EncodeTo(e)
	entry.Settings.MaxCollateral.EncodeTo(e)
	entry.Settings.BaseRPCPrice.EncodeTo(e)
	entry.Settings.ContractPrice.EncodeTo(e)
	entry.Settings.DownloadBandwidthPrice.EncodeTo(e)
	entry.Settings.SectorAccessPrice.EncodeTo(e)
	entry.Settings.StoragePrice.EncodeTo(e)
	entry.Settings.UploadBandwidthPrice.EncodeTo(e)
	e.WriteUint64(uint64(entry.Settings.EphemeralAccountExpiry))
	entry.Settings.MaxEphemeralAccountBalance.EncodeTo(e)
	e.WriteUint64(entry.Settings.RevisionNumber)
	e.WriteString(entry.Settings.Version)
	e.WriteString(entry.Settings.SiaMuxPort)

	// Price table.
	e.Write(entry.PriceTable.UID[:])
	e.WriteUint64(uint64(entry.PriceTable.Validity))
	e.WriteUint64(entry.PriceTable.HostBlockHeight)
	entry.PriceTable.UpdatePriceTableCost.EncodeTo(e)
	entry.PriceTable.AccountBalanceCost.EncodeTo(e)
	entry.PriceTable.FundAccountCost.EncodeTo(e)
	entry.PriceTable.LatestRevisionCost.EncodeTo(e)
	entry.PriceTable.SubscriptionMemoryCost.EncodeTo(e)
	entry.PriceTable.SubscriptionNotificationCost.EncodeTo(e)
	entry.PriceTable.InitBaseCost.EncodeTo(e)
	entry.PriceTable.MemoryTimeCost.EncodeTo(e)
	entry.PriceTable.DownloadBandwidthCost.EncodeTo(e)
	entry.PriceTable.UploadBandwidthCost.EncodeTo(e)
	entry.PriceTable.DropSectorsBaseCost.EncodeTo(e)
	entry.PriceTable.DropSectorsUnitCost.EncodeTo(e)
	entry.PriceTable.HasSectorBaseCost.EncodeTo(e)
	entry.PriceTable.ReadBaseCost.EncodeTo(e)
	entry.PriceTable.ReadLengthCost.EncodeTo(e)
	entry.PriceTable.RenewContractCost.EncodeTo(e)
	entry.PriceTable.RevisionBaseCost.EncodeTo(e)
	entry.PriceTable.SwapSectorBaseCost.EncodeTo(e)
	entry.PriceTable.WriteBaseCost.EncodeTo(e)
	entry.PriceTable.WriteLengthCost.EncodeTo(e)
	entry.PriceTable.WriteStoreCost.EncodeTo(e)
	entry.PriceTable.TxnFeeMinRecommended.EncodeTo(e)
	entry.PriceTable.TxnFeeMaxRecommended.EncodeTo(e)
	entry.PriceTable.ContractPrice.EncodeTo(e)
	entry.PriceTable.CollateralCost.EncodeTo(e)
	entry.PriceTable.MaxCollateral.EncodeTo(e)
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
	entry.Settings.Collateral.DecodeFrom(d)
	entry.Settings.MaxCollateral.DecodeFrom(d)
	entry.Settings.BaseRPCPrice.DecodeFrom(d)
	entry.Settings.ContractPrice.DecodeFrom(d)
	entry.Settings.DownloadBandwidthPrice.DecodeFrom(d)
	entry.Settings.SectorAccessPrice.DecodeFrom(d)
	entry.Settings.StoragePrice.DecodeFrom(d)
	entry.Settings.UploadBandwidthPrice.DecodeFrom(d)
	entry.Settings.EphemeralAccountExpiry = time.Duration(d.ReadUint64())
	entry.Settings.MaxEphemeralAccountBalance.DecodeFrom(d)
	entry.Settings.RevisionNumber = d.ReadUint64()
	entry.Settings.Version = d.ReadString()
	entry.Settings.SiaMuxPort = d.ReadString()

	// Price table.
	d.Read(entry.PriceTable.UID[:])
	entry.PriceTable.Validity = time.Duration(d.ReadUint64())
	entry.PriceTable.HostBlockHeight = d.ReadUint64()
	entry.PriceTable.UpdatePriceTableCost.DecodeFrom(d)
	entry.PriceTable.AccountBalanceCost.DecodeFrom(d)
	entry.PriceTable.FundAccountCost.DecodeFrom(d)
	entry.PriceTable.LatestRevisionCost.DecodeFrom(d)
	entry.PriceTable.SubscriptionMemoryCost.DecodeFrom(d)
	entry.PriceTable.SubscriptionNotificationCost.DecodeFrom(d)
	entry.PriceTable.InitBaseCost.DecodeFrom(d)
	entry.PriceTable.MemoryTimeCost.DecodeFrom(d)
	entry.PriceTable.DownloadBandwidthCost.DecodeFrom(d)
	entry.PriceTable.UploadBandwidthCost.DecodeFrom(d)
	entry.PriceTable.DropSectorsBaseCost.DecodeFrom(d)
	entry.PriceTable.DropSectorsUnitCost.DecodeFrom(d)
	entry.PriceTable.HasSectorBaseCost.DecodeFrom(d)
	entry.PriceTable.ReadBaseCost.DecodeFrom(d)
	entry.PriceTable.ReadLengthCost.DecodeFrom(d)
	entry.PriceTable.RenewContractCost.DecodeFrom(d)
	entry.PriceTable.RevisionBaseCost.DecodeFrom(d)
	entry.PriceTable.SwapSectorBaseCost.DecodeFrom(d)
	entry.PriceTable.WriteBaseCost.DecodeFrom(d)
	entry.PriceTable.WriteLengthCost.DecodeFrom(d)
	entry.PriceTable.WriteStoreCost.DecodeFrom(d)
	entry.PriceTable.TxnFeeMinRecommended.DecodeFrom(d)
	entry.PriceTable.TxnFeeMaxRecommended.DecodeFrom(d)
	entry.PriceTable.ContractPrice.DecodeFrom(d)
	entry.PriceTable.CollateralCost.DecodeFrom(d)
	entry.PriceTable.MaxCollateral.DecodeFrom(d)
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
		hdb.filteredTree = hosttree.New(hdb.scoreFunc, hdb.staticLog)
	}

	// "Lazily" load the hosts into the host trees.
	go hdb.threadedLoadHosts()

	return nil
}
