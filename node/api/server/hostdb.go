package server

import (
	"fmt"
	"net/http"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
	"go.sia.tech/core/types"
	"go.sia.tech/jape"
)

func (s *server) hostdbHandler(jc jape.Context) {
	isc, bh, err := s.m.InitialScanComplete()
	if jc.Check("failed to get initial scan status", err) != nil {
		return
	}

	jc.Encode(api.HostdbGET{
		BlockHeight:         bh,
		InitialScanComplete: isc,
	})
}

func (s *server) hostdbActiveHandler(jc jape.Context) {
	var numHosts uint64
	if jc.DecodeForm("numHosts", &numHosts) != nil {
		return
	}

	hosts, err := s.m.ActiveHosts()
	if jc.Check("unable to get active hosts", err) != nil {
		return
	}

	if numHosts == 0 || numHosts > uint64(len(hosts)) {
		numHosts = uint64(len(hosts))
	}

	var extendedHosts []api.ExtendedHostDBEntry
	for _, host := range hosts {
		extendedHosts = append(extendedHosts, api.ExtendedHostDBEntry{
			HostDBEntry:     host,
			PublicKeyString: host.PublicKey.String(),
		})
	}

	jc.Encode(api.HostdbHostsGET{
		Hosts: extendedHosts[:numHosts],
	})
}

func (s *server) hostdbAllHandler(jc jape.Context) {
	var numHosts uint64
	if jc.DecodeForm("numHosts", &numHosts) != nil {
		return
	}

	hosts, err := s.m.AllHosts()
	if jc.Check("unable to get active hosts", err) != nil {
		return
	}

	if numHosts == 0 || numHosts > uint64(len(hosts)) {
		numHosts = uint64(len(hosts))
	}

	var extendedHosts []api.ExtendedHostDBEntry
	for _, host := range hosts {
		extendedHosts = append(extendedHosts, api.ExtendedHostDBEntry{
			HostDBEntry:     host,
			PublicKeyString: host.PublicKey.String(),
		})
	}

	jc.Encode(api.HostdbHostsGET{
		Hosts: extendedHosts[:numHosts],
	})
}

func (s *server) hostdbHostHandler(jc jape.Context) {
	var pk types.PublicKey
	if jc.DecodeParam("publickey", &pk) != nil {
		return
	}

	entry, exists, err := s.m.Host(pk)
	if jc.Check("unable to get host", err) != nil {
		return
	}
	if !exists {
		jc.Error(fmt.Errorf("requested host does not exist"), http.StatusBadRequest)
		return
	}

	breakdown, err := s.m.ScoreBreakdown(entry)
	if jc.Check("error calculating score breakdown", err) != nil {
		return
	}

	// Extend the hostdb entry  to have the public key string.
	extendedEntry := api.ExtendedHostDBEntry{
		HostDBEntry:     entry,
		PublicKeyString: entry.PublicKey.String(),
	}
	jc.Encode(api.HostdbHostGET{
		Entry:          extendedEntry,
		ScoreBreakdown: breakdown,
	})
}

func (s *server) hostdbFilterModeHandler(jc jape.Context) {
	fm, hostMap, netAddresses, err := s.m.Filter()
	if jc.Check("unable to get filter mode", err) != nil {
		return
	}

	var hosts []string
	for key := range hostMap {
		hosts = append(hosts, key)
	}

	jc.Encode(api.HostdbFilterModeGET{
		FilterMode:   fm.String(),
		Hosts:        hosts,
		NetAddresses: netAddresses,
	})
}

func (s *server) hostdbSetFilterModeHandler(jc jape.Context) {
	var params api.HostdbFilterModePOST
	if jc.Decode(&params) != nil {
		return
	}

	var fm modules.FilterMode
	if jc.Check("unable to load filter mode from string", fm.FromString(params.FilterMode)) != nil {
		return
	}

	if jc.Check("failed to set the list mode", s.m.SetFilterMode(fm, params.Hosts, params.NetAddresses)) != nil {
		return
	}
}
