package modules

import (
	"errors"
	"fmt"
	"math/bits"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	core "go.sia.tech/core/types"
	smodules "go.sia.tech/siad/modules"
	"go.sia.tech/siad/types"
)

// CheckGouging performs a number of gouging checks before forming
// a contract with the host.
func CheckGouging(a Allowance, height types.BlockHeight, hes *smodules.HostExternalSettings, pt *rhpv3.HostPriceTable, txnFee types.Currency) (err error) {
	if hes == nil && pt == nil {
		return errors.New("either host settings or price table must be not nil")
	}

	// Host settings checks.
	if hes != nil {
		if err = checkDownloadGougingRHPv2(a, *hes); err != nil {
			return
		}
		if err = checkPriceGougingHS(a, *hes); err != nil {
			return
		}
		if err = checkUploadGougingRHPv2(a, *hes); err != nil {
			return
		}
	}

	// Price table checks.
	if pt != nil {
		if err = checkDownloadGougingRHPv3(a, *pt); err != nil {
			return
		}
		if err = checkPriceGougingPT(a, height, txnFee, *pt); err != nil {
			return
		}
		if err = checkUploadGougingRHPv3(a, *pt); err != nil {
			return
		}
		if err = checkContractGougingPT(a, *pt); err != nil {
			return
		}
	}

	return nil
}

// checkPriceGougingHS checks the host settings.
func checkPriceGougingHS(a Allowance, hes smodules.HostExternalSettings) error {
	// Check base RPC price.
	if !a.MaxRPCPrice.IsZero() && hes.BaseRPCPrice.Cmp(a.MaxRPCPrice) > 0 {
		return fmt.Errorf("rpc price exceeds max: %v>%v", ConvertCurrency(hes.BaseRPCPrice), ConvertCurrency(a.MaxRPCPrice))
	}

	// Check max storage price.
	if !a.MaxStoragePrice.IsZero() && hes.StoragePrice.Cmp(a.MaxStoragePrice) > 0 {
		return fmt.Errorf("storage price exceeds max: %v>%v", ConvertCurrency(hes.StoragePrice), ConvertCurrency(a.MaxStoragePrice))
	}

	// Check contract price.
	if !a.MaxContractPrice.IsZero() && hes.ContractPrice.Cmp(a.MaxContractPrice) > 0 {
		return fmt.Errorf("contract price exceeds max: %v>%v", ConvertCurrency(hes.ContractPrice), ConvertCurrency(a.MaxContractPrice))
	}

	// Check max collateral.
	if hes.MaxCollateral.IsZero() {
		return errors.New("MaxCollateral of the host is 0")
	}
	if hes.MaxCollateral.Cmp(a.MinMaxCollateral) < 0 {
		return fmt.Errorf("MaxCollateral is below minimum: %v<%v", ConvertCurrency(hes.MaxCollateral), ConvertCurrency(a.MinMaxCollateral))
	}

	return nil
}

// checkContractGougingPT checks the price table.
func checkContractGougingPT(a Allowance, pt rhpv3.HostPriceTable) error {
	// Check MaxDuration.
	if a.Period != 0 && a.Period > types.BlockHeight(pt.MaxDuration) {
		return fmt.Errorf("MaxDuration %v is lower than the period %v", pt.MaxDuration, a.Period)
	}

	// Check WindowSize.
	if a.RenewWindow != 0 && a.RenewWindow < types.BlockHeight(pt.WindowSize) {
		return fmt.Errorf("minimum WindowSize %v is greater than the renew window %v", pt.WindowSize, a.RenewWindow)
	}

	return nil
}

// checkPriceGougingPT checks the price table.
func checkPriceGougingPT(a Allowance, height types.BlockHeight, txnFee types.Currency, pt rhpv3.HostPriceTable) error {
	// Check base RPC price.
	if !a.MaxRPCPrice.IsZero() && a.MaxRPCPrice.Cmp(types.NewCurrency(pt.InitBaseCost.Big())) < 0 {
		return fmt.Errorf("init base cost exceeds max: %v>%v", pt.InitBaseCost, ConvertCurrency(a.MaxRPCPrice))
	}

	// Check contract price.
	if !a.MaxContractPrice.IsZero() && pt.ContractPrice.Cmp(ConvertCurrency(a.MaxContractPrice)) > 0 {
		return fmt.Errorf("contract price exceeds max: %v>%v", pt.ContractPrice, ConvertCurrency(a.MaxContractPrice))
	}

	// Check storage price.
	if !a.MaxStoragePrice.IsZero() && pt.WriteStoreCost.Cmp(ConvertCurrency(a.MaxStoragePrice)) > 0 {
		return fmt.Errorf("storage price exceeds max: %v>%v", pt.WriteStoreCost, ConvertCurrency(a.MaxStoragePrice))
	}

	// Check max collateral.
	if pt.MaxCollateral.IsZero() {
		return errors.New("MaxCollateral of host is 0")
	}
	if pt.MaxCollateral.Cmp(ConvertCurrency(a.MinMaxCollateral)) < 0 {
		return fmt.Errorf("MaxCollateral is below minimum: %v<%v", pt.MaxCollateral, ConvertCurrency(a.MinMaxCollateral))
	}

	// Check ReadLengthCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.ReadLengthCost) < 0 {
		return fmt.Errorf("ReadLengthCost of %v exceeds 1H", pt.ReadLengthCost)
	}

	// Check WriteLengthCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.WriteLengthCost) < 0 {
		return fmt.Errorf("WriteLengthCost of %v exceeds 1H", pt.WriteLengthCost)
	}

	// Check AccountBalanceCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.AccountBalanceCost) < 0 {
		return fmt.Errorf("AccountBalanceCost of %v exceeds 1H", pt.AccountBalanceCost)
	}

	// Check FundAccountCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.FundAccountCost) < 0 {
		return fmt.Errorf("FundAccountCost of %v exceeds 1H", pt.FundAccountCost)
	}

	// Check UpdatePriceTableCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.UpdatePriceTableCost) < 0 {
		return fmt.Errorf("UpdatePriceTableCost of %v exceeds 1H", pt.UpdatePriceTableCost)
	}

	// Check HasSectorBaseCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.HasSectorBaseCost) < 0 {
		return fmt.Errorf("HasSectorBaseCost of %v exceeds 1H", pt.HasSectorBaseCost)
	}

	// Check MemoryTimeCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.MemoryTimeCost) < 0 {
		return fmt.Errorf("MemoryTimeCost of %v exceeds 1H", pt.MemoryTimeCost)
	}

	// Check DropSectorsBaseCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.DropSectorsBaseCost) < 0 {
		return fmt.Errorf("DropSectorsBaseCost of %v exceeds 1H", pt.DropSectorsBaseCost)
	}

	// Check DropSectorsUnitCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.DropSectorsUnitCost) < 0 {
		return fmt.Errorf("DropSectorsUnitCost of %v exceeds 1H", pt.DropSectorsUnitCost)
	}

	// Check SwapSectorCost - should be 1H as it's unused by hosts.
	if core.NewCurrency64(1).Cmp(pt.SwapSectorCost().Base) < 0 {
		return fmt.Errorf("SwapSectorBaseCost of %v exceeds 1H", pt.SwapSectorCost().Base)
	}

	// Check SubscriptionMemoryCost - expect 1H default.
	if core.NewCurrency64(1).Cmp(pt.SubscriptionMemoryCost) < 0 {
		return fmt.Errorf("SubscriptionMemoryCost of %v exceeds 1H", pt.SubscriptionMemoryCost)
	}

	// Check SubscriptionNotificationCost - expect 1H default.
	if core.NewCurrency64(1).Cmp(pt.SubscriptionNotificationCost) < 0 {
		return fmt.Errorf("SubscriptionNotificationCost of %v exceeds 1H", pt.SubscriptionNotificationCost)
	}

	// Check LatestRevisionCost - expect sane value.
	maxRevisionCost := a.MaxDownloadBandwidthPrice.Mul64(4096)
	if pt.LatestRevisionCost.Cmp(ConvertCurrency(maxRevisionCost)) > 0 {
		return fmt.Errorf("LatestRevisionCost of %v exceeds maximum cost of %v", pt.LatestRevisionCost, ConvertCurrency(maxRevisionCost))
	}

	// Check RenewContractCost - expect 100nS default.
	if core.Siacoins(1).Mul64(100).Div64(1e9).Cmp(pt.RenewContractCost) < 0 {
		return fmt.Errorf("RenewContractCost of %v exceeds 100nS", pt.RenewContractCost)
	}

	// Check RevisionBaseCost - expect 0H default.
	if core.ZeroCurrency.Cmp(pt.RevisionBaseCost) < 0 {
		return fmt.Errorf("RevisionBaseCost of %v exceeds 0H", pt.RevisionBaseCost)
	}

	// Check block height
	if pt.HostBlockHeight < uint64(height) {
		return fmt.Errorf("consensus not synced and host block height is lower, %v < %v", pt.HostBlockHeight, height)
	} else {
		min := height - a.BlockHeightLeeway
		max := height + a.BlockHeightLeeway
		if !(min <= types.BlockHeight(pt.HostBlockHeight) && types.BlockHeight(pt.HostBlockHeight) <= max) {
			return fmt.Errorf("host block height is not within range, %v-%v %v", min, max, pt.HostBlockHeight)
		}
	}

	// Check TxnFeeMaxRecommended - expect at most a multiple of our fee.
	if !txnFee.IsZero() && pt.TxnFeeMaxRecommended.Cmp(ConvertCurrency(txnFee.Mul64(5))) > 0 {
		return fmt.Errorf("TxnFeeMaxRecommended %v exceeds %v", pt.TxnFeeMaxRecommended, ConvertCurrency(txnFee.Mul64(5)))
	}

	// Check TxnFeeMinRecommended - expect it to be lower or equal than the max.
	if pt.TxnFeeMinRecommended.Cmp(pt.TxnFeeMaxRecommended) > 0 {
		return fmt.Errorf("TxnFeeMinRecommended is greater than TxnFeeMaxRecommended, %v>%v", pt.TxnFeeMinRecommended, pt.TxnFeeMaxRecommended)
	}

	return nil
}

// checkDownloadGougingRHPv2 checks the host settings.
func checkDownloadGougingRHPv2(a Allowance, hes smodules.HostExternalSettings) error {
	sectorDownloadPrice, overflow := sectorReadCostRHPv2(hes)
	if overflow {
		return fmt.Errorf("overflow detected when computing sector download price")
	}
	return checkDownloadGouging(a, sectorDownloadPrice)
}

// checkDownloadGougingRHPv2 checks the price table.
func checkDownloadGougingRHPv3(a Allowance, pt rhpv3.HostPriceTable) error {
	sectorDownloadPrice, overflow := sectorReadCostRHPv3(pt)
	if overflow {
		return fmt.Errorf("overflow detected when computing sector download price")
	}
	return checkDownloadGouging(a, sectorDownloadPrice)
}

// checkDownloadGouging performs the actual check.
func checkDownloadGouging(a Allowance, sectorDownloadPrice core.Currency) error {
	dpptb, overflow := sectorDownloadPrice.Mul64WithOverflow(1 << 40 / smodules.SectorSize) // sectors per TiB
	if overflow {
		return fmt.Errorf("overflow detected when computing download price per TiB")
	}
	downloadPriceTotalShards, overflow := dpptb.Mul64WithOverflow(a.TotalShards)
	if overflow {
		return fmt.Errorf("overflow detected when multiplying %v * %v in download gouging", dpptb, a.TotalShards)
	}
	downloadPrice := downloadPriceTotalShards.Div64(a.MinShards)
	max, overflow := ConvertCurrency(a.MaxDownloadBandwidthPrice).Mul64WithOverflow(BytesPerTerabyte)
	if overflow {
		return fmt.Errorf("overflow detected when computing download price per TiB")
	}
	if !a.MaxDownloadBandwidthPrice.IsZero() && downloadPrice.Cmp(max) > 0 {
		return fmt.Errorf("cost per TiB exceeds max dl price: %v>%v", downloadPrice, max)
	}
	return nil
}

// checkDownloadGougingRHPv2 checks the host settings.
func checkUploadGougingRHPv2(a Allowance, hes smodules.HostExternalSettings) error {
	sectorUploadPricePerMonth, overflow := sectorUploadCostPerMonthRHPv2(hes)
	if overflow {
		return fmt.Errorf("overflow detected when computing sector price")
	}
	return checkUploadGouging(a, sectorUploadPricePerMonth)
}

// checkDownloadGougingRHPv2 checks the price table.
func checkUploadGougingRHPv3(a Allowance, pt rhpv3.HostPriceTable) error {
	sectorUploadPricePerMonth, overflow := sectorUploadCostPerMonthRHPv3(pt)
	if overflow {
		return fmt.Errorf("overflow detected when computing sector price")
	}
	return checkUploadGouging(a, sectorUploadPricePerMonth)
}

// checkUploadGouging performs the actual check.
func checkUploadGouging(a Allowance, sectorUploadPricePerMonth core.Currency) error {
	upptb, overflow := sectorUploadPricePerMonth.Mul64WithOverflow(1 << 40 / smodules.SectorSize) // sectors per TiB
	if overflow {
		return fmt.Errorf("overflow detected when computing upload price per TiB")
	}
	uploadPriceTotalShards, overflow := upptb.Mul64WithOverflow(a.TotalShards)
	if overflow {
		return fmt.Errorf("overflow detected when multiplying %v * %v in upload gouging", upptb, a.TotalShards)
	}
	uploadPrice := uploadPriceTotalShards.Div64(a.MinShards)
	max, overflow := ConvertCurrency(a.MaxUploadBandwidthPrice).Mul64WithOverflow(BytesPerTerabyte)
	if overflow {
		return fmt.Errorf("overflow detected when computing download price per TiB")
	}
	if !a.MaxUploadBandwidthPrice.IsZero() && uploadPrice.Cmp(max) > 0 {
		return fmt.Errorf("cost per TiB exceeds max ul price: %v>%v", uploadPrice, max)
	}
	return nil
}

// sectorReadCostRHPv2 calculates the cost of reading a sector.
func sectorReadCostRHPv2(hes smodules.HostExternalSettings) (core.Currency, bool) {
	bandwidth := rhpv2.SectorSize + 2 * uint64(bits.Len64(rhpv2.SectorSize / rhpv2.LeavesPerSector)) * 32
	bandwidthPrice, overflow := ConvertCurrency(hes.DownloadBandwidthPrice).Mul64WithOverflow(bandwidth)
	if overflow {
		return core.ZeroCurrency, true
	}
	total, overflow := ConvertCurrency(hes.BaseRPCPrice).AddWithOverflow(ConvertCurrency(hes.SectorAccessPrice))
	if overflow {
		return core.ZeroCurrency, true
	}
	total, overflow = total.AddWithOverflow(bandwidthPrice)
	if overflow {
		return core.ZeroCurrency, true
	}
	return total, false
}

// sectorUploadCostPerMonthRHPv2 calculates the cost of uploading a sector per month.
func sectorUploadCostPerMonthRHPv2(hes smodules.HostExternalSettings) (core.Currency, bool) {
	// Base.
	base := ConvertCurrency(hes.BaseRPCPrice)
	// Storage.
	storage, overflow := ConvertCurrency(hes.StoragePrice).Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return core.ZeroCurrency, true
	}
	storage, overflow = storage.Mul64WithOverflow(4032)
	if overflow {
		return core.ZeroCurrency, true
	}
	// Bandwidth.
	upload, overflow := ConvertCurrency(hes.UploadBandwidthPrice).Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return core.ZeroCurrency, true
	}
	download, overflow := ConvertCurrency(hes.DownloadBandwidthPrice).Mul64WithOverflow(128 * 32) // Proof.
	if overflow {
		return core.ZeroCurrency, true
	}
	// Total.
	total, overflow := base.AddWithOverflow(storage)
	if overflow {
		return core.ZeroCurrency, true
	}
	total, overflow = total.AddWithOverflow(upload)
	if overflow {
		return core.ZeroCurrency, true
	}
	total, overflow = total.AddWithOverflow(download)
	if overflow {
		return core.ZeroCurrency, true
	}
	return total, false
}

// sectorReadCostRHPv3 calculates the cost of reading a sector.
func sectorReadCostRHPv3(pt rhpv3.HostPriceTable) (core.Currency, bool) {
	// Base.
	base, overflow := pt.ReadLengthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return core.ZeroCurrency, true
	}
	base, overflow = base.AddWithOverflow(pt.ReadBaseCost)
	if overflow {
		return core.ZeroCurrency, true
	}
	// Bandwidth.
	ingress, overflow := pt.UploadBandwidthCost.Mul64WithOverflow(32)
	if overflow {
		return core.ZeroCurrency, true
	}
	egress, overflow := pt.DownloadBandwidthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return core.ZeroCurrency, true
	}
	// Total.
	total, overflow := base.AddWithOverflow(ingress)
	if overflow {
		return core.ZeroCurrency, true
	}
	total, overflow = total.AddWithOverflow(egress)
	if overflow {
		return core.ZeroCurrency, true
	}
	return total, false
}

// sectorUploadCostPerMonthRHPv3 calculates the cost of uploading a sector per month.
func sectorUploadCostPerMonthRHPv3(pt rhpv3.HostPriceTable) (core.Currency, bool) {
	// Write.
	writeCost, overflow := pt.WriteLengthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return core.ZeroCurrency, true
	}
	writeCost, overflow = writeCost.AddWithOverflow(pt.WriteBaseCost)
	if overflow {
		return core.ZeroCurrency, true
	}
	// Storage.
	storage, overflow := pt.WriteStoreCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return core.ZeroCurrency, true
	}
	storage, overflow = storage.Mul64WithOverflow(4032)
	if overflow {
		return core.ZeroCurrency, true
	}
	// Bandwidth.
	ingress, overflow := pt.UploadBandwidthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return core.ZeroCurrency, true
	}
	// Total.
	total, overflow := writeCost.AddWithOverflow(storage)
	if overflow {
		return core.ZeroCurrency, true
	}
	total, overflow = total.AddWithOverflow(ingress)
	if overflow {
		return core.ZeroCurrency, true
	}
	return total, false
}
