package contractor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
	"go.sia.tech/renterd/object"

	"lukechampine.com/frand"
)

const (
	// defaultWithdrawalExpiryBlocks is the number of blocks we add to the
	// current blockheight when we define an expiry block height for withdrawal
	// messages.
	defaultWithdrawalExpiryBlocks = 6

	// maxConcurrentSectorsPerHost is how many sectors may be downloaded from a
	// single host at a time.
	maxConcurrentSectorsPerHost = 3

	// maxConcurrentSlabsPerDownload is how many slabs a downloader may be
	// downloading at a time.
	maxConcurrentSlabsPerDownload = 3
)

type (
	// slabID is the same as [8]byte.
	slabID [8]byte

	// downloadManager manages all downloads.
	downloadManager struct {
		contractor *Contractor

		maxOverdrive     uint64
		overdriveTimeout time.Duration

		stopChan chan struct{}

		mu          sync.Mutex
		downloaders map[string]*downloader
	}

	// downloader represents an individual worker that downloads
	// sectors from a host for a renter.
	downloader struct {
		contractor *Contractor

		renterKey types.PublicKey
		hostKey   types.PublicKey

		signalWorkChan chan struct{}
		stopChan       chan struct{}

		mu                  sync.Mutex
		consecutiveFailures uint64
		queue               []*sectorDownloadReq
		numDownloads        uint64
	}

	// sectorDownloadReq contains a sector download request.
	sectorDownloadReq struct {
		ctx context.Context

		length uint32
		offset uint32
		root   types.Hash256

		renterKey types.PublicKey
		hostKey   types.PublicKey

		overdrive   bool
		sectorIndex int
		resps       *sectorResponses
	}

	// sectorResponses collects all sector download responses.
	sectorResponses struct {
		mu        sync.Mutex
		closed    bool
		responses []*sectorDownloadResp
		c         chan struct{} // Signal that a new response is available.
	}

	// sectorDownloadResp contains a response to a download request.
	sectorDownloadResp struct {
		overdrive   bool
		hk          types.PublicKey
		sectorIndex int
		sector      []byte
		err         error
	}

	// slabDownload contains the slab download information.
	slabDownload struct {
		mgr *downloadManager

		renterKey types.PublicKey

		sID       slabID
		created   time.Time
		index     int
		minShards int
		length    uint32
		offset    uint32

		mu             sync.Mutex
		lastOverdrive  time.Time
		numCompleted   int
		numInflight    uint64
		numLaunched    uint64
		numOverdriving uint64

		curr          types.PublicKey
		hostToSectors map[types.PublicKey][]sectorInfo
		used          map[types.PublicKey]struct{}

		sectors [][]byte
	}

	// slabDownloadResponse contains a response to a slab download.
	slabDownloadResponse struct {
		shards [][]byte
		index  int
		err    error
	}

	// sectorInfo combines the sector and its index.
	sectorInfo struct {
		object.Sector
		index int
	}
)

// newDownloadManager returns an initialized download manager.
func newDownloadManager(c *Contractor, maxOverdrive uint64, overdriveTimeout time.Duration) *downloadManager {
	return &downloadManager{
		contractor: c,

		maxOverdrive:     maxOverdrive,
		overdriveTimeout: overdriveTimeout,

		stopChan: make(chan struct{}),

		downloaders: make(map[string]*downloader),
	}
}

// newDownloader returns a new downloader.
func newDownloader(c *Contractor, rpk, hpk types.PublicKey) *downloader {
	return &downloader{
		contractor: c,

		renterKey: rpk,
		hostKey:   hpk,

		signalWorkChan: make(chan struct{}, 1),
		stopChan:       make(chan struct{}),

		queue: make([]*sectorDownloadReq, 0),
	}
}

// managedDownloadSlab downloads a slab.
func (mgr *downloadManager) managedDownloadSlab(ctx context.Context, rpk types.PublicKey, slab object.Slab, contracts []modules.RenterContract) ([][]byte, error) {
	// Refresh the downloaders.
	mgr.refreshDownloaders(rpk, contracts)

	// Grab available hosts.
	available := make(map[types.PublicKey]struct{})
	for _, c := range contracts {
		available[c.HostPublicKey] = struct{}{}
	}

	// Count how many shards we can download (best-case).
	var availableShards uint8
	for _, shard := range slab.Shards {
		if _, exists := available[shard.Host]; exists {
			availableShards++
		}
	}

	// Check if we have enough shards.
	if availableShards < slab.MinShards {
		return nil, fmt.Errorf("not enough hosts available to download the slab: %v/%v", availableShards, slab.MinShards)
	}

	// Download the slab.
	responseChan := make(chan *slabDownloadResponse)
	slice := object.SlabSlice{
		Slab:   slab,
		Offset: 0,
		Length: uint32(slab.MinShards) * rhpv2.SectorSize,
	}
	go func() {
		mgr.downloadSlab(ctx, rpk, slice, 0, responseChan)
		close(responseChan)
	}()

	// Await the response.
	var resp *slabDownloadResponse
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp = <-responseChan:
		if resp.err != nil {
			return nil, resp.err
		}
	}

	// Decrypt and recover.
	slice.Decrypt(resp.shards)
	err := slice.Reconstruct(resp.shards)
	if err != nil {
		return nil, err
	}

	return resp.shards, err
}

// stop stops the download manager and all running downloads.
func (mgr *downloadManager) stop() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	close(mgr.stopChan)
	for _, d := range mgr.downloaders {
		close(d.stopChan)
	}
}

// numDownloaders returns the current number of downloaders.
func (mgr *downloadManager) numDownloaders() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return len(mgr.downloaders)
}

// refreshDownloaders updates the downloaders map.
func (mgr *downloadManager) refreshDownloaders(rpk types.PublicKey, contracts []modules.RenterContract) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// Build map.
	want := make(map[string]modules.RenterContract)
	for _, c := range contracts {
		want[rpk.String()+c.HostPublicKey.String()] = c
	}

	// Prune downloaders.
	for key := range mgr.downloaders {
		_, wanted := want[key]
		if !wanted {
			close(mgr.downloaders[key].stopChan)
			delete(mgr.downloaders, key)
			continue
		}

		delete(want, key) // Remove from want so remainging ones are the missing ones.
	}

	// Update downloaders.
	for _, c := range want {
		downloader := newDownloader(mgr.contractor, rpk, c.HostPublicKey)
		mgr.downloaders[rpk.String()+c.HostPublicKey.String()] = downloader
		go downloader.processQueue(mgr.contractor)
	}
}

// newSlabDownload prepares a new slab download.
func (mgr *downloadManager) newSlabDownload(ctx context.Context, rpk types.PublicKey, slice object.SlabSlice, slabIndex int) *slabDownload {
	// Create slab ID.
	var sID slabID
	frand.Read(sID[:])

	// Calculate the offset and length.
	offset, length := slice.SectorRegion()

	// Build sector info.
	hostToSectors := make(map[types.PublicKey][]sectorInfo)
	for sI, s := range slice.Shards {
		hostToSectors[s.Host] = append(hostToSectors[s.Host], sectorInfo{s, sI})
	}

	// Create slab download.
	return &slabDownload{
		mgr: mgr,

		renterKey: rpk,

		sID:       sID,
		created:   time.Now(),
		index:     slabIndex,
		minShards: int(slice.MinShards),
		offset:    offset,
		length:    length,

		hostToSectors: hostToSectors,
		used:          make(map[types.PublicKey]struct{}),

		sectors: make([][]byte, len(slice.Shards)),
	}
}

// downloadSlab performs the slab download.
func (mgr *downloadManager) downloadSlab(ctx context.Context, rpk types.PublicKey, slice object.SlabSlice, index int, responseChan chan *slabDownloadResponse) {
	// Prepare the slab download.
	slab := mgr.newSlabDownload(ctx, rpk, slice, index)

	// Download shards.
	resp := &slabDownloadResponse{index: index}
	resp.shards, resp.err = slab.downloadShards(ctx)

	// Send the response.
	select {
	case <-ctx.Done():
	case responseChan <- resp:
	}
}

// isStopped returns whether the downloader is stopped.
func (d *downloader) isStopped() bool {
	select {
	case <-d.stopChan:
		return true
	default:
	}
	return false
}

// fillBatch returns a batch of download requests.
func (d *downloader) fillBatch() (batch []*sectorDownloadReq) {
	for len(batch) < maxConcurrentSectorsPerHost {
		if req := d.pop(); req == nil {
			break
		} else if req.done() {
			continue
		} else {
			batch = append(batch, req)
		}
	}
	return
}

// processBatch processes the batch of download requests.
func (d *downloader) processBatch(batch []*sectorDownloadReq) chan struct{} {
	doneChan := make(chan struct{})

	// Define some state.
	var mu sync.Mutex
	var concurrent int64

	// Define a worker to process download requests.
	inflight := uint64(len(batch))
	reqsChan := make(chan *sectorDownloadReq)
	workerFn := func() {
		for req := range reqsChan {
			if d.isStopped() {
				break
			}

			// Update state.
			mu.Lock()
			concurrent++
			mu.Unlock()

			// Execute the request.
			err := d.execute(req)
			d.trackFailure(err)

			// Update state.
			mu.Lock()
			concurrent--
			if concurrent < 0 {
				panic("concurrent can never be less than zero") // Developer error.
			}
			mu.Unlock()
		}

		// Last worker that's done closes the channel.
		if atomic.AddUint64(&inflight, ^uint64(0)) == 0 {
			close(doneChan)
		}
	}

	// Launch workers.
	for i := 0; i < len(batch); i++ {
		go workerFn()
	}
	for _, req := range batch {
		reqsChan <- req
	}

	// Launch a goroutine to keep the request coming.
	go func() {
		defer close(reqsChan)
		for {
			if req := d.pop(); req == nil {
				break
			} else if req.done() {
				continue
			} else {
				reqsChan <- req
			}
		}
	}()

	return doneChan
}

// processQueue processes the queue of download requests.
func (d *downloader) processQueue(c *Contractor) {
outer:
	for {
		// Wait for work.
		select {
		case <-d.signalWorkChan:
		case <-d.stopChan:
			return
		}

		for {
			// Try fill a batch of requests.
			batch := d.fillBatch()
			if len(batch) == 0 {
				continue outer
			}

			// Process the batch.
			doneChan := d.processBatch(batch)
			for {
				select {
				case <-d.stopChan:
					return
				case <-doneChan:
					continue outer
				}
			}
		}
	}
}

// enqueue adds a new download request to the queue.
func (d *downloader) enqueue(download *sectorDownloadReq) {
	// Enqueue the job.
	d.mu.Lock()
	d.queue = append(d.queue, download)
	d.mu.Unlock()

	// Signal there's work.
	select {
	case d.signalWorkChan <- struct{}{}:
	default:
	}
}

// pop removes a request from the queue and returns it.
func (d *downloader) pop() *sectorDownloadReq {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.queue) > 0 {
		j := d.queue[0]
		d.queue[0] = nil
		d.queue = d.queue[1:]
		return j
	}
	return nil
}

// trackFailure increments the number of request failures.
func (d *downloader) trackFailure(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err == nil {
		d.consecutiveFailures = 0
		return
	}

	if errors.Is(err, errBalanceInsufficient) ||
		errors.Is(err, errPriceTableExpired) ||
		errors.Is(err, errPriceTableNotFound) ||
		errors.Is(err, errSectorNotFound) {
		return // The host is not to blame for these errors.
	}

	d.consecutiveFailures++
}

// execute executes the download request.
func (d *downloader) execute(req *sectorDownloadReq) (err error) {
	buf := bytes.NewBuffer(make([]byte, 0, rhpv2.SectorSize))
	err = d.contractor.managedDownloadSector(req.ctx, d.renterKey, d.hostKey, buf, req.root, req.offset, req.length)
	if err != nil {
		req.fail(err)
		return err
	}

	d.mu.Lock()
	d.numDownloads++
	d.mu.Unlock()

	req.succeed(buf.Bytes())
	return nil
}

// succeed reports a download success.
func (req *sectorDownloadReq) succeed(sector []byte) {
	req.resps.add(&sectorDownloadResp{
		hk:          req.hostKey,
		overdrive:   req.overdrive,
		sectorIndex: req.sectorIndex,
		sector:      sector,
	})
}

// fail reports a download failure.
func (req *sectorDownloadReq) fail(err error) {
	req.resps.add(&sectorDownloadResp{
		err:       err,
		hk:        req.hostKey,
		overdrive: req.overdrive,
	})
}

// done returns whether the download request is completed.
func (req *sectorDownloadReq) done() bool {
	select {
	case <-req.ctx.Done():
		return true
	default:
		return false
	}
}

// overdrive spins up new workers when required.
func (s *slabDownload) overdrive(ctx context.Context, resps *sectorResponses) (resetTimer func()) {
	// Overdrive is disabled.
	if s.mgr.overdriveTimeout == 0 {
		return func() {}
	}

	// Create a helper function that increases the timeout for each overdrive.
	timeout := func() time.Duration {
		s.mu.Lock()
		defer s.mu.Unlock()
		return time.Duration(s.numOverdriving+1) * s.mgr.overdriveTimeout
	}

	// Create a timer to trigger overdrive.
	timer := time.NewTimer(timeout())
	resetTimer = func() {
		timer.Stop()
		select {
		case <-timer.C:
		default:
		}
		timer.Reset(timeout())
	}

	// Create a function to check whether overdrive is possible.
	canOverdrive := func(timeout time.Duration) bool {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Overdrive is not due yet.
		if time.Since(s.lastOverdrive) < timeout {
			return false
		}

		// Overdrive is maxed out.
		remaining := s.minShards - s.numCompleted
		if s.numInflight >= s.mgr.maxOverdrive+uint64(remaining) {
			return false
		}

		s.lastOverdrive = time.Now()
		return true
	}

	// Try overdriving every time the timer fires.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if canOverdrive(timeout()) {
					for {
						if req := s.nextRequest(ctx, resps, true); req != nil {
							if err := s.launch(req); err != nil {
								continue // Try the next request if this fails to launch.
							}
						}
						break
					}
				}
				resetTimer()
			}
		}
	}()

	return
}

// nextRequest returns the next request from the queue.
func (s *slabDownload) nextRequest(ctx context.Context, resps *sectorResponses, overdrive bool) *sectorDownloadReq {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prepare next sectors to download.
	if len(s.hostToSectors[s.curr]) == 0 {
		// Grab unused hosts.
		var hosts []types.PublicKey
		for host := range s.hostToSectors {
			if _, used := s.used[host]; !used {
				hosts = append(hosts, host)
			}
		}

		// Pick a new host from the list.
		if len(hosts) == 0 {
			return nil
		}
		s.curr = hosts[0]
		s.used[s.curr] = struct{}{}

		// No more sectors to download.
		if len(s.hostToSectors[s.curr]) == 0 {
			return nil
		}
	}

	// Pop the next sector.
	sector := s.hostToSectors[s.curr][0]
	s.hostToSectors[s.curr] = s.hostToSectors[s.curr][1:]

	// Build the request.
	return &sectorDownloadReq{
		ctx: ctx,

		offset: s.offset,
		length: s.length,
		root:   sector.Root,

		renterKey: s.renterKey,
		hostKey:   sector.Host,

		overdrive:   overdrive,
		sectorIndex: sector.index,
		resps:       resps,
	}
}

// finish completes the download.
func (s *slabDownload) finish() ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.numCompleted < s.minShards {
		var unused int
		for host := range s.hostToSectors {
			if _, used := s.used[host]; !used {
				unused++
			}
		}
		return nil, fmt.Errorf("failed to download slab: completed=%d, inflight=%d, launched=%d downloaders=%d unused=%d", s.numCompleted, s.numInflight, s.numLaunched, s.mgr.numDownloaders(), unused)
	}
	return s.sectors, nil
}

// inflight returns the number of inflight requests.
func (s *slabDownload) inflight() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.numInflight
}

// launch starts a new download request.
func (s *slabDownload) launch(req *sectorDownloadReq) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for nil.
	if req == nil {
		return errors.New("no request given")
	}

	// Launch the req.
	err := s.mgr.launch(req)
	if err != nil {
		return err
	}

	// Update the state.
	s.numInflight++
	s.numLaunched++
	if req.overdrive {
		s.numOverdriving++
	}
	return nil
}

// receive receives the downloaded sector and returns whether the
// download is completed.
func (s *slabDownload) receive(resp sectorDownloadResp) (finished bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update num overdriving.
	if resp.overdrive {
		s.numOverdriving--
	}

	// Failed reqs can't complete the upload.
	s.numInflight--
	if resp.err != nil {
		return false
	}

	// Store the sector.
	s.sectors[resp.sectorIndex] = resp.sector
	s.numCompleted++

	return s.numCompleted >= s.minShards
}

// launch starts a new download request.
func (mgr *downloadManager) launch(req *sectorDownloadReq) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	downloader, exists := mgr.downloaders[req.renterKey.String()+req.hostKey.String()]
	if !exists {
		return fmt.Errorf("no downloader for host %v", req.hostKey)
	}

	downloader.enqueue(req)
	return nil
}

// downloadShards uses a series of requests to download a slab.
func (s *slabDownload) downloadShards(ctx context.Context) ([][]byte, error) {
	// Cancel any sector downloads once the download is done.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create the responses queue.
	resps := &sectorResponses{
		c: make(chan struct{}, 1),
	}
	defer resps.close()

	// Launch overdrive.
	resetOverdrive := s.overdrive(ctx, resps)

	// Launch 'MinShard' requests.
	for i := 0; i < int(s.minShards); i++ {
		req := s.nextRequest(ctx, resps, false)
		if err := s.launch(req); err != nil {
			return nil, errors.New("no hosts available")
		}
	}

	// Collect responses.
	var done bool
	for s.inflight() > 0 && !done {
		select {
		case <-s.mgr.stopChan:
			return nil, errors.New("download stopped")
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-resps.c:
			resetOverdrive()
		}

		for {
			resp := resps.next()
			if resp == nil {
				break
			}

			done = s.receive(*resp)
			if !done && resp.err != nil {
				for {
					if req := s.nextRequest(ctx, resps, true); req != nil {
						if err := s.launch(req); err != nil {
							continue // Try the next request if this fails to launch.
						}
					}
					break
				}
			}
		}
	}

	return s.finish()
}

// add adds a response to the collection.
func (sr *sectorResponses) add(resp *sectorDownloadResp) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if sr.closed {
		return
	}
	sr.responses = append(sr.responses, resp)
	select {
	case sr.c <- struct{}{}:
	default:
	}
}

// close closes and clears the responses.
func (sr *sectorResponses) close() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	sr.closed = true
	sr.responses = nil // Clear responses.
	close(sr.c)
	return nil
}

// next returns the next response.
func (sr *sectorResponses) next() *sectorDownloadResp {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	if len(sr.responses) == 0 {
		return nil
	}
	resp := sr.responses[0]
	sr.responses = sr.responses[1:]
	return resp
}

// managedDownloadSector downloads a single sector from the host.
func (c *Contractor) managedDownloadSector(ctx context.Context, rpk, hpk types.PublicKey, w io.Writer, root types.Hash256, offset, length uint32) (err error) {
	// Get the renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return ErrRenterNotFound
	}

	// Fetch the host.
	host, exists, err := c.hdb.Host(hpk)
	if err != nil {
		return modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return errHostNotFound
	}
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return modules.AddContext(err, "failed to get host name")
	}
	siamuxAddr := net.JoinHostPort(hostName, host.Settings.SiaMuxPort)

	// Increase Successful/Failed interactions accordingly.
	var hostFault bool
	defer func() {
		if err != nil {
			c.hdb.IncrementFailedInteractions(hpk)
			if hostFault {
				err = fmt.Errorf("%v: %v", errHostFault, err)
			}
		} else {
			c.hdb.IncrementSuccessfulInteractions(hpk)
		}
	}()

	// Derive the account key.
	ak := modules.DeriveAccountKey(renter.AccountKey, hpk)

	// Derive the account ID.
	accountID := rhpv3.Account(ak.PublicKey())

	// Initiate the protocol.
	err = proto.WithTransportV3(ctx, siamuxAddr, hpk, func(t *rhpv3.Transport) (err error) {
		// Get the cost estimation.
		cost, err := readSectorCost(host.PriceTable, uint64(length))
		if err != nil {
			return modules.AddContext(err, "unable to estimate costs")
		}

		// Fund the account if the balance is insufficient.
		// This way we also get a valid price table.
		pt, err := c.managedFundAccount(rpk, hpk, cost)
		if err != nil {
			return modules.AddContext(err, "unable to fund account")
		}

		payment := rhpv3.PayByEphemeralAccount(accountID, cost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
		_, _, err = proto.RPCReadSector(ctx, t, w, pt, &payment, offset, length, root)
		if err != nil {
			hostFault = true
			return err
		}

		return nil
	})

	return err
}

// readSectorCost returns an overestimate for the cost of reading a sector from a host.
func readSectorCost(pt rhpv3.HostPriceTable, length uint64) (types.Currency, error) {
	rc := pt.BaseCost()
	rc = rc.Add(pt.ReadSectorCost(length))
	cost, _ := rc.Total()

	// Overestimate the cost by 5%.
	cost, overflow := cost.Mul64WithOverflow(21)
	if overflow {
		return types.ZeroCurrency, errors.New("overflow occurred while adding leeway to read sector cost")
	}
	return cost.Div64(20), nil
}
