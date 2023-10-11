package contractor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"go.sia.tech/core/types"
	"go.sia.tech/renterd/object"
)

// healthCutoff represents the health of a slab, beyond which
// it shall be repaired.
const healthCutoff = 0.75

type (
	// slabInfo combines a slab with some other information.
	slabInfo struct {
		modules.Slab
		renterKey types.PublicKey
		health    float64
	}
)

// migrator manages slab migrations.
type migrator struct {
	contractor          *Contractor
	parallelSlabs       uint64
	maintenanceFinished chan struct{}

	mu                 sync.Mutex
	migrating          bool
	migratingLastStart time.Time
}

// newMigrator returns an initialized migrator.
func newMigrator(c *Contractor, parallelSlabs uint64) *migrator {
	return &migrator{
		contractor:          c,
		parallelSlabs:       parallelSlabs,
		maintenanceFinished: make(chan struct{}, 1),
	}
}

// signalMaintenanceFinished signals that the contract maintenance is finished.
func (m *migrator) signalMaintenanceFinished() {
	select {
	case m.maintenanceFinished <- struct{}{}:
	default:
	}
}

// tryPerformMigrations tries to start the migration process.
func (m *migrator) tryPerformMigrations() {
	m.mu.Lock()
	if m.migrating {
		m.mu.Unlock()
		return
	}
	m.migrating = true
	m.migratingLastStart = time.Now()

	err := m.contractor.tg.Add()
	if err != nil {
		m.contractor.log.Println("ERROR: couldn't add thread:", err)
		m.migrating = false
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	go func() {
		defer m.contractor.tg.Done()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.performMigrations(ctx)
		m.mu.Lock()
		m.migrating = false
		m.mu.Unlock()
	}()
}

// performMigrations performs the slab migrations.
func (m *migrator) performMigrations(ctx context.Context) {
	m.contractor.log.Println("INFO: performing migrations")

	// Prepare a channel to push work.
	type job struct {
		slabInfo
		slabIdx   int
		batchSize int
	}
	jobs := make(chan job)

	// Prepare a response channel.
	type jobResponse struct {
		renterKey types.PublicKey
		success   bool
	}
	responses := make(chan jobResponse)
	var threadsLeft uint64

	// Prepare a map of migrated slabs.
	type migratedSlabs struct {
		slabs map[types.PublicKey]uint64
		mu    sync.Mutex
	}
	var migrated migratedSlabs
	migrated.slabs = make(map[types.PublicKey]uint64)

	go func() {
		for {
			if atomic.LoadUint64(&threadsLeft) == 0 {
				break
			}
			select {
			case <-m.contractor.tg.StopChan():
				return
			case resp := <-responses:
				if resp.success {
					migrated.mu.Lock()
					migrated.slabs[resp.renterKey]++
					migrated.mu.Unlock()
				}
			default:
			}
		}
	}()

	defer func() {
		close(jobs)
		for {
			if atomic.LoadUint64(&threadsLeft) == 0 {
				break
			}
		}

		for rpk, num := range migrated.slabs {
			// Deduct from the account balance.
			renter, err := m.contractor.m.GetRenter(rpk)
			if err != nil {
				m.contractor.log.Println("ERROR: couldn't get the renter:", err)
				continue
			}
			ub, err := m.contractor.m.GetBalance(renter.Email)
			if err != nil {
				m.contractor.log.Println("ERROR: couldn't get renter balance:", err)
				continue
			}
			fee := float64(num) * modules.MigrateSlabFee
			ub.Balance -= fee
			err = m.contractor.m.UpdateBalance(renter.Email, ub)
			if err != nil {
				m.contractor.log.Println("ERROR: couldn't update renter balance:", err)
				continue
			}

			// Update the spendings.
			us, err := m.contractor.m.GetSpendings(renter.Email)
			if err != nil {
				m.contractor.log.Println("ERROR: couldn't retrieve renter spendings:", err)
				continue
			}
			us.CurrentOverhead += fee
			us.CurrentSlabsMigrated += num

			err = m.contractor.m.UpdateSpendings(renter.Email, us)
			if err != nil {
				m.contractor.log.Println("ERROR: couldn't update renter spendings:", err)
			}
		}
	}()

	// Run migrations.
	for i := uint64(0); i < m.parallelSlabs; i++ {
		atomic.AddUint64(&threadsLeft, 1)
		go func() {
			defer atomic.AddUint64(&threadsLeft, ^uint64(0))

			for j := range jobs {
				slab, offset, length, err := m.contractor.getSlab(j.Key)
				if err != nil {
					m.contractor.log.Printf("ERROR: failed to fetch slab for migration %d/%d, health: %v, err: %v\n", j.slabIdx+1, j.batchSize, j.health, err)
					continue
				}

				// Migrate the slab.
				err = m.contractor.migrateSlab(ctx, j.renterKey, &slab)
				if err != nil {
					m.contractor.log.Printf("ERROR: failed to migrate slab %d/%d, health: %v, err: %v\n", j.slabIdx+1, j.batchSize, j.health, err)
				} else {
					m.contractor.log.Printf("INFO: successfully migrated slab %d/%d\n", j.slabIdx+1, j.batchSize)

					// Update the slab in the database.
					key, _ := convertEncryptionKey(slab.Key)
					s := modules.Slab{
						Key:       key,
						MinShards: slab.MinShards,
						Offset:    uint64(offset),
						Length:    uint64(length),
					}
					for _, shard := range slab.Shards {
						var ss modules.Shard
						copy(ss.Host[:], shard.Host[:])
						copy(ss.Root[:], shard.Root[:])
						s.Shards = append(s.Shards, ss)
					}
					err = m.contractor.updateSlab(s)
					if err != nil {
						m.contractor.log.Printf("ERROR: failed to update slab %d/%d, err: %v\n", j.slabIdx+1, j.batchSize, err)
					}
				}

				// Send a response.
				select {
				case responses <- jobResponse{j.renterKey, err == nil}:
				}
			}
		}()
	}
	var toMigrate []slabInfo

	// Ignore a potential signal before the first iteration of the 'OUTER' loop.
	select {
	case <-m.maintenanceFinished:
	default:
	}

OUTER:
	for {
		// Fetch slabs for migration.
		toRepair, err := m.contractor.managedCheckFileHealth()
		if err != nil {
			return
		}

		// Iterate through the slabs and exclude those where migrations can't
		// be paid for due to an insufficient balance.
		var toMigrateNew []slabInfo
		numSlabs := make(map[types.PublicKey]int)
		for _, slab := range toRepair {
			numSlabs[slab.renterKey]++
		}
		for rpk, num := range numSlabs {
			renter, err := m.contractor.m.GetRenter(rpk)
			if err != nil {
				m.contractor.log.Println("ERROR: couldn't get the renter:", err)
				numSlabs[rpk] = 0
				continue
			}
			ub, err := m.contractor.m.GetBalance(renter.Email)
			if err != nil {
				m.contractor.log.Println("ERROR: couldn't get renter balance:", err)
				numSlabs[rpk] = 0
				continue
			}
			fee := float64(num) * modules.MigrateSlabFee
			if !ub.Subscribed && ub.Balance < fee {
				m.contractor.log.Println("WARN: skipping slab migrations due to an insufficient account balance:", renter.Email)
				numSlabs[rpk] = 0
			}
		}
		for _, slab := range toRepair {
			if numSlabs[slab.renterKey] > 0 {
				toMigrateNew = append(toMigrateNew, slab)
			}
		}

		// Merge toMigrateNew with toMigrate.
		// NOTE: when merging, we remove all slabs from toMigrate that don't
		// require migration anymore. However, slabs that have been in toMigrate
		// before will be repaired before any new slabs. This is to prevent
		// starvation.
		migrateNewMap := make(map[types.Hash256]*slabInfo)
		for i, slab := range toMigrateNew {
			migrateNewMap[slab.Key] = &toMigrateNew[i]
		}
		removed := 0
		for i := 0; i < len(toMigrate)-removed; {
			slab := toMigrate[i]
			if _, exists := migrateNewMap[slab.Key]; exists {
				delete(migrateNewMap, slab.Key) // Delete from map to leave only new slabs.
				i++
			} else {
				toMigrate[i] = toMigrate[len(toMigrate)-1-removed]
				removed++
			}
		}
		toMigrate = toMigrate[:len(toMigrate)-removed]
		for _, slab := range migrateNewMap {
			toMigrate = append(toMigrate, *slab)
		}

		// Sort the newly added slabs by health.
		newSlabs := toMigrate[len(toMigrate)-len(migrateNewMap):]
		sort.Slice(newSlabs, func(i, j int) bool {
			return newSlabs[i].health < newSlabs[j].health
		})
		migrateNewMap = nil // Free map.

		// Return if there are no slabs to migrate.
		if len(toMigrate) == 0 {
			return
		}

		for i, slab := range toMigrate {
			select {
			case <-m.contractor.tg.StopChan():
				return
			case <-m.maintenanceFinished:
				m.contractor.log.Println("INFO: migrations interrupted - updating slabs for migration")
				continue OUTER
			case jobs <- job{slab, i, len(toMigrate)}:
			}
		}
	}
}

// managedCheckFileHealth checks the health of the slabs and
// returns those needing a repair.
func (c *Contractor) managedCheckFileHealth() (toRepair []slabInfo, err error) {
	// Load slabs.
	slabs, err := c.getSlabs()
	if err != nil {
		c.log.Println("ERROR: couldn't load slabs:", err)
		return
	}

	// Iterate through the slabs and check if any needs a repair.
	for _, slab := range slabs {
		// Sanity check.
		if slab.MinShards > uint8(len(slab.Shards)) {
			c.log.Printf("ERROR: retrieved less shards than MinShards (%v/%v)\n", len(slab.Shards), slab.MinShards)
			continue
		}

		// Check if the renter has opted in for repairs.
		renter, err := c.GetRenter(slab.renterKey)
		if err != nil {
			c.log.Println("ERROR: couldn't fetch the renter:", err)
			continue
		}
		if !renter.Settings.AutoRepairFiles {
			continue
		}

		contracts := c.staticContracts.ByRenter(renter.PublicKey)
		var numShards uint8
		for _, shard := range slab.Shards {
			// Fetch the host.
			host, exists, err := c.hdb.Host(shard.Host)
			if err != nil {
				c.log.Println("ERROR: couldn't fetch host:", err)
				continue
			}
			if !exists {
				c.log.Printf("WARN: host %v not found in the database\n", shard.Host)
				continue
			}
			if !host.ScanHistory[len(host.ScanHistory)-1].Success {
				// Host is offline. Check the second to last scan.
				if len(host.ScanHistory) < 2 || !host.ScanHistory[len(host.ScanHistory)-2].Success {
					continue
				}
			}
			// Check if the renter has a contract with this host.
			for _, contract := range contracts {
				if contract.HostPublicKey == shard.Host {
					if cu, ok := c.managedContractUtility(contract.ID); ok && cu.GoodForRenew {
						numShards++
					}
					break
				}
			}
		}

		// Catch the edge cases.
		if slab.MinShards == uint8(len(slab.Shards)) {
			if numShards == slab.MinShards {
				slab.health = 1
			}
		} else if numShards > slab.MinShards {
			slab.health = float64(numShards-slab.MinShards) / float64(uint8(len(slab.Shards))-slab.MinShards)
		}

		if slab.health < healthCutoff {
			toRepair = append(toRepair, slab)
		}
	}

	return
}

// migrateSlab migrates a slab by downloading and re-uploading
// its shards.
func (c *Contractor) migrateSlab(ctx context.Context, rpk types.PublicKey, s *object.Slab) error {
	// Get the current height.
	c.mu.RLock()
	bh := c.blockHeight
	c.mu.RUnlock()

	// Make two slices. One shall contain all renter contracts,
	// and the other only those that are GFU.
	dlContracts := c.staticContracts.ByRenter(rpk)
	ulContracts := make([]modules.RenterContract, 0, len(dlContracts))
	for _, dl := range dlContracts {
		if cu, ok := c.managedContractUtility(dl.ID); ok && cu.GoodForUpload {
			ulContracts = append(ulContracts, dl)
		}
	}

	// Make a map of good hosts.
	goodHosts := make(map[types.PublicKey]struct{})
	for _, ul := range ulContracts {
		goodHosts[ul.HostPublicKey] = struct{}{}
	}

	// Make a map of host to contract ID.
	h2c := make(map[types.PublicKey]types.FileContractID)
	for _, dl := range dlContracts {
		h2c[dl.HostPublicKey] = dl.ID
	}

	// Collect indices of shards that need to be migrated.
	usedMap := make(map[types.FileContractID]struct{})
	var shardIndices []int
	for i, shard := range s.Shards {
		// Bad host.
		if _, exists := goodHosts[shard.Host]; !exists {
			shardIndices = append(shardIndices, i)
			continue
		}

		// Reused host.
		_, exists := usedMap[h2c[shard.Host]]
		if exists {
			shardIndices = append(shardIndices, i)
			continue
		}
		usedMap[h2c[shard.Host]] = struct{}{}
	}

	// If all shards are on good hosts, we're done.
	if len(shardIndices) == 0 {
		return nil
	}

	// Subtract the number of shards that are on hosts with contracts and might
	// therefore be used for downloading from.
	missingShards := len(shardIndices)
	for _, si := range shardIndices {
		_, hasContract := h2c[s.Shards[si].Host]
		if hasContract {
			missingShards--
		}
	}

	// Perform some sanity checks.
	if len(ulContracts) < int(s.MinShards) {
		return fmt.Errorf("not enough hosts to repair unhealthy shard to minimum redundancy, %d<%d", len(ulContracts), int(s.MinShards))
	}
	if len(s.Shards)-missingShards < int(s.MinShards) {
		return fmt.Errorf("not enough hosts to download unhealthy shard, %d<%d", len(s.Shards)-len(shardIndices), int(s.MinShards))
	}

	// Download the slab.
	shards, err := c.dm.managedDownloadSlab(ctx, rpk, *s, dlContracts)
	if err != nil {
		return fmt.Errorf("failed to download slab for migration: %v", err)
	}
	s.Encrypt(shards)

	// Filter it down to the shards we need to migrate.
	for i, si := range shardIndices {
		shards[i] = shards[si]
	}
	shards = shards[:len(shardIndices)]

	// Filter upload contracts to the ones we haven't used yet.
	var allowed []modules.RenterContract
	for ul := range ulContracts {
		if _, exists := usedMap[ulContracts[ul].ID]; !exists {
			allowed = append(allowed, ulContracts[ul])
		}
	}

	// Migrate the shards.
	uploaded, err := c.um.migrate(ctx, rpk, shards, allowed, bh)
	if err != nil {
		return fmt.Errorf("failed to upload slab for migration: %v", err)
	}

	// Overwrite the unhealthy shards with the newly migrated ones.
	for i, si := range shardIndices {
		s.Shards[si] = uploaded[i]
	}

	return nil
}
