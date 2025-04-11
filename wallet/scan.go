package wallet

import (
	"context"
	"log"
	"runtime"
	"sync"

	"github.com/mike76-dev/sia-satellite/internal/utils"
	"go.sia.tech/core/types"
	"go.sia.tech/coreutils/chain"
	"go.sia.tech/coreutils/wallet"
)

const defaultAddresses = 1e6

// scanner scans the blockchain for relevant addresses.
type scanner struct {
	seed  *[32]byte
	addrs map[types.Address]uint64
	mu    sync.Mutex
}

// newScanner returns an initialized scanner.
func newScanner(seed *[32]byte) *scanner {
	return &scanner{
		seed: seed,
	}
}

// generateAddresses generates the specified number of addresses.
func (s *scanner) generateAddresses(ctx context.Context, num uint64) {
	log.Printf("Generating %d lookahead addresses... ", num)
	s.mu.Lock()
	defer s.mu.Unlock()

	addrs := make([]types.Address, num)
	seed := s.seed
	var wg sync.WaitGroup
	wg.Add(runtime.NumCPU())
	for cpu := range runtime.NumCPU() {
		go func(offset uint64) {
			defer wg.Done()
			for i := offset; i < num; i += uint64(runtime.NumCPU()) {
				select {
				case <-ctx.Done():
					return
				default:
				}
				key := wallet.KeyFromSeed(seed, i)
				addrs[i] = types.StandardUnlockHash(key.PublicKey())
			}
		}(uint64(cpu))
	}
	wg.Wait()

	s.addrs = make(map[types.Address]uint64)
	for index, addr := range addrs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.addrs[addr] = uint64(index)
	}
	log.Println("Done!")
}

// scan performs the blockchain scan.
func (s *scanner) scan(ctx context.Context, cm *chain.Manager, num uint64) (map[types.Address]uint64, error) {
	s.generateAddresses(ctx, num)
	addrs := make(map[types.Address]uint64)
	index := types.ChainIndex{}
	for index != cm.Tip() {
		select {
		case <-ctx.Done():
			return nil, nil
		default:
		}

		reverted, applied, err := cm.UpdatesSince(index, 1000)
		if err != nil {
			return nil, utils.AddContext(err, "couldn't request chain updates")
		}

		for _, cru := range reverted {
			for _, txn := range cru.Block.Transactions {
				for _, sci := range txn.SiacoinInputs {
					delete(addrs, sci.UnlockConditions.UnlockHash())
				}
				for _, sco := range txn.SiacoinOutputs {
					delete(addrs, sco.Address)
				}
			}

			for _, txn := range cru.Block.V2Transactions() {
				for _, sci := range txn.SiacoinInputs {
					delete(addrs, sci.Parent.SiacoinOutput.Address)
				}
				for _, sco := range txn.SiacoinOutputs {
					delete(addrs, sco.Address)
				}
			}
		}

		for _, cau := range applied {
			for _, txn := range cau.Block.Transactions {
				for _, sci := range txn.SiacoinInputs {
					addr := sci.UnlockConditions.UnlockHash()
					i, found := s.addrs[addr]
					if found {
						addrs[addr] = i
					}
				}
				for _, sco := range txn.SiacoinOutputs {
					i, found := s.addrs[sco.Address]
					if found {
						addrs[sco.Address] = i
					}
				}
			}

			for _, txn := range cau.Block.V2Transactions() {
				for _, sci := range txn.SiacoinInputs {
					addr := sci.Parent.SiacoinOutput.Address
					i, found := s.addrs[addr]
					if found {
						addrs[addr] = i
					}
				}
				for _, sco := range txn.SiacoinOutputs {
					i, found := s.addrs[sco.Address]
					if found {
						addrs[sco.Address] = i
					}
				}
			}
		}

		if len(applied) > 0 {
			index = applied[len(applied)-1].State.Index
		} else {
			index = reverted[len(reverted)-1].State.Index
		}
		log.Printf("Scanned up to block %d\n", index.Height)
	}

	return addrs, nil
}
