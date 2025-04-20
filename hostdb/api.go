package hostdb

import (
	"slices"

	"go.sia.tech/core/types"
	renterd "go.sia.tech/renterd/api"
)

// BenchmarkBasis is used to convert an index to a string.
var BenchmarkBasis = []string{"global", "eu", "us", "ap"}

// Host combines the relevant attributes of a host.
type Host struct {
	PublicKey     types.PublicKey `json:"publicKey"`
	NetAddress    string          `json:"netAddress"`
	ContractPrice types.Currency  `json:"contractPrice"`
	StoragePrice  types.Currency  `json:"storagePrice"`
	IngressPrice  types.Currency  `json:"ingressPrice"`
	EgressPrice   types.Currency  `json:"egressPrice"`
	Country       string          `json:"country"`
	Latency       int64           `json:"latency"`
	UploadSpeed   float64         `json:"uploadSpeed"`
	DownloadSpeed float64         `json:"downloadSpeed"`
	Score         float64         `json:"score"`
}

// GetHosts returns a list of hosts sorted by their score.
// The blacklist and the whitelist are also considered.
func (hdb *HostDB) GetHosts(config renterd.ContractsConfig, settings RenterSettings, basis ScoreBasis, blacklist, whitelist []types.PublicKey) (hosts []Host) {
	// Map out blacklist and whitelist for convenience.
	bl := make(map[types.PublicKey]struct{})
	for _, pk := range blacklist {
		bl[pk] = struct{}{}
	}

	wl := make(map[types.PublicKey]struct{})
	for _, pk := range whitelist {
		wl[pk] = struct{}{}
	}

	// Create a list of all hosts considering the blacklist and the whitelist.
	hdb.mu.Lock()
	var allHosts []HostDBEntry
	for pk, host := range hdb.hosts {
		// If the host is whitelisted, it must be included.
		if _, ok := wl[pk]; ok {
			allHosts = append(allHosts, *host)
			continue
		}

		// The whitelist overrules the blacklist.
		if len(wl) > 0 {
			continue
		}

		// Exclude blacklisted hosts.
		if _, ok := bl[pk]; !ok {
			allHosts = append(allHosts, *host)
		}
	}

	// Calculate the score of each host.
	for i := range allHosts {
		allHosts[i].Score = calculateScore(allHosts[i], config, settings, basis)
	}

	// Sort the hosts by their score.
	slices.SortStableFunc(allHosts, func(a, b HostDBEntry) int {
		return int(b.Score.TotalScore - a.Score.TotalScore)
	})

	for _, h := range allHosts {
		var latency int64
		var uploadSpeed, downloadSpeed float64
		if basis == ScoreBasisGlobal {
			latency, uploadSpeed, downloadSpeed = calculateGlobalBenchmarkData(h)
		} else {
			latency, uploadSpeed, downloadSpeed = calculateBenchmarkData(h, basis)
		}

		hosts = append(hosts, Host{
			PublicKey:     h.PublicKey,
			NetAddress:    h.NetAddress,
			ContractPrice: h.Settings.Prices.ContractPrice,
			StoragePrice:  h.Settings.Prices.StoragePrice,
			IngressPrice:  h.Settings.Prices.IngressPrice,
			EgressPrice:   h.Settings.Prices.EgressPrice,
			Country:       h.Country,
			Latency:       latency,
			UploadSpeed:   uploadSpeed,
			DownloadSpeed: downloadSpeed,
			Score:         h.Score.TotalScore,
		})
	}

	return
}

// GetHost returns the host with the given public key.
func (hdb *HostDB) GetHost(pk types.PublicKey, config renterd.ContractsConfig, settings RenterSettings, basis ScoreBasis) (host Host, err error) {
	h, err := hdb.Host(pk)
	if err != nil {
		return Host{}, err
	}

	var latency int64
	var uploadSpeed, downloadSpeed float64
	if basis == ScoreBasisGlobal {
		latency, uploadSpeed, downloadSpeed = calculateGlobalBenchmarkData(*h)
	} else {
		latency, uploadSpeed, downloadSpeed = calculateBenchmarkData(*h, basis)
	}

	return Host{
		PublicKey:     h.PublicKey,
		NetAddress:    h.NetAddress,
		ContractPrice: h.Settings.Prices.ContractPrice,
		StoragePrice:  h.Settings.Prices.StoragePrice,
		IngressPrice:  h.Settings.Prices.IngressPrice,
		EgressPrice:   h.Settings.Prices.EgressPrice,
		Country:       h.Country,
		Latency:       latency,
		UploadSpeed:   uploadSpeed,
		DownloadSpeed: downloadSpeed,
		Score:         calculateScore(*h, config, settings, basis).TotalScore,
	}, nil
}
