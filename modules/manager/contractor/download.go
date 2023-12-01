package contractor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/object"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"

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

		statsOverdrivePct                *dataPoints
		statsSlabDownloadSpeedBytesPerMS *dataPoints

		stopChan chan struct{}

		mu            sync.Mutex
		downloaders   map[string]*downloader
		lastRecompute time.Time
	}

	// downloader represents an individual worker that downloads
	// sectors from a host for a renter.
	downloader struct {
		contractor *Contractor

		renterKey types.PublicKey
		hostKey   types.PublicKey

		statsDownloadSpeedBytesPerMS    *dataPoints // Keep track of this separately for stats
		statsSectorDownloadEstimateInMS *dataPoints // (no decay is applied)

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
		root        types.Hash256
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
		errs    hostErrorSet
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

// hostError associates an error with a given host.
type hostError struct {
	HostKey types.PublicKey
	Err     error
}

// Error implements error.
func (he hostError) Error() string {
	return fmt.Sprintf("%x: %v", he.HostKey[:4], he.Err.Error())
}

// Unwrap returns the underlying error.
func (he hostError) Unwrap() error {
	return he.Err
}

// A hostErrorSet is a collection of errors from various hosts.
type hostErrorSet []*hostError

// Error implements error.
func (hes hostErrorSet) Error() string {
	strs := make([]string, len(hes))
	for i := range strs {
		strs[i] = hes[i].Error()
	}
	// Include a leading newline so that the first error isn't printed on the
	// same line as the error context.
	return "\n" + strings.Join(strs, "\n")
}

// newDownloadManager returns an initialized download manager.
func newDownloadManager(c *Contractor, maxOverdrive uint64, overdriveTimeout time.Duration) *downloadManager {
	return &downloadManager{
		contractor: c,

		maxOverdrive:     maxOverdrive,
		overdriveTimeout: overdriveTimeout,

		statsOverdrivePct:                newDataPoints(0),
		statsSlabDownloadSpeedBytesPerMS: newDataPoints(0),

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

		statsSectorDownloadEstimateInMS: newDataPoints(statsDecayHalfTime),
		statsDownloadSpeedBytesPerMS:    newDataPoints(0), // No decay for exposed stats.

		signalWorkChan: make(chan struct{}, 1),
		stopChan:       make(chan struct{}),

		queue: make([]*sectorDownloadReq, 0),
	}
}

// managedDownloadObject downloads the whole object.
func (mgr *downloadManager) managedDownloadObject(ctx context.Context, w io.Writer, rpk types.PublicKey, o object.Object, id types.Hash256, offset, length uint64, contracts []modules.RenterContract) (err error) {
	// Calculate what slabs we need.
	var ss []slabSlice
	for _, s := range o.Slabs {
		ss = append(ss, slabSlice{
			SlabSlice:   s,
			PartialSlab: s.IsPartial(),
		})
	}
	slabs := slabsForDownload(ss, offset, length)
	if len(slabs) == 0 {
		return nil
	}

	// Go through the slabs and fetch any partial slab data.
	for i := range slabs {
		if !slabs[i].PartialSlab {
			continue
		}
		data, err := mgr.contractor.getPartialSlab(id, slabs[i].Key, uint64(slabs[i].Offset), uint64(slabs[i].Length))
		if err != nil {
			return modules.AddContext(err, "failed to fetch partial slab data")
		}
		slabs[i].Data = data
	}

	// Refresh the downloaders.
	mgr.refreshDownloaders(rpk, contracts)

	// Build a map to count available shards later.
	hosts := make(map[types.PublicKey]struct{})
	for _, c := range contracts {
		hosts[c.HostPublicKey] = struct{}{}
	}

	// Create the cipher writer.
	cw := o.Key.Decrypt(w, offset)

	// Create next slab chan.
	nextSlabChan := make(chan struct{})
	defer close(nextSlabChan)

	// Create response chan and ensure it's closed properly.
	var wg sync.WaitGroup
	responseChan := make(chan *slabDownloadResponse)
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		wg.Wait()
		close(responseChan)
	}()

	// Launch a goroutine to launch consecutive slab downloads.
	var concurrentSlabs uint64

	wg.Add(1)
	go func() {
		defer wg.Done()

		var slabIndex int
		for {
			if slabIndex < len(slabs) && atomic.LoadUint64(&concurrentSlabs) < maxConcurrentSlabsPerDownload {
				next := slabs[slabIndex]

				// Check if the next slab is a partial slab.
				if next.PartialSlab {
					responseChan <- &slabDownloadResponse{index: slabIndex}
					slabIndex++
					atomic.AddUint64(&concurrentSlabs, 1)
					continue // Handle partial slab separately.
				}

				// Check if we have enough downloaders.
				var available uint8
				for _, s := range next.Shards {
					if _, exists := hosts[s.LatestHost]; exists {
						available++
					}
				}
				if available < next.MinShards {
					responseChan <- &slabDownloadResponse{err: fmt.Errorf("not enough hosts available to download the slab: %v/%v", available, next.MinShards)}
					return
				}

				// Launch the download.
				wg.Add(1)
				go func(index int) {
					mgr.downloadSlab(ctx, rpk, next.SlabSlice, index, responseChan, nextSlabChan)
					wg.Done()
				}(slabIndex)
				atomic.AddUint64(&concurrentSlabs, 1)
				slabIndex++
			}

			// Block until we are ready for the next slab.
			select {
			case <-ctx.Done():
				return
			case nextSlabChan <- struct{}{}:
			}
		}
	}()

	// Collect the response, responses might come in out of order so we keep
	// them in a map and return what we can when we can.
	responses := make(map[int]*slabDownloadResponse)
	var respIndex int
outer:
	for {
		select {
		case <-mgr.stopChan:
			return errors.New("manager was stopped")
		case <-ctx.Done():
			return errors.New("download timed out")
		case resp := <-responseChan:
			atomic.AddUint64(&concurrentSlabs, ^uint64(0))

			if resp.err != nil {
				mgr.contractor.log.Printf("ERROR: download slab %v failed: %v\n", resp.index, resp.err)
				return resp.err
			}

			responses[resp.index] = resp
			for {
				if next, exists := responses[respIndex]; exists {
					s := slabs[respIndex]
					if s.PartialSlab {
						// Partial slab.
						_, err = cw.Write(s.Data)
						if err != nil {
							mgr.contractor.log.Printf("failed to send partial slab %v: %v\n", respIndex, err)
							return err
						}
					} else {
						// Regular slab.
						slabs[respIndex].Decrypt(next.shards)
						err := slabs[respIndex].Recover(cw, next.shards)
						if err != nil {
							mgr.contractor.log.Printf("failed to recover slab %v: %v\n", respIndex, err)
							return err
						}
					}

					next = nil
					delete(responses, respIndex)
					respIndex++

					select {
					case <-nextSlabChan:
					default:
					}

					continue
				} else {
					break
				}
			}

			// Exit condition.
			if respIndex == len(slabs) {
				break outer
			}
		}
	}

	return nil
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
		if _, exists := available[shard.LatestHost]; exists {
			availableShards++
		}
	}

	// Check if we have enough shards.
	if availableShards < slab.MinShards {
		return nil, fmt.Errorf("not enough hosts available to download the slab: %v/%v", availableShards, slab.MinShards)
	}

	// Download the slab.
	responseChan := make(chan *slabDownloadResponse)
	nextSlabChan := make(chan struct{})
	slice := object.SlabSlice{
		Slab:   slab,
		Offset: 0,
		Length: uint32(slab.MinShards) * rhpv2.SectorSize,
	}
	go func() {
		mgr.downloadSlab(ctx, rpk, slice, 0, responseChan, nextSlabChan)
		// NOTE: when downloading 1 slab we can simply close both channels.
		close(responseChan)
		close(nextSlabChan)
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

// tryRecomputeStats tries to recalculate the downloadManager's stats.
func (mgr *downloadManager) tryRecomputeStats() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if time.Since(mgr.lastRecompute) < statsRecomputeMinInterval {
		return
	}

	for _, d := range mgr.downloaders {
		d.statsSectorDownloadEstimateInMS.recompute()
		d.statsDownloadSpeedBytesPerMS.recompute()
	}
	mgr.lastRecompute = time.Now()
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
		go downloader.processQueue()
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
		hostToSectors[s.LatestHost] = append(hostToSectors[s.LatestHost], sectorInfo{s, sI})
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
func (mgr *downloadManager) downloadSlab(ctx context.Context, rpk types.PublicKey, slice object.SlabSlice, index int, responseChan chan *slabDownloadResponse, nextSlabChan chan struct{}) {
	// Prepare the slab download.
	slab := mgr.newSlabDownload(ctx, rpk, slice, index)

	// Download shards.
	resp := &slabDownloadResponse{index: index}
	resp.shards, resp.err = slab.downloadShards(ctx, nextSlabChan)

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

	// Define some state to keep track of stats.
	var mu sync.Mutex
	var start time.Time
	var concurrent int64
	var downloadedB int64
	trackStatsFn := func() {
		if start.IsZero() || time.Since(start).Milliseconds() == 0 || downloadedB == 0 {
			return
		}
		durationMS := time.Since(start).Milliseconds()
		d.statsDownloadSpeedBytesPerMS.track(float64(downloadedB / durationMS))
		d.statsSectorDownloadEstimateInMS.track(float64(durationMS))
		start = time.Time{}
		downloadedB = 0
	}

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
			if start.IsZero() {
				start = time.Now()
			}
			concurrent++
			mu.Unlock()

			// Execute the request.
			err := d.execute(req)
			d.trackFailure(err)

			// Update state + potentially track stats.
			mu.Lock()
			if err == nil {
				downloadedB += int64(req.length) + 284
				if downloadedB >= maxConcurrentSectorsPerHost*rhpv2.SectorSize || concurrent == maxConcurrentSectorsPerHost {
					trackStatsFn()
				}
			}
			concurrent--
			if concurrent < 0 {
				panic("concurrent can never be less than zero") // Developer error.
			}
			mu.Unlock()
		}

		// Last worker that's done closes the channel and flushes the stats.
		if atomic.AddUint64(&inflight, ^uint64(0)) == 0 {
			close(doneChan)
			trackStatsFn()
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
func (d *downloader) processQueue() {
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

// estimate calculates an estimated time to complete a download.
func (d *downloader) estimate() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Fetch estimated duration per sector.
	estimateP90 := d.statsSectorDownloadEstimateInMS.getP90()
	if estimateP90 == 0 {
		if avg := d.statsSectorDownloadEstimateInMS.average(); avg > 0 {
			estimateP90 = avg
		} else {
			estimateP90 = 1
		}
	}

	numSectors := float64(len(d.queue) + 1)
	return numSectors * estimateP90
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
	d.statsSectorDownloadEstimateInMS.track(float64(time.Hour.Milliseconds()))
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
		root:        req.root,
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
		root:      req.root,
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

		// make the fastest host the current host
		s.curr = s.mgr.fastest(s.renterKey, hosts)
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
		hostKey:   sector.LatestHost,

		overdrive:   overdrive,
		sectorIndex: sector.index,
		resps:       resps,
	}
}

// downloadShards uses a series of requests to download a slab.
func (s *slabDownload) downloadShards(ctx context.Context, nextSlabChan chan struct{}) ([][]byte, error) {
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
	for i := 0; i < int(s.minShards); {
		req := s.nextRequest(ctx, resps, false)
		if req == nil {
			return nil, fmt.Errorf("no hosts available")
		} else if err := s.launch(req); err == nil {
			i++
		}
	}

	// Collect responses.
	var done bool
	var next bool
	var triggered bool
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

			done, next = s.receive(*resp)
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
			if next && !triggered {
				select {
				case <-nextSlabChan:
					triggered = true
				default:
				}
			}
		}
	}

	// Track stats.
	s.mgr.statsOverdrivePct.track(s.overdrivePct())
	s.mgr.statsSlabDownloadSpeedBytesPerMS.track(float64(s.downloadSpeed()))

	return s.finish()
}

// overdrivePct returns the overdrive percentage.
func (s *slabDownload) overdrivePct() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	numOverdrive := int(s.numLaunched) - s.minShards
	if numOverdrive < 0 {
		numOverdrive = 0
	}

	return float64(numOverdrive) / float64(s.minShards)
}

// downloadSpeed calculates the speed of a download.
func (s *slabDownload) downloadSpeed() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	completedShards := len(s.sectors)
	bytes := completedShards * rhpv2.SectorSize
	ms := time.Since(s.created).Milliseconds()
	if ms == 0 {
		ms = 1 // Avoid division by zero.
	}
	return int64(bytes) / ms
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
		return nil, fmt.Errorf("failed to download slab: completed=%d, inflight=%d, launched=%d downloaders=%d unused=%d %w", s.numCompleted, s.numInflight, s.numLaunched, s.mgr.numDownloaders(), unused, s.errs)
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
func (s *slabDownload) receive(resp sectorDownloadResp) (finished bool, next bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update num overdriving.
	if resp.overdrive {
		s.numOverdriving--
	}

	// Failed reqs can't complete the upload.
	s.numInflight--
	if resp.err != nil {
		s.errs = append(s.errs, &hostError{resp.hk, resp.err})
		return false, false
	}

	// Store the sector.
	s.sectors[resp.sectorIndex] = resp.sector
	s.numCompleted++

	return s.numCompleted >= s.minShards, s.numCompleted+int(s.mgr.maxOverdrive) >= s.minShards
}

// fastest returns the fastest host.
func (mgr *downloadManager) fastest(rpk types.PublicKey, hosts []types.PublicKey) (fastest types.PublicKey) {
	// Recompute stats.
	mgr.tryRecomputeStats()

	// Return the fastest host.
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	lowest := math.MaxFloat64
	for _, h := range hosts {
		if d, ok := mgr.downloaders[rpk.String()+h.String()]; !ok {
			continue
		} else if estimate := d.estimate(); estimate < lowest {
			lowest = estimate
			fastest = h
		}
	}
	return
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

// slabSlice is a helper type for downloading slabs.
type slabSlice struct {
	object.SlabSlice
	PartialSlab bool
	Data        []byte
}

// slabsForDownload picks a slice of slabs from the provided set.
func slabsForDownload(slabs []slabSlice, offset, length uint64) []slabSlice {
	// Declare a helper to cast a uint64 to uint32 with overflow detection. This
	// should never produce an overflow.
	cast32 := func(in uint64) uint32 {
		if in > math.MaxUint32 {
			panic("slabsForDownload: overflow detected")
		}
		return uint32(in)
	}

	// Mutate a copy.
	slabs = append([]slabSlice(nil), slabs...)

	firstOffset := offset
	for i, ss := range slabs {
		if firstOffset < uint64(ss.Length) {
			slabs = slabs[i:]
			break
		}
		firstOffset -= uint64(ss.Length)
	}
	slabs[0].Offset += cast32(firstOffset)
	slabs[0].Length -= cast32(firstOffset)

	lastLength := length
	for i, ss := range slabs {
		if lastLength <= uint64(ss.Length) {
			slabs = slabs[:i+1]
			break
		}
		lastLength -= uint64(ss.Length)
	}
	slabs[len(slabs)-1].Length = cast32(lastLength)
	return slabs
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
		cost, err := proto.ReadSectorCost(host.PriceTable, uint64(length))
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
