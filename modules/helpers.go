package modules

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"go.sia.tech/core/types"
)

// ReadCurrency converts a string to types.Currency.
func ReadCurrency(s string) types.Currency {
	i, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return types.ZeroCurrency
	} else if i.Sign() < 0 {
		return types.ZeroCurrency
	} else if i.BitLen() > 128 {
		return types.ZeroCurrency
	}
	return types.NewCurrency(i.Uint64(), new(big.Int).Rsh(i, 64).Uint64())
}

// Float64 converts types.Currency to float64.
func Float64(c types.Currency) float64 {
	f, _ := new(big.Rat).SetInt(c.Big()).Float64()
	return f
}

// FromFloat converts f Siacoins to a types.Currency value.
func FromFloat(f float64) types.Currency {
	if f < 0 {
		return types.ZeroCurrency
	}
	h := new(big.Rat).SetInt(types.HastingsPerSiacoin.Big())
	r := new(big.Rat).Mul(h, new(big.Rat).SetFloat64(f))
	nBuf := make([]byte, 16)
	n := r.Num().Bytes()
	copy(nBuf[16 - len(n):], n[:])
	num := types.NewCurrency(binary.BigEndian.Uint64(nBuf[8:]), binary.BigEndian.Uint64(nBuf[:8]))
	dBuf := make([]byte, 16)
	d := r.Denom().Bytes()
	copy(dBuf[16 - len(d):], d[:])
	denom := types.NewCurrency(binary.BigEndian.Uint64(dBuf[8:]), binary.BigEndian.Uint64(dBuf[:8]))
	return num.Div(denom)
}

// MulFloat multiplies a types.Currency by a float64 value.
func MulFloat(c types.Currency, f float64) types.Currency {
	x := new(big.Rat).SetInt(c.Big())
	y := new(big.Rat).SetFloat64(f)
	x = x.Mul(x, y)
	nBuf := make([]byte, 16)
	n := x.Num().Bytes()
	copy(nBuf[16 - len(n):], n[:])
	num := types.NewCurrency(binary.BigEndian.Uint64(nBuf[8:]), binary.BigEndian.Uint64(nBuf[:8]))
	dBuf := make([]byte, 16)
	d := x.Denom().Bytes()
	copy(dBuf[16 - len(d):], d[:])
	denom := types.NewCurrency(binary.BigEndian.Uint64(dBuf[8:]), binary.BigEndian.Uint64(dBuf[:8]))
	return num.Div(denom)
}

// Tax calculates the Siafund fee from the amount.
func Tax(height uint64, payout types.Currency) types.Currency {
	// First 21,000 blocks need to be treated differently.
	i := payout.Big()
	if height + 1 < TaxHardforkHeight {
		r := new(big.Rat).SetInt(i)
		r.Mul(r, new(big.Rat).SetFloat64(0.039))
		i.Div(r.Num(), r.Denom())
	} else {
		i.Mul(i, big.NewInt(39))
		i.Div(i, big.NewInt(1000))
	}

	// Round down to multiple of SiafundCount.
	i.Sub(i, new(big.Int).Mod(i, big.NewInt(int64(SiafundCount))))

	// Convert to currency.
	lo := i.Uint64()
	hi := i.Rsh(i, 64).Uint64()
	return types.NewCurrency(lo, hi)
}

// PostTax returns the amount of currency remaining in a file contract payout
// after tax.
func PostTax(height uint64, payout types.Currency) types.Currency {
	return payout.Sub(Tax(height, payout))
}

// FilesizeUnits returns a string that displays a filesize in human-readable units.
func FilesizeUnits(size uint64) string {
	if size == 0 {
		return "0  B"
	}
	sizes := []string{" B", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
	i := int(math.Log10(float64(size)) / 3)
	return fmt.Sprintf("%.*f %s", i, float64(size)/math.Pow10(3*i), sizes[i])
}

// StorageProofOutputID returns the ID of an output created by a file
// contract, given the status of the storage proof. The ID is calculating by
// hashing the concatenation of the StorageProofOutput Specifier, the ID of
// the file contract that the proof is for, a boolean indicating whether the
// proof was valid (true) or missed (false), and the index of the output
// within the file contract.
func StorageProofOutputID(fcid types.FileContractID, proofStatus bool, i int) types.SiacoinOutputID {
	h := types.NewHasher()
	types.SpecifierStorageProof.EncodeTo(h.E)
	fcid.EncodeTo(h.E)
	h.E.WriteBool(proofStatus)
	h.E.WriteUint64(uint64(i))
	return types.SiacoinOutputID(h.Sum())
}

// CalculateCoinbase calculates the coinbase for a given height. The coinbase
// equation is:
// coinbase := max(InitialCoinbase - height, MinimumCoinbase)
func CalculateCoinbase(height uint64) types.Currency {
	base := InitialCoinbase - height
	if height > InitialCoinbase || base < MinimumCoinbase {
		base = MinimumCoinbase
	}
	return types.NewCurrency64(base).Mul(types.HastingsPerSiacoin)
}

// CalculateSubsidy takes a block and a height and determines the block
// subsidy.
func CalculateSubsidy(b types.Block, height uint64) types.Currency {
	subsidy := CalculateCoinbase(height)
	for _, txn := range b.Transactions {
		for _, fee := range txn.MinerFees {
			subsidy = subsidy.Add(fee)
		}
	}
	return subsidy
}

// CalculateNumSiacoins calculates the number of siacoins in circulation at a
// given height.
func CalculateNumSiacoins(height uint64) (total types.Currency) {
	total = numGenesisSiacoins
	deflationBlocks := InitialCoinbase - MinimumCoinbase
	avgDeflationSiacoins := CalculateCoinbase(0).Add(CalculateCoinbase(height)).Div64(2)
	if height <= deflationBlocks {
		total = total.Add(avgDeflationSiacoins.Mul64(height + 1))
	} else {
		total = total.Add(avgDeflationSiacoins.Mul64(deflationBlocks + 1))
		total = total.Add(CalculateCoinbase(height).Mul64(height - deflationBlocks))
	}
	if height >= FoundationHardforkHeight {
		total = total.Add(InitialFoundationSubsidy)
		perSubsidy := FoundationSubsidyPerBlock.Mul64(FoundationSubsidyFrequency)
		subsidies := (height - FoundationHardforkHeight) / FoundationSubsidyFrequency
		total = total.Add(perSubsidy.Mul64(subsidies))
	}
	return
}
