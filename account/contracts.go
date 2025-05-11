package account

import (
	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
	renterd "go.sia.tech/renterd/api"
)

// Contract keeps the metadata of a storage contract.
type Contract struct {
	renterd.ContractMetadata
	RenterKey types.PublicKey `json:"-"`
}

// loadContracts loads the contracts from the database.
func (am *AccountManager) loadContracts() error {
	rows, err := am.db.Query(`
		SELECT
			id,
			renter_key,
			host_key,
			proof_height,
			renewed_from,
			revision_height,
			revision_number,
			contract_size,
			start_height,
			contract_state,
			usability,
			window_start,
			window_end,
			contract_price,
			renter_funds,
			deletions,
			fund_account,
			sector_roots,
			uploads,
			email
		FROM am_contracts
	`)
	if err != nil {
		return utils.AddContext(err, "couldn't query contracts")
	}
	defer rows.Close()

	for rows.Next() {
		fcid := make([]byte, 32)
		rk := make([]byte, 32)
		hk := make([]byte, 32)
		rf := make([]byte, 32)
		var ph, rh, rn, size, sh, ws, we uint64
		var cs, u, email string
		var cp, funds, deletions, fa, sr, uploads []byte
		if err := rows.Scan(
			&fcid,
			&rk,
			&hk,
			&ph,
			&rf,
			&rh,
			&rn,
			&size,
			&sh,
			&cs,
			&u,
			&ws,
			&we,
			&cp,
			&funds,
			&deletions,
			&fa,
			&sr,
			&uploads,
			&email,
		); err != nil {
			return utils.AddContext(err, "couldn't decode contract")
		}

		acc, ok := am.accounts[email]
		if !ok {
			return ErrUserNotFound
		}

		fc := Contract{
			ContractMetadata: renterd.ContractMetadata{
				ID:             types.FileContractID(fcid),
				HostKey:        types.PublicKey(hk),
				V2:             true,
				ProofHeight:    ph,
				RenewedFrom:    types.FileContractID(rf),
				RevisionHeight: rh,
				RevisionNumber: rn,
				Size:           size,
				StartHeight:    sh,
				State:          cs,
				Usability:      u,
				WindowStart:    ws,
				WindowEnd:      we,
			},
			RenterKey: types.PublicKey(rk),
		}

		d := types.NewBufDecoder(cp)
		(*types.V2Currency)(&fc.ContractPrice).DecodeFrom(d)
		if err := d.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode contract price")
		}
		d = types.NewBufDecoder(funds)
		(*types.V2Currency)(&fc.InitialRenterFunds).DecodeFrom(d)
		if err := d.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode initial renter funds")
		}
		d = types.NewBufDecoder(deletions)
		(*types.V2Currency)(&fc.Spending.Deletions).DecodeFrom(d)
		if err := d.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode deletions")
		}
		d = types.NewBufDecoder(fa)
		(*types.V2Currency)(&fc.Spending.FundAccount).DecodeFrom(d)
		if err := d.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode fund accounts")
		}
		d = types.NewBufDecoder(sr)
		(*types.V2Currency)(&fc.Spending.SectorRoots).DecodeFrom(d)
		if err := d.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode sector roots")
		}
		d = types.NewBufDecoder(uploads)
		(*types.V2Currency)(&fc.Spending.Uploads).DecodeFrom(d)
		if err := d.Err(); err != nil {
			return utils.AddContext(err, "couldn't decode uploads")
		}

		acc.contracts[fc.HostKey] = fc
	}

	return nil
}
