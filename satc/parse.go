package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"sort"
	"strings"
	"time"

	"go.sia.tech/core/types"
)

var (
	// ErrParsePeriodAmount is returned when the input is unable to be parsed
	// into a period unit due to a malformed amount.
	ErrParsePeriodAmount = errors.New("malformed amount")
	// ErrParsePeriodUnits is returned when the input is unable to be parsed
	// into a period unit due to missing units.
	ErrParsePeriodUnits = errors.New("amount is missing period units")

	// ErrParseSizeAmount is returned when the input is unable to be parsed into
	// a file size unit due to a malformed amount.
	ErrParseSizeAmount = errors.New("malformed amount")
	// ErrParseSizeUnits is returned when the input is unable to be parsed into
	// a file size unit due to missing units.
	ErrParseSizeUnits = errors.New("amount is missing filesize units")

	// ErrParseTimeoutAmount is returned when the input is unable to be parsed
	// into a timeout unit due to a malformed amount.
	ErrParseTimeoutAmount = errors.New("malformed amount")
	// ErrParseTimeoutUnits is returned when the input is unable to be parsed
	// into a timeout unit due to missing units.
	ErrParseTimeoutUnits = errors.New("amount is missing timeout units")
)

// parseFilesize converts strings of form '10GB' or '10 gb' to a size in bytes.
// Fractional sizes are truncated at the byte size.
func parseFilesize(strSize string) (string, error) {
	units := []struct {
		suffix     string
		multiplier int64
	}{
		{"kb", 1e3},
		{"mb", 1e6},
		{"gb", 1e9},
		{"tb", 1e12},
		{"kib", 1 << 10},
		{"mib", 1 << 20},
		{"gib", 1 << 30},
		{"tib", 1 << 40},
		{"b", 1}, // must be after others else it'll match on them all
	}

	strSize = strings.ToLower(strings.TrimSpace(strSize))
	for _, unit := range units {
		if strings.HasSuffix(strSize, unit.suffix) {
			// Trim spaces after removing the suffix to allow spaces between the
			// value and the unit.
			value := strings.TrimSpace(strings.TrimSuffix(strSize, unit.suffix))
			r, ok := new(big.Rat).SetString(value)
			if !ok {
				return "", ErrParseSizeAmount
			}
			r.Mul(r, new(big.Rat).SetInt(big.NewInt(unit.multiplier)))
			if !r.IsInt() {
				f, _ := r.Float64()
				return fmt.Sprintf("%d", int64(f)), nil
			}
			return r.RatString(), nil
		}
	}

	return "", ErrParseSizeUnits
}

// periodUnits turns a period in terms of blocks to a number of weeks.
func periodUnits(blocks uint64) string {
	return fmt.Sprint(blocks / 1008) // 1008 blocks per week
}

// parsePeriod converts a duration specified in blocks, hours, or weeks to a
// number of blocks.
func parsePeriod(period string) (string, error) {
	units := []struct {
		suffix     string
		multiplier float64
	}{
		{"b", 1},        // blocks
		{"block", 1},    // blocks
		{"blocks", 1},   // blocks
		{"h", 6},        // hours
		{"hour", 6},     // hours
		{"hours", 6},    // hours
		{"d", 144},      // days
		{"day", 144},    // days
		{"days", 144},   // days
		{"w", 1008},     // weeks
		{"week", 1008},  // weeks
		{"weeks", 1008}, // weeks
	}

	period = strings.ToLower(strings.TrimSpace(period))
	for _, unit := range units {
		if strings.HasSuffix(period, unit.suffix) {
			var base float64
			_, err := fmt.Sscan(strings.TrimSuffix(period, unit.suffix), &base)
			if err != nil {
				return "", ErrParsePeriodAmount
			}
			blocks := int(base * unit.multiplier)
			return fmt.Sprint(blocks), nil
		}
	}

	return "", ErrParsePeriodUnits
}

// parseTimeout converts a duration specified in seconds, hours, days or weeks
// to a number of seconds.
func parseTimeout(duration string) (string, error) {
	units := []struct {
		suffix     string
		multiplier float64
	}{
		{"s", 1},          // seconds
		{"second", 1},     // seconds
		{"seconds", 1},    // seconds
		{"h", 3600},       // hours
		{"hour", 3600},    // hours
		{"hours", 3600},   // hours
		{"d", 86400},      // days
		{"day", 86400},    // days
		{"days", 86400},   // days
		{"w", 604800},     // weeks
		{"week", 604800},  // weeks
		{"weeks", 604800}, // weeks
	}

	duration = strings.ToLower(strings.TrimSpace(duration))
	for _, unit := range units {
		if strings.HasSuffix(duration, unit.suffix) {
			value := strings.TrimSpace(strings.TrimSuffix(duration, unit.suffix))
			var base float64
			_, err := fmt.Sscan(value, &base)
			if err != nil {
				return "", ErrParseTimeoutAmount
			}
			seconds := int(base * unit.multiplier)
			return fmt.Sprint(seconds), nil
		}
	}

	return "", ErrParseTimeoutUnits
}

// yesNo returns "Yes" if b is true, and "No" if b is false.
func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// parseTxn decodes a transaction from s, which can be JSON, base64, or a path
// to a file containing either encoding.
func parseTxn(s string) (types.Transaction, error) {
	// First assume s is a file.
	txnBytes, err := os.ReadFile(s)
	if os.IsNotExist(err) {
		// Assume s is a literal encoding.
		txnBytes = []byte(s)
	} else if err != nil {
		return types.Transaction{}, errors.New("could not read transaction file: " + err.Error())
	}
	// txnBytes now contains either s or the contents of the file, so it is
	// either JSON or base64.
	var txn types.Transaction
	if json.Valid(txnBytes) {
		if err := json.Unmarshal(txnBytes, &txn); err != nil {
			return types.Transaction{}, errors.New("could not decode JSON transaction: " + err.Error())
		}
	} else {
		bin, err := base64.StdEncoding.DecodeString(string(txnBytes))
		if err != nil {
			return types.Transaction{}, errors.New("argument is not valid JSON, base64, or filepath")
		}
		buf := bytes.NewBuffer(bin)
		d := types.NewDecoder(io.LimitedReader{R: buf, N: int64(len(bin))})
		txn.DecodeFrom(d)
		if err := d.Err(); err != nil {
			return types.Transaction{}, errors.New("could not decode binary transaction: " + err.Error())
		}
	}
	return txn, nil
}

// fmtDuration converts a time.Duration into a days,hours,minutes string.
func fmtDuration(dur time.Duration) string {
	dur = dur.Round(time.Minute)
	d := dur / time.Hour / 24
	dur -= d * time.Hour * 24
	h := dur / time.Hour
	dur -= h * time.Hour
	m := dur / time.Minute
	return fmt.Sprintf("%02d days %02d hours %02d minutes", d, h, m)
}

// parsePercentages takes a range of floats and returns them rounded to
// percentages that add up to 100. They will be returned in the same order that
// they were provided.
func parsePercentages(values []float64) []float64 {
	// Create a slice of percentInfo to track information of the values in the
	// slice and calculate the subTotal of the floor values.
	type percentInfo struct {
		index       int
		floorVal    float64
		originalVal float64
	}
	var percentages []*percentInfo
	var subTotal float64
	for i, v := range values {
		fv := math.Floor(v)
		percentages = append(percentages, &percentInfo{
			index:       i,
			floorVal:    fv,
			originalVal: v,
		})
		subTotal += fv
	}

	// Sanity check and regression check that all values were added. Fine to
	// continue through in production as result will only be a minor UX
	// discrepancy.
	if len(percentages) != len(values) {
		fmt.Println("CRITICAL: not all values added to percentage slice; potential duplicate value error")
	}

	// Determine the difference to 100 from the subTotal of the floor values
	diff := 100 - subTotal

	// Diff should always be smaller than the number of values. Sanity check for
	// developers, fine to continue through in production as result will only be
	// a minor UX discrepancy.
	if int(diff) > len(values) {
		fmt.Println(fmt.Errorf("Unexpected diff value %v, number of values %v", diff, len(values)))
	}

	// Sort the slice based on the size of the decimal value.
	sort.Slice(percentages, func(i, j int) bool {
		_, a := math.Modf(percentages[i].originalVal)
		_, b := math.Modf(percentages[j].originalVal)
		return a > b
	})

	// Divide the diff amongst the floor values from largest decimal value to
	// the smallest to decide which values get rounded up.
	for _, pi := range percentages {
		if diff <= 0 {
			break
		}
		pi.floorVal++
		diff--
	}

	// Reorder the slice and return
	for _, pi := range percentages {
		values[pi.index] = pi.floorVal
	}

	return values
}

// sizeString converts the uint64 size to a string with appropriate units and
// truncates to 4 significant digits.
func sizeString(size uint64) string {
	sizes := []struct {
		unit   string
		factor float64
	}{
		{"EB", 1e18},
		{"PB", 1e15},
		{"TB", 1e12},
		{"GB", 1e9},
		{"MB", 1e6},
		{"KB", 1e3},
		{"B", 1e0},
	}

	// Convert size to a float.
	for i, s := range sizes {
		// Check to see if we are at the right order of magnitude.
		res := float64(size) / s.factor
		if res < 1 {
			continue
		}
		// Create the string.
		str := fmt.Sprintf("%.4g %s", res, s.unit)
		// Check for rounding to three 0s.
		if !strings.Contains(str, "000") {
			return str
		}
		// If we are at the max unit then there is no trimming to do.
		if i == 0 {
			fmt.Println("CRITICAL: input uint64 overflows uint64, shouldn't be possible")
			return str
		}
		// Trim the trailing three 0s and round to the next unit size
		return fmt.Sprintf("1 %s", sizes[i-1].unit)
	}
	return "0 B"
}
