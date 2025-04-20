package external

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	rhpv4 "go.sia.tech/core/rhp/v4"
	"go.sia.tech/core/types"
)

const (
	// hostscoretAPI is the endpoint of the HostScore API.
	hostscoreAPI = "https://api.hostscore.info/v1/hosts?offset=0&limit=-1"
)

var ErrHostScoreTimeout = errors.New("HostScore service unavailable")

type (
	// HostScan represents the result of a host scan.
	HostScan struct {
		Timestamp time.Time     `json:"timestamp"`
		Success   bool          `json:"success"`
		Latency   time.Duration `json:"latency"`
		Error     string        `json:"error"`
	}

	// HostBenchmark represents the result of a host benchmark.
	HostBenchmark struct {
		Timestamp     time.Time `json:"timestamp"`
		Success       bool      `json:"success"`
		Error         string    `json:"error"`
		UploadSpeed   float64   `json:"uploadSpeed"`
		DownloadSpeed float64   `json:"downloadSpeed"`
		TTFB          uint64    `json:"ttfb"`
	}

	// HostScoreBreakdown combines different host score metrics.
	HostScoreBreakdown struct {
		PricesScore       float64 `json:"prices"`
		StorageScore      float64 `json:"storage"`
		CollateralScore   float64 `json:"collateral"`
		InteractionsScore float64 `json:"interactions"`
		UptimeScore       float64 `json:"uptime"`
		AgeScore          float64 `json:"age"`
		VersionScore      float64 `json:"version"`
		LatencyScore      float64 `json:"latency"`
		BenchmarksScore   float64 `json:"benchmarks"`
		ContractsScore    float64 `json:"contracts"`
		TotalScore        float64 `json:"total"`
	}

	// HostInteraction represents the current interaction status with a host.
	HostInteraction struct {
		Uptime           time.Duration      `json:"uptime"`
		Downtime         time.Duration      `json:"downtime"`
		ScanHistory      []HostScan         `json:"scanHistory"`
		BenchmarkHistory []HostBenchmark    `json:"benchmarkHistory"`
		LastSeen         time.Time          `json:"lastSeen"`
		ActiveHosts      int                `json:"activeHosts"`
		Score            HostScoreBreakdown `json:"score"`
		Successes        float64            `json:"successes"`
		Failures         float64            `json:"failures"`
	}

	// Host represents a HostScore host.
	Host struct {
		ID              int                        `json:"id"`
		Rank            int                        `json:"rank"`
		PublicKey       types.PublicKey            `json:"publicKey"`
		FirstSeen       time.Time                  `json:"firstSeen"`
		KnownSince      uint64                     `json:"knownSince"`
		NetAddress      string                     `json:"netaddress"`
		Blocked         bool                       `json:"blocked"`
		V2              bool                       `json:"v2"`
		Interactions    map[string]HostInteraction `json:"interactions"`
		IPNets          []string                   `json:"ipNets"`
		LastIPChange    time.Time                  `json:"lastIPChange"`
		Score           HostScoreBreakdown         `json:"score"`
		Settings        rhpv4.HostSettings         `json:"v2Settings,omitempty"`
		SiamuxAddresses []string                   `json:"siamuxAddresses"`
		IPInfo
	}

	// IPInfo contains the geolocation data of a host.
	IPInfo struct {
		IP       string `json:"ip"`
		HostName string `json:"hostname"`
		City     string `json:"city"`
		Region   string `json:"region"`
		Country  string `json:"country"`
		Location string `json:"loc"`
		ISP      string `json:"org"`
		ZIP      string `json:"postal"`
		TimeZone string `json:"timezone"`
	}
)

// hostsResponse is the response type for the /hosts API call.
type hostsResponse struct {
	Hosts []Host `json:"hosts"`
}

// GetHosts retrieves the list of online hosts.
func GetHosts(zen bool) ([]Host, error) {
	urlString := hostscoreAPI
	if zen {
		urlString += "&network=zen"
	}
	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Get(urlString)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("falied to fetch hosts: %s", resp.Status)
		}
		var data hostsResponse
		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&data)
		if err != nil {
			return nil, utils.AddContext(err, "couldn't decode hosts")
		}
		return data.Hosts, nil
	}

	if ue, ok := err.(*url.Error); ok && ue.Timeout() {
		return nil, ErrHostScoreTimeout
	}

	return nil, utils.AddContext(err, "falied to fetch hosts")
}

// Score calculates the total score of a HostScoreBreakdown.
func (hsb HostScoreBreakdown) Score() float64 {
	return hsb.AgeScore *
		hsb.BenchmarksScore *
		hsb.CollateralScore *
		hsb.ContractsScore *
		hsb.InteractionsScore *
		hsb.LatencyScore *
		hsb.PricesScore *
		hsb.StorageScore *
		hsb.UptimeScore *
		hsb.VersionScore
}
