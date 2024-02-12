package wallet

import (
	"runtime"
	"sync"

	"github.com/mike76-dev/sia-satellite/modules"
	"go.sia.tech/core/types"
	"go.uber.org/zap"
)

const (
	// lookaheadBuffer together with lookaheadRescanThreshold defines the constant part
	// of the maxLookahead.
	lookaheadBuffer = uint64(4000)

	// lookaheadRescanThreshold is the number of keys in the lookahead that will be
	// generated before a complete wallet rescan is initialized.
	lookaheadRescanThreshold = uint64(1000)
)

// maxLookahead returns the size of the lookahead for a given seed progress
// which usually is the current primarySeedProgress.
func maxLookahead(start uint64) uint64 {
	return start + lookaheadRescanThreshold + lookaheadBuffer + start/10
}

// generateKeys generates n keys from seed, starting from index start.
func generateKeys(seed modules.Seed, start, n uint64) []types.PrivateKey {
	// Generate in parallel, one goroutine per core.
	keys := make([]types.PrivateKey, n)
	var wg sync.WaitGroup
	wg.Add(runtime.NumCPU())
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
		go func(offset uint64) {
			defer wg.Done()
			for i := offset; i < n; i += uint64(runtime.NumCPU()) {
				keys[i] = modules.KeyFromSeed(&seed, start+i)
			}
		}(uint64(cpu))
	}
	wg.Wait()
	return keys
}

func (w *Wallet) generate(index uint64) {
	for index > uint64(len(w.keys)) {
		key := modules.KeyFromSeed(&w.seed, uint64(len(w.keys)))
		addr := types.StandardUnlockHash(key.PublicKey())
		w.keys[addr] = key
		if err := w.insertAddress(addr); err != nil {
			w.log.Error("couldn't insert address", zap.Error(err))
			return
		}
	}
}

// regenerateLookahead creates future keys up to a maximum of maxKeys keys.
func (w *Wallet) regenerateLookahead(start uint64) {
	// Check how many keys need to be generated.
	maxKeys := maxLookahead(start)
	existingKeys := uint64(len(w.lookahead))

	for i, key := range generateKeys(w.seed, start+existingKeys, maxKeys-existingKeys) {
		w.lookahead[types.StandardUnlockHash(key.PublicKey())] = start + existingKeys + uint64(i)
	}
}

// nextAddresses fetches the next n addresses from the primary seed.
func (w *Wallet) nextAddresses(n uint64) ([]types.UnlockConditions, error) {
	// Check how many unused addresses we have available.
	neededUnused := uint64(len(w.unusedKeys))
	if neededUnused > n {
		neededUnused = n
	}
	n -= neededUnused

	// Generate new keys if the unused ones are not enough. This happens first
	// since it's the only part of the code that might fail. So we don't want to
	// remove keys from the unused map until after we are sure this worked.
	var ucs []types.UnlockConditions
	if n > 0 {
		// Fetch and increment the seed progress.
		progress, err := w.getSeedProgress()
		if err != nil {
			return nil, err
		}
		if err = w.putSeedProgress(progress + n); err != nil {
			return nil, err
		}

		// Integrate the next keys into the wallet, and return the unlock
		// conditions. Also remove new keys from the future keys and update them
		// according to new progress.
		keys := generateKeys(w.seed, progress, n)
		ucs = make([]types.UnlockConditions, 0, len(keys))
		for _, key := range keys {
			w.keys[types.StandardUnlockHash(key.PublicKey())] = key
			if err := w.insertAddress(types.StandardUnlockHash(key.PublicKey())); err != nil {
				return nil, err
			}
			delete(w.lookahead, types.StandardUnlockHash(key.PublicKey()))
			ucs = append(ucs, types.StandardUnlockConditions(key.PublicKey()))
		}
		w.regenerateLookahead(progress + n)
		if err := w.save(); err != nil {
			return nil, err
		}
	}

	// Add as many unused UCs as necessary.
	unusedUCs := make([]types.UnlockConditions, 0, int(neededUnused))
	for uh, uc := range w.unusedKeys {
		if neededUnused == 0 {
			break
		}
		unusedUCs = append(unusedUCs, uc)
		delete(w.unusedKeys, uh)
		neededUnused--
	}
	ucs = append(unusedUCs, ucs...)

	// Reset map if empty for GC to pick it up.
	if len(w.unusedKeys) == 0 {
		w.unusedKeys = make(map[types.Address]types.UnlockConditions)
	}

	return ucs, nil
}

// nextAddress fetches the next address from the seed.
func (w *Wallet) nextAddress() (types.UnlockConditions, error) {
	ucs, err := w.nextAddresses(1)
	if err != nil {
		return types.UnlockConditions{}, err
	}
	return ucs[0], nil
}

// MarkAddressUnused marks the provided address as unused which causes it to be
// handed out by a subsequent call to `NextAddresses` again.
func (w *Wallet) MarkAddressUnused(addrs ...types.UnlockConditions) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()
	w.markAddressUnused(addrs...)

	return nil
}

// markAddressUnused marks the provided address as unused which causes it
// to be handed out by a subsequent call to `NextAddresses` again.
func (w *Wallet) markAddressUnused(addrs ...types.UnlockConditions) {
	for _, addr := range addrs {
		w.unusedKeys[addr.UnlockHash()] = addr
	}
}

// NextAddresses returns n unlock hashes that are ready to receive Siacoins or
// Siafunds.
//
// Warning: If this function is used to generate large numbers of addresses,
// those addresses should be used. Otherwise the lookahead might not be able to
// keep up and multiple wallets with the same seed might desync.
func (w *Wallet) NextAddresses(n uint64) ([]types.UnlockConditions, error) {
	if err := w.tg.Add(); err != nil {
		return nil, err
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Generate some keys.
	ucs, err := w.nextAddresses(n)
	if err != nil {
		return nil, err
	}

	return ucs, nil
}

// NextAddress returns an unlock hash that is ready to receive Siacoins or
// Siafunds.
func (w *Wallet) NextAddress() (types.UnlockConditions, error) {
	if err := w.tg.Add(); err != nil {
		return types.UnlockConditions{}, err
	}
	defer w.tg.Done()

	ucs, err := w.NextAddresses(1)
	if err != nil {
		return types.UnlockConditions{}, err
	}
	return ucs[0], nil
}

// ownsAddress returns true if the provided address belongs to the wallet.
func (w *Wallet) ownsAddress(addr types.Address) bool {
	_, exists := w.keys[addr]
	return exists
}

// Addresses returns the addresses of the wallet.
func (w *Wallet) Addresses() (addrs []types.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for addr := range w.addrs {
		addrs = append(addrs, addr)
	}

	return
}
