package modules

import (
	"errors"
	"fmt"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
)

const (
	// maxBaseRPCPriceVsBandwidth is the max ratio for sane pricing between the
	// MinBaseRPCPrice and the MinDownloadBandwidthPrice. This ensures that 1
	// million base RPC charges are at most 1% of the cost to download 4TB. This
	// ratio should be used by checking that the MinBaseRPCPrice is less than or
	// equal to the MinDownloadBandwidthPrice multiplied by this constant
	maxBaseRPCPriceVsBandwidth = uint64(40e3)

	// maxSectorAccessPriceVsBandwidth is the max ratio for sane pricing between
	// the MinSectorAccessPrice and the MinDownloadBandwidthPrice. This ensures
	// that 1 million base accesses are at most 10% of the cost to download 4TB.
	// This ratio should be used by checking that the MinSectorAccessPrice is
	// less than or equal to the MinDownloadBandwidthPrice multiplied by this
	// constant
	maxSectorAccessPriceVsBandwidth = uint64(400e3)
)

// CheckGouging performs a number of gouging checks before forming
// a contract with the host.
func CheckGouging(a Allowance, height uint64, hs *rhpv2.HostSettings, pt *rhpv3.HostPriceTable, txnFee types.Currency) (err error) {
	if hs == nil && pt == nil {
		return errors.New("either host settings or price table must be not nil")
	}

	// Host settings checks.
	if hs != nil {
		if err = checkContractGougingRHPv2(a, *hs); err != nil {
			return
		}
		if err = checkPriceGougingHS(a, *hs); err != nil {
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
		if err = checkContractGougingRHPv3(a, *pt); err != nil {
			return
		}
	}

	return nil
}

// checkPriceGougingHS checks the host settings.
func checkPriceGougingHS(a Allowance, hs rhpv2.HostSettings) error {
	// Check base RPC price.
	if !a.MaxRPCPrice.IsZero() && hs.BaseRPCPrice.Cmp(a.MaxRPCPrice) > 0 {
		return fmt.Errorf("rpc price exceeds max: %v>%v", hs.BaseRPCPrice, a.MaxRPCPrice)
	}
	maxBaseRPCPrice := hs.DownloadBandwidthPrice.Mul64(maxBaseRPCPriceVsBandwidth)
	if hs.BaseRPCPrice.Cmp(maxBaseRPCPrice) > 0 {
		return fmt.Errorf("rpc price too high, %v > %v", hs.BaseRPCPrice, maxBaseRPCPrice)
	}

	// Check sector access price.
	if hs.DownloadBandwidthPrice.IsZero() {
		hs.DownloadBandwidthPrice = types.NewCurrency64(1)
	}
	maxSectorAccessPrice := hs.DownloadBandwidthPrice.Mul64(maxSectorAccessPriceVsBandwidth)
	if hs.SectorAccessPrice.Cmp(maxSectorAccessPrice) > 0 {
		return fmt.Errorf("sector access price too high, %v > %v", hs.SectorAccessPrice, maxSectorAccessPrice)
	}

	// Check max storage price.
	if !a.MaxStoragePrice.IsZero() && hs.StoragePrice.Cmp(a.MaxStoragePrice) > 0 {
		return fmt.Errorf("storage price exceeds max: %v > %v", hs.StoragePrice, a.MaxStoragePrice)
	}

	// Check contract price.
	if !a.MaxContractPrice.IsZero() && hs.ContractPrice.Cmp(a.MaxContractPrice) > 0 {
		return fmt.Errorf("contract price exceeds max: %v > %v", hs.ContractPrice, a.MaxContractPrice)
	}

	// Check max collateral.
	if hs.MaxCollateral.IsZero() {
		return errors.New("MaxCollateral of the host is 0")
	}
	if hs.MaxCollateral.Cmp(a.MinMaxCollateral) < 0 {
		return fmt.Errorf("MaxCollateral is below minimum: %v < %v", hs.MaxCollateral, a.MinMaxCollateral)
	}

	return nil
}

// checkPriceGougingPT checks the price table.
func checkPriceGougingPT(a Allowance, height uint64, txnFee types.Currency, pt rhpv3.HostPriceTable) error {
	// Check base RPC price.
	if !a.MaxRPCPrice.IsZero() && a.MaxRPCPrice.Cmp(pt.InitBaseCost) < 0 {
		return fmt.Errorf("init base cost exceeds max: %v > %v", pt.InitBaseCost, a.MaxRPCPrice)
	}

	// Check contract price.
	if !a.MaxContractPrice.IsZero() && pt.ContractPrice.Cmp(a.MaxContractPrice) > 0 {
		return fmt.Errorf("contract price exceeds max: %v > %v", pt.ContractPrice, a.MaxContractPrice)
	}

	// Check storage price.
	if !a.MaxStoragePrice.IsZero() && pt.WriteStoreCost.Cmp(a.MaxStoragePrice) > 0 {
		return fmt.Errorf("storage price exceeds max: %v > %v", pt.WriteStoreCost, a.MaxStoragePrice)
	}

	// Check max collateral.
	if pt.MaxCollateral.IsZero() {
		return errors.New("MaxCollateral of host is 0")
	}
	if pt.MaxCollateral.Cmp(a.MinMaxCollateral) < 0 {
		return fmt.Errorf("MaxCollateral is below minimum: %v < %v", pt.MaxCollateral, a.MinMaxCollateral)
	}

	// Check ReadLengthCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.ReadLengthCost) < 0 {
		return fmt.Errorf("ReadLengthCost of %v exceeds 1H", pt.ReadLengthCost)
	}

	// Check WriteLengthCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.WriteLengthCost) < 0 {
		return fmt.Errorf("WriteLengthCost of %v exceeds 1H", pt.WriteLengthCost)
	}

	// Check AccountBalanceCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.AccountBalanceCost) < 0 {
		return fmt.Errorf("AccountBalanceCost of %v exceeds 1H", pt.AccountBalanceCost)
	}

	// Check FundAccountCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.FundAccountCost) < 0 {
		return fmt.Errorf("FundAccountCost of %v exceeds 1H", pt.FundAccountCost)
	}

	// Check UpdatePriceTableCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.UpdatePriceTableCost) < 0 {
		return fmt.Errorf("UpdatePriceTableCost of %v exceeds 1H", pt.UpdatePriceTableCost)
	}

	// Check HasSectorBaseCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.HasSectorBaseCost) < 0 {
		return fmt.Errorf("HasSectorBaseCost of %v exceeds 1H", pt.HasSectorBaseCost)
	}

	// Check MemoryTimeCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.MemoryTimeCost) < 0 {
		return fmt.Errorf("MemoryTimeCost of %v exceeds 1H", pt.MemoryTimeCost)
	}

	// Check DropSectorsBaseCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.DropSectorsBaseCost) < 0 {
		return fmt.Errorf("DropSectorsBaseCost of %v exceeds 1H", pt.DropSectorsBaseCost)
	}

	// Check DropSectorsUnitCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.DropSectorsUnitCost) < 0 {
		return fmt.Errorf("DropSectorsUnitCost of %v exceeds 1H", pt.DropSectorsUnitCost)
	}

	// Check SwapSectorCost - should be 1H as it's unused by hosts.
	if types.NewCurrency64(1).Cmp(pt.SwapSectorCost().Base) < 0 {
		return fmt.Errorf("SwapSectorBaseCost of %v exceeds 1H", pt.SwapSectorCost().Base)
	}

	// Check SubscriptionMemoryCost - expect 1H default.
	if types.NewCurrency64(1).Cmp(pt.SubscriptionMemoryCost) < 0 {
		return fmt.Errorf("SubscriptionMemoryCost of %v exceeds 1H", pt.SubscriptionMemoryCost)
	}

	// Check SubscriptionNotificationCost - expect 1H default.
	if types.NewCurrency64(1).Cmp(pt.SubscriptionNotificationCost) < 0 {
		return fmt.Errorf("SubscriptionNotificationCost of %v exceeds 1H", pt.SubscriptionNotificationCost)
	}

	// Check LatestRevisionCost - expect sane value.
	maxRevisionCost := a.MaxDownloadBandwidthPrice.Mul64(4096)
	if pt.LatestRevisionCost.Cmp(maxRevisionCost) > 0 {
		return fmt.Errorf("LatestRevisionCost of %v exceeds maximum cost of %v", pt.LatestRevisionCost, maxRevisionCost)
	}

	// Check RenewContractCost - expect 100nS default.
	if types.Siacoins(1).Mul64(100).Div64(1e9).Cmp(pt.RenewContractCost) < 0 {
		return fmt.Errorf("RenewContractCost of %v exceeds 100nS", pt.RenewContractCost)
	}

	// Check RevisionBaseCost - expect 0H default.
	if types.ZeroCurrency.Cmp(pt.RevisionBaseCost) < 0 {
		return fmt.Errorf("RevisionBaseCost of %v exceeds 0H", pt.RevisionBaseCost)
	}

	// Check block height.
	if pt.HostBlockHeight < height {
		return fmt.Errorf("consensus not synced and host block height is lower, %v < %v", pt.HostBlockHeight, height)
	} else {
		min := height - a.BlockHeightLeeway
		max := height + a.BlockHeightLeeway
		if !(min <= pt.HostBlockHeight && pt.HostBlockHeight <= max) {
			return fmt.Errorf("host block height is not within range, %v-%v %v", min, max, pt.HostBlockHeight)
		}
	}

	// Check TxnFeeMaxRecommended - expect at most a multiple of our fee.
	if !txnFee.IsZero() && pt.TxnFeeMaxRecommended.Cmp(txnFee.Mul64(5)) > 0 {
		return fmt.Errorf("TxnFeeMaxRecommended %v exceeds %v", pt.TxnFeeMaxRecommended, txnFee.Mul64(5))
	}

	// Check TxnFeeMinRecommended - expect it to be lower or equal than the max.
	if pt.TxnFeeMinRecommended.Cmp(pt.TxnFeeMaxRecommended) > 0 {
		return fmt.Errorf("TxnFeeMinRecommended is greater than TxnFeeMaxRecommended, %v>%v", pt.TxnFeeMinRecommended, pt.TxnFeeMaxRecommended)
	}

	return nil
}

func checkContractGougingRHPv2(a Allowance, hs rhpv2.HostSettings) error {
	return checkContractGouging(a.Period, a.RenewWindow, hs.MaxDuration, hs.WindowSize)
}

func checkContractGougingRHPv3(a Allowance, pt rhpv3.HostPriceTable) error {
	return checkContractGouging(a.Period, a.RenewWindow, pt.MaxDuration, pt.WindowSize)
}

func checkContractGouging(period, renewWindow, maxDuration, windowSize uint64) error {
	// Check MaxDuration.
	if period != 0 && period > maxDuration {
		return fmt.Errorf("MaxDuration %v is lower than the period %v", maxDuration, period)
	}

	// Check WindowSize.
	if renewWindow != 0 && renewWindow < windowSize {
		return fmt.Errorf("minimum WindowSize %v is greater than the renew window %v", windowSize, renewWindow)
	}

	return nil
}

// checkDownloadGougingRHPv3 checks the price table.
func checkDownloadGougingRHPv3(a Allowance, pt rhpv3.HostPriceTable) error {
	sectorDownloadPrice, overflow := sectorReadCostRHPv3(pt)
	if overflow {
		return fmt.Errorf("overflow detected when computing sector download price")
	}
	dpptb, overflow := sectorDownloadPrice.Mul64WithOverflow(1 << 40 / rhpv2.SectorSize) // sectors per TiB
	if overflow {
		return fmt.Errorf("overflow detected when computing download price per TiB")
	}
	if !a.MaxDownloadBandwidthPrice.IsZero() && dpptb.Cmp(a.MaxDownloadBandwidthPrice) > 0 {
		return fmt.Errorf("cost per TiB exceeds max dl price: %v > %v", dpptb, a.MaxDownloadBandwidthPrice)
	}
	return nil
}

// checkUploadGougingRHPv3 checks the price table.
func checkUploadGougingRHPv3(a Allowance, pt rhpv3.HostPriceTable) error {
	sectorUploadPricePerMonth, overflow := sectorUploadCostRHPv3(pt)
	if overflow {
		return fmt.Errorf("overflow detected when computing sector price")
	}
	uploadPrice, overflow := sectorUploadPricePerMonth.Mul64WithOverflow(1 << 40 / rhpv2.SectorSize) // sectors per TiB
	if overflow {
		return fmt.Errorf("overflow detected when computing upload price per TiB")
	}
	if !a.MaxUploadBandwidthPrice.IsZero() && uploadPrice.Cmp(a.MaxUploadBandwidthPrice) > 0 {
		return fmt.Errorf("cost per TiB exceeds max ul price: %v > %v", uploadPrice, a.MaxUploadBandwidthPrice)
	}
	return nil
}

// sectorReadCostRHPv3 calculates the cost of reading a sector.
func sectorReadCostRHPv3(pt rhpv3.HostPriceTable) (types.Currency, bool) {
	// Base.
	base, overflow := pt.ReadLengthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return types.ZeroCurrency, true
	}
	base, overflow = base.AddWithOverflow(pt.ReadBaseCost)
	if overflow {
		return types.ZeroCurrency, true
	}
	base, overflow = base.AddWithOverflow(pt.InitBaseCost)
	if overflow {
		return types.ZeroCurrency, true
	}
	// Bandwidth.
	ingress, overflow := pt.UploadBandwidthCost.Mul64WithOverflow(32)
	if overflow {
		return types.ZeroCurrency, true
	}
	egress, overflow := pt.DownloadBandwidthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return types.ZeroCurrency, true
	}
	// Total.
	total, overflow := base.AddWithOverflow(ingress)
	if overflow {
		return types.ZeroCurrency, true
	}
	total, overflow = total.AddWithOverflow(egress)
	if overflow {
		return types.ZeroCurrency, true
	}
	return total, false
}

// sectorUploadCostRHPv3 calculates the cost of uploading a sector per month.
func sectorUploadCostRHPv3(pt rhpv3.HostPriceTable) (types.Currency, bool) {
	// Write.
	writeCost, overflow := pt.WriteLengthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return types.ZeroCurrency, true
	}
	writeCost, overflow = writeCost.AddWithOverflow(pt.WriteBaseCost)
	if overflow {
		return types.ZeroCurrency, true
	}
	writeCost, overflow = writeCost.AddWithOverflow(pt.InitBaseCost)
	if overflow {
		return types.ZeroCurrency, true
	}
	// Bandwidth.
	ingress, overflow := pt.UploadBandwidthCost.Mul64WithOverflow(rhpv2.SectorSize)
	if overflow {
		return types.ZeroCurrency, true
	}
	// Total.
	total, overflow := writeCost.AddWithOverflow(ingress)
	if overflow {
		return types.ZeroCurrency, true
	}
	return total, false
}
