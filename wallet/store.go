package wallet

import (
	"bytes"
	"database/sql"
	"errors"
	"sync"
	"time"

	"slices"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/wallet"
	"go.uber.org/zap"
)

// A DBStore stores wallet state in a MySQL database.
type DBStore struct {
	tip           types.ChainIndex
	seed          *[32]byte
	addresses     map[types.Address]uint64
	sces          map[types.SiacoinOutputID]types.SiacoinElement
	greatestIndex uint64
	mu            sync.Mutex
	db            *sql.DB
	tx            *sql.Tx
	log           *zap.Logger
	lastCommitted time.Time
}

// save saves the DBStore to disk.
func (s *DBStore) save() error {
	if s.tx == nil {
		return errors.New("there is no transaction")
	}

	_, err := s.tx.Exec(`
		REPLACE INTO wt_tip (id, height, bid)
		VALUES (1, ?, ?)
	`, s.tip.Height, s.tip.ID[:])
	if err != nil {
		s.tx.Rollback()
		s.tx, _ = s.db.Begin()
		return utils.AddContext(err, "couldn't update tip")
	}

	err = s.tx.Commit()
	if err != nil {
		return utils.AddContext(err, "couldn't commit transaction")
	}

	s.tx, err = s.db.Begin()
	s.lastCommitted = time.Now()
	return err
}

// load loads the DBStore from disk.
func (s *DBStore) load() error {
	var height uint64
	id := make([]byte, 32)
	err := s.db.QueryRow(`
		SELECT height, bid
		FROM wt_tip
		WHERE id = 1
	`).Scan(&height, &id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return utils.AddContext(err, "couldn't load tip")
	}
	s.tip.Height = height
	copy(s.tip.ID[:], id)

	rows, err := s.db.Query(`
		SELECT scoid, bytes
		FROM wt_sces
	`)
	if err != nil {
		return utils.AddContext(err, "couldn't query SC elements")
	}

	var b []byte
	for rows.Next() {
		if err := rows.Scan(&id, &b); err != nil {
			rows.Close()
			return utils.AddContext(err, "couldn't scan SC element")
		}
		var scoid types.SiacoinOutputID
		copy(scoid[:], id)
		d := types.NewBufDecoder(b)
		var sce types.SiacoinElement
		sce.DecodeFrom(d)
		if d.Err() != nil {
			rows.Close()
			return utils.AddContext(err, "couldn't decode SC element")
		}
		s.sces[scoid] = sce
	}
	rows.Close()

	rows, err = s.db.Query(`
		SELECT id, addr
		FROM wt_addrs
	`)
	if err != nil {
		return utils.AddContext(err, "couldn't query addresses")
	}

	for rows.Next() {
		var index uint64
		addr := make([]byte, 32)
		if err := rows.Scan(&index, &addr); err != nil {
			rows.Close()
			return utils.AddContext(err, "couldn't scan address")
		}
		s.addresses[types.Address(addr)] = index
		if index > s.greatestIndex {
			s.greatestIndex = index
		}
	}
	rows.Close()

	s.tx, err = s.db.Begin()
	return err
}

// lastSyncedIndex returns the last synced index of the DBStore.
func (s *DBStore) lastSyncedIndex() types.ChainIndex {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.tip
}

// unspentSiacoinElements returns the list of the DBStore's utxos.
func (s *DBStore) unspentSiacoinElements() (utxos []types.SiacoinElement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.getSiacoinElements()
}

// resetChainState cleans up the database tables.
func (s *DBStore) resetChainState() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tx == nil {
		var err error
		s.tx, err = s.db.Begin()
		if err != nil {
			return err
		}
	}

	_, err := s.tx.Exec("DELETE FROM wt_sces")
	if err != nil {
		return err
	}

	_, err = s.tx.Exec("DELETE FROM wt_addrs")
	if err != nil {
		return err
	}

	s.tip = types.ChainIndex{}
	s.greatestIndex = 0
	s.addresses = make(map[types.Address]uint64)
	s.sces = make(map[types.SiacoinOutputID]types.SiacoinElement)
	return s.save()
}

// getSiacoinElements returns the list of the DBStore's utxos.
func (s *DBStore) getSiacoinElements() (sces []types.SiacoinElement) {
	for _, sce := range s.sces {
		sce.StateElement = types.StateElement{
			LeafIndex:   sce.StateElement.LeafIndex,
			MerkleProof: slices.Clone(sce.StateElement.MerkleProof),
		}
		sces = append(sces, sce)
	}

	return
}

// updateSiacoinElements updates the SC elements in the database.
func (s *DBStore) updateSiacoinElements(sces []types.SiacoinElement) error {
	for _, sce := range sces {
		sce.StateElement.MerkleProof = slices.Clone(sce.StateElement.MerkleProof)
		s.sces[types.SiacoinOutputID(sce.ID)] = sce
		var buf bytes.Buffer
		e := types.NewEncoder(&buf)
		sce.EncodeTo(e)
		e.Flush()
		_, err := s.tx.Exec(`
			INSERT INTO wt_sces (scoid, bytes)
			VALUES (?, ?) AS new
			ON DUPLICATE KEY UPDATE
				bytes = new.bytes
		`, sce.ID[:], buf.Bytes())
		if err != nil {
			s.log.Error("couldn't add SC output", zap.Error(err))
			return err
		}
	}
	return nil
}

// removeSiacoinElements removes the specified SC elements in the database.
func (s *DBStore) removeSiacoinElements(sces []types.SiacoinElement) error {
	for _, sce := range sces {
		delete(s.sces, types.SiacoinOutputID(sce.ID))
		_, err := s.tx.Exec(`
			DELETE FROM wt_sces
			WHERE scoid = ?
		`, sce.ID[:])
		if err != nil {
			s.log.Error("couldn't delete SC output", zap.Error(err))
			return err
		}
	}
	return nil
}

// updateWalletSiacoinElementProofs updates the proofs of the Siacoin state elements.
func (s *DBStore) updateWalletSiacoinElementProofs(updater wallet.ProofUpdater) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sces := s.getSiacoinElements()
	for i := range sces {
		updater.UpdateElementProof(&sces[i].StateElement)
	}

	return s.updateSiacoinElements(sces)
}

// logEvent logs an incoming event depending on its type.
func (s *DBStore) logEvent(event wallet.Event) {
	inflow, outflow := event.SiacoinInflow(), event.SiacoinOutflow()
	desc := "found new "
	switch event.Type {
	case wallet.EventTypeV1Transaction:
		desc += "v1 transaction"
	case wallet.EventTypeV2Transaction:
		desc += "v2 transaction"
	case wallet.EventTypeV1ContractResolution, wallet.EventTypeV2ContractResolution:
	default:
		desc += "unknown event"
	}
	desc += ", id: " + event.ID.String()
	if !inflow.IsZero() {
		desc += ", inflow: " + inflow.String()
	}
	if !outflow.IsZero() {
		desc += ", outflow: " + outflow.String()
	}
	s.log.Info(desc)
}

// walletApplyIndex applies the chain index to the wallet.
func (s *DBStore) walletApplyIndex(created, spent []types.SiacoinElement, events []wallet.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.removeSiacoinElements(spent); err != nil {
		return utils.AddContext(err, "failed to delete Siacoin elements")
	}

	if err := s.updateSiacoinElements(created); err != nil {
		return utils.AddContext(err, "failed to create Siacoin elements")
	}

	for _, event := range events {
		s.logEvent(event)
	}

	return nil
}

// walletRevertIndex reverts the chain index application.
func (s *DBStore) walletRevertIndex(removed, unspent []types.SiacoinElement) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.removeSiacoinElements(removed); err != nil {
		return utils.AddContext(err, "failed to delete Siacoin elements")
	}

	if err := s.updateSiacoinElements(unspent); err != nil {
		return utils.AddContext(err, "failed to create Siacoin elements")
	}

	return nil
}

// updateChainState applies the chain manager updates.
func (s *DBStore) updateChainState(index types.ChainIndex, mayCommit bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tip = index
	if mayCommit || time.Since(s.lastCommitted) >= 3*time.Second {
		return s.save()
	}

	return nil
}

// addressFound returns true if the provided address is known to the wallet.
func (s *DBStore) addressFound(addr types.Address) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, found := s.addresses[addr]
	return found
}

// insertAddress generates a new address and inserts it into the database.
func (s *DBStore) insertAddress(index uint64) (addr types.Address, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	addr = types.StandardUnlockHash(wallet.KeyFromSeed(s.seed, index).PublicKey())
	s.addresses[addr] = index
	if index > s.greatestIndex {
		s.greatestIndex = index
	}

	_, err = s.tx.Exec(`
		REPLACE INTO wt_addrs (id, addr)
		VALUES (?, ?)
	`, index, addr[:])
	if err != nil {
		return addr, utils.AddContext(err, "couldn't insert address")
	}

	return addr, s.save()
}

// rootAddress returns the first address derived from the wallet seed.
func (s *DBStore) rootAddress() types.Address {
	return types.StandardUnlockHash(wallet.KeyFromSeed(s.seed, 0).PublicKey())
}

// close saves the changes to disk.
func (s *DBStore) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tx != nil {
		s.tx.Commit()
	}
}

// NewDBStore returns a new DBStore.
func NewDBStore(db *sql.DB, seedPhrase string, logger *zap.Logger) (*DBStore, types.ChainIndex, error) {
	var seed [32]byte
	if err := wallet.SeedFromPhrase(&seed, seedPhrase); err != nil {
		return nil, types.ChainIndex{}, err
	}

	s := &DBStore{
		seed:      &seed,
		sces:      make(map[types.SiacoinOutputID]types.SiacoinElement),
		addresses: make(map[types.Address]uint64),
		db:        db,
		log:       logger,
	}

	if err := s.load(); err != nil {
		s.log.Error("couldn't load wallet", zap.Error(err))
		return nil, types.ChainIndex{}, err
	}

	// TODO scan

	return s, s.tip, nil
}
