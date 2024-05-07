package modules

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/big"
	"strings"

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
	if f < 1e-24 {
		return types.ZeroCurrency
	}
	h := new(big.Rat).SetInt(types.HastingsPerSiacoin.Big())
	r := new(big.Rat).Mul(h, new(big.Rat).SetFloat64(f))
	nBuf := make([]byte, 16)
	n := r.Num().Bytes()
	copy(nBuf[16-len(n):], n[:])
	num := types.NewCurrency(binary.BigEndian.Uint64(nBuf[8:]), binary.BigEndian.Uint64(nBuf[:8]))
	dBuf := make([]byte, 16)
	d := r.Denom().Bytes()
	copy(dBuf[16-len(d):], d[:])
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
	copy(nBuf[16-len(n):], n[:])
	num := types.NewCurrency(binary.BigEndian.Uint64(nBuf[8:]), binary.BigEndian.Uint64(nBuf[:8]))
	dBuf := make([]byte, 16)
	d := x.Denom().Bytes()
	copy(dBuf[16-len(d):], d[:])
	denom := types.NewCurrency(binary.BigEndian.Uint64(dBuf[8:]), binary.BigEndian.Uint64(dBuf[:8]))
	return num.Div(denom)
}

// ToString formats a types.Currency to a maximum of 2 decimal places.
func ToString(c types.Currency) string {
	if c.IsZero() {
		return "0 H"
	}
	s := c.Big().String()
	u := (len(s) - 1) / 3
	if u < 4 {
		return s + " H"
	} else if u > 12 {
		u = 12
	}
	mant := s[:len(s)-u*3]
	if frac := strings.TrimRight(s[len(s)-u*3:], "0"); len(frac) > 0 {
		if len(frac) > 2 {
			frac = frac[:2]
		}
		mant += "." + frac
	}
	unit := []string{"pS", "nS", "uS", "mS", "SC", "KS", "MS", "GS", "TS"}[u-4]
	return mant + " " + unit
}

// Tax calculates the Siafund fee from the amount.
func Tax(height uint64, payout types.Currency) types.Currency {
	// First 21,000 blocks need to be treated differently.
	i := payout.Big()
	if height+1 < 21000 {
		r := new(big.Rat).SetInt(i)
		r.Mul(r, new(big.Rat).SetFloat64(0.039))
		i.Div(r.Num(), r.Denom())
	} else {
		i.Mul(i, big.NewInt(39))
		i.Div(i, big.NewInt(1000))
	}

	// Round down to multiple of SiafundCount.
	i.Sub(i, new(big.Int).Mod(i, big.NewInt(10000)))

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

// RenterPayoutsPreTax calculates the renterPayout before tax and the hostPayout
// given a host, the available renter funding, the expected txnFee for the
// transaction and an optional basePrice in case this helper is used for a
// renewal. It also returns the hostCollateral.
func RenterPayoutsPreTax(host HostDBEntry, funding, txnFee, basePrice, baseCollateral types.Currency, period uint64, expectedStorage uint64) (renterPayout, hostPayout, hostCollateral types.Currency, err error) {
	// Divide by zero check.
	if host.Settings.StoragePrice.IsZero() {
		host.Settings.StoragePrice = types.NewCurrency64(1)
	}

	// Underflow check.
	if funding.Cmp(host.Settings.ContractPrice.Add(txnFee).Add(basePrice)) <= 0 {
		err = fmt.Errorf("contract price (%v) plus transaction fee (%v) plus base price (%v) exceeds funding (%v)",
			host.Settings.ContractPrice, txnFee, basePrice, funding)
		return
	}

	// Calculate renterPayout.
	renterPayout = funding.Sub(host.Settings.ContractPrice).Sub(txnFee).Sub(basePrice)

	// Calculate hostCollateral by calculating the maximum amount of storage
	// the renter can afford with 'funding' and calculating how much collateral
	// the host wouldl have to put into the contract for that. We also add a
	// potential baseCollateral.
	maxStorageSizeTime := renterPayout.Div(host.Settings.StoragePrice)
	hostCollateral = maxStorageSizeTime.Mul(host.Settings.Collateral).Add(baseCollateral)

	// Don't add more collateral than 10x the collateral for the expected
	// storage to save on fees.
	maxRenterCollateral := host.Settings.Collateral.Mul64(period).Mul64(expectedStorage).Mul64(5)
	if hostCollateral.Cmp(maxRenterCollateral) > 0 {
		hostCollateral = maxRenterCollateral
	}

	// Don't add more collateral than the host is willing to put into a single
	// contract.
	if hostCollateral.Cmp(host.Settings.MaxCollateral) > 0 {
		hostCollateral = host.Settings.MaxCollateral
	}

	// Calculate hostPayout.
	hostPayout = hostCollateral.Add(host.Settings.ContractPrice).Add(basePrice)
	return
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

// CopyTransaction creates a deep copy of the transaction.
func CopyTransaction(txn types.Transaction) types.Transaction {
	var newTxn types.Transaction
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	txn.EncodeTo(e)
	e.Flush()
	d := types.NewDecoder(io.LimitedReader{R: &buf, N: int64(buf.Len())})
	newTxn.DecodeFrom(d)
	return newTxn
}

// EncodedLen returns the encoded length of a transaction.
func EncodedLen(txn types.Transaction) int {
	var buf bytes.Buffer
	e := types.NewEncoder(&buf)
	txn.EncodeTo(e)
	e.Flush()
	return buf.Len()
}
