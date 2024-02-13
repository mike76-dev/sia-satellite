package server

import (
	"fmt"
	"strings"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
	"go.sia.tech/core/types"
	"go.sia.tech/jape"
)

func (s *server) managerAveragesHandler(jc jape.Context) {
	var currency string
	if jc.DecodeParam("currency", &currency) != nil {
		return
	}
	currency = strings.ToUpper(currency)
	if currency == "" {
		currency = "SC"
	}

	rate := float64(1.0)
	var err error
	if currency != "SC" {
		rate, err = s.m.GetSiacoinRate(currency)
		if jc.Check("couldn't get exchange rate", err) != nil {
			return
		}
	}

	var ha api.HostAverages
	ha.HostAverages = s.m.GetAverages()
	ha.Rate = rate

	jc.Encode(ha)
}

func (s *server) managerRentersHandler(jc jape.Context) {
	renters := s.m.Renters()

	r := api.RentersGET{
		Renters: make([]api.Renter, 0, len(renters)),
	}

	for _, renter := range renters {
		r.Renters = append(r.Renters, api.Renter{
			Email:     renter.Email,
			PublicKey: renter.PublicKey,
		})
	}

	jc.Encode(r)
}

func (s *server) managerRenterHandler(jc jape.Context) {
	var key types.PublicKey
	if jc.DecodeParam("publickey", &key) != nil {
		return
	}

	renter, err := s.m.GetRenter(key)
	if jc.Check("renter not found", err) != nil {
		return
	}

	jc.Encode(renter)
}

func (s *server) managerBalanceHandler(jc jape.Context) {
	var key types.PublicKey
	if jc.DecodeParam("publickey", &key) != nil {
		return
	}

	renter, err := s.m.GetRenter(key)
	if jc.Check("renter not found", err) != nil {
		return
	}

	ub, err := s.m.GetBalance(renter.Email)
	if jc.Check("unable to get balance", err) != nil {
		return
	}

	jc.Encode(ub)
}

func (s *server) managerContractsHandler(jc jape.Context) {
	var pk string
	if jc.DecodeParam("publickey", &pk) != nil {
		return
	}

	if pk == "" {
		contracts := s.m.Contracts()
		oldContracts := s.m.OldContracts()
		jc.Encode(s.getContracts(contracts, oldContracts, modules.Renter{}))
		return
	}

	var key types.PublicKey
	if jc.Check("couldn't unmarshal key", key.UnmarshalText([]byte(pk))) != nil {
		return
	}

	renter, err := s.m.GetRenter(key)
	if jc.Check("renter not found", err) != nil {
		return
	}

	contracts := s.m.ContractsByRenter(key)
	oldContracts := s.m.OldContractsByRenter(key)

	jc.Encode(s.getContracts(contracts, oldContracts, renter))
}

func (s *server) getContracts(contracts, oldContracts []modules.RenterContract, renter modules.Renter) api.RenterContracts {
	var rc api.RenterContracts
	currentBlockHeight := s.cm.Tip().Height

	for _, c := range contracts {
		// Fetch host address.
		var netAddress string
		hdbe, exists, _ := s.m.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.Settings.NetAddress
		}

		// Build the contract.
		contract := api.RenterContract{
			BadContract:         c.Utility.BadContract,
			DownloadSpending:    c.DownloadSpending,
			EndHeight:           c.EndHeight,
			Fees:                c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee),
			FundAccountSpending: c.FundAccountSpending,
			GoodForUpload:       c.Utility.GoodForUpload,
			GoodForRenew:        c.Utility.GoodForRenew,
			RenterPublicKey:     c.RenterPublicKey,
			HostPublicKey:       c.HostPublicKey,
			HostVersion:         hdbe.Settings.Version,
			ID:                  c.ID,
			LastTransaction:     c.Transaction,
			NetAddress:          netAddress,
			MaintenanceSpending: c.MaintenanceSpending,
			RenterFunds:         c.RenterFunds,
			Size:                c.Size(),
			StartHeight:         c.StartHeight,
			StorageSpending:     c.StorageSpending,
			TotalCost:           c.TotalCost,
			UploadSpending:      c.UploadSpending,
		}

		// Determine contract status.
		refreshed := s.m.RefreshedContract(c.ID)
		active := c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
		passive := !c.Utility.GoodForUpload && c.Utility.GoodForRenew && !refreshed
		disabledContract := !active && !passive && !refreshed

		// A contract can either be active, passive, refreshed, or disabled.
		statusErr := active && passive && refreshed || active && refreshed || active && passive || passive && refreshed
		if statusErr {
			fmt.Println("CRITICAL: Contract has multiple status types, this should never happen")
		} else if active {
			rc.ActiveContracts = append(rc.ActiveContracts, contract)
		} else if passive {
			rc.PassiveContracts = append(rc.PassiveContracts, contract)
		} else if refreshed {
			rc.RefreshedContracts = append(rc.RefreshedContracts, contract)
		} else if disabledContract {
			rc.DisabledContracts = append(rc.DisabledContracts, contract)
		}
	}

	// Process old contracts.
	for _, c := range oldContracts {
		var size uint64
		if len(c.Transaction.FileContractRevisions) != 0 {
			size = c.Transaction.FileContractRevisions[0].Filesize
		}

		// Fetch host address.
		var netAddress string
		hdbe, exists, _ := s.m.Host(c.HostPublicKey)
		if exists {
			netAddress = hdbe.Settings.NetAddress
		}

		// Build the contract.
		contract := api.RenterContract{
			BadContract:         c.Utility.BadContract,
			DownloadSpending:    c.DownloadSpending,
			EndHeight:           c.EndHeight,
			Fees:                c.TxnFee.Add(c.SiafundFee).Add(c.ContractFee),
			FundAccountSpending: c.FundAccountSpending,
			GoodForUpload:       c.Utility.GoodForUpload,
			GoodForRenew:        c.Utility.GoodForRenew,
			RenterPublicKey:     c.RenterPublicKey,
			HostPublicKey:       c.HostPublicKey,
			HostVersion:         hdbe.Settings.Version,
			ID:                  c.ID,
			LastTransaction:     c.Transaction,
			MaintenanceSpending: c.MaintenanceSpending,
			NetAddress:          netAddress,
			RenterFunds:         c.RenterFunds,
			Size:                size,
			StartHeight:         c.StartHeight,
			StorageSpending:     c.StorageSpending,
			TotalCost:           c.TotalCost,
			UploadSpending:      c.UploadSpending,
		}

		// Determine contract status.
		refreshed := s.m.RefreshedContract(c.ID)
		endHeightInPast := c.EndHeight < uint64(currentBlockHeight)
		if renter.Email != "" {
			endHeightInPast = endHeightInPast || c.StartHeight < renter.CurrentPeriod
		}
		expiredContract := endHeightInPast && !refreshed
		expiredRefreshed := endHeightInPast && refreshed
		refreshedContract := refreshed && !endHeightInPast
		disabledContract := !refreshed && !endHeightInPast

		// A contract can only be refreshed, disabled, expired, or expired refreshed.
		if expiredContract {
			rc.ExpiredContracts = append(rc.ExpiredContracts, contract)
		} else if expiredRefreshed {
			rc.ExpiredRefreshedContracts = append(rc.ExpiredRefreshedContracts, contract)
		} else if refreshedContract {
			rc.RefreshedContracts = append(rc.RefreshedContracts, contract)
		} else if disabledContract {
			rc.DisabledContracts = append(rc.DisabledContracts, contract)
		}
	}

	return rc
}

func (s *server) managerPreferencesHandler(jc jape.Context) {
	email, threshold := s.m.GetEmailPreferences()
	ep := api.EmailPreferences{
		Email:         email,
		WarnThreshold: threshold,
	}

	jc.Encode(ep)
}

func (s *server) managerUpdatePreferencesHandler(jc jape.Context) {
	var ep api.EmailPreferences
	if jc.Decode(&ep) != nil {
		return
	}

	if jc.Check("failed to change preferences", s.m.SetEmailPreferences(ep.Email, ep.WarnThreshold)) != nil {
		return
	}
}

func (s *server) managerPricesHandler(jc jape.Context) {
	jc.Encode(modules.StaticPricing)
}

func (s *server) managerUpdatePricesHandler(jc jape.Context) {
	var prices modules.Pricing
	if jc.Decode(&prices) != nil {
		return
	}

	if jc.Check("failed to change prices", s.m.UpdatePrices(prices)) != nil {
		return
	}
}

func (s *server) managerMaintenanceHandler(jc jape.Context) {
	jc.Encode(struct {
		Maintenance bool `json:"maintenance"`
	}{Maintenance: s.m.Maintenance()})
}

func (s *server) managerSetMaintenanceHandler(jc jape.Context) {
	var start struct {
		Start bool `json:"start"`
	}
	if jc.Decode(&start) != nil {
		return
	}

	if jc.Check("couldn't set maintenance flag", s.m.StartMaintenance(start.Start)) != nil {
		return
	}
}
