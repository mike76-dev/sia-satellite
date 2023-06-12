package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// abs returns the absolute representation of a path.
func abs(path string) string {
	abspath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abspath
}

// absDuration is a small helper function that sanitizes the output for the
// given time duration. If the duration is less than 0 it will return 0,
// otherwise it will return the duration rounded to the nearest second.
func absDuration(t time.Duration) time.Duration {
	if t <= 0 {
		return 0
	}
	return t.Round(time.Second)
}

// askForConfirmation prints a question and waits for confirmation until the
// user gives a valid answer ("y", "yes", "n", "no" with any capitalization).
func askForConfirmation(s string) bool {
	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s [y/n]: ", s)
		answer, err := r.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "y" || answer == "yes" {
			return true
		} else if answer == "n" || answer == "no" {
			return false
		}
	}
}

// calculateAverageUint64 calculates the average of a uint64 slice and returns the average as a uint64
func calculateAverageUint64(input []uint64) uint64 {
	total := uint64(0)
	if len(input) == 0 {
		return 0
	}
	for _, v := range input {
		total += v
	}
	return total / uint64(len(input))
}

// calculateMedianUint64 calculates the median of a uint64 slice and returns the median as a uint64
func calculateMedianUint64(mm []uint64) uint64 {
	sort.Slice(mm, func(i, j int) bool { return mm[i] < mm[j] }) // sort the numbers

	mNumber := len(mm) / 2

	if len(mm) % 2 == 0 {
		return mm[mNumber]
	}

	return (mm[mNumber - 1] + mm[mNumber]) / 2
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
