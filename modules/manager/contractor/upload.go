package contractor

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mike76-dev/sia-satellite/internal/object"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/manager/proto"
	"github.com/montanaflynn/stats"
	"go.uber.org/zap"

	rhpv2 "go.sia.tech/core/rhp/v2"
	rhpv3 "go.sia.tech/core/rhp/v3"
	"go.sia.tech/core/types"
	"go.sia.tech/mux/v1"

	"lukechampine.com/frand"
)

const (
	// Stats-related constants.
	statsDecayHalfTime        = 10 * time.Minute
	statsDecayThreshold       = 5 * time.Minute
	statsRecomputeMinInterval = 3 * time.Second

	// A timeout for uploading a packed slab.
	packedSlabUploadTimeout = 10 * time.Minute
)

var (
	// errNoCandidateUploader is returned when no host was found to
	// upload sector.
	errNoCandidateUploader = errors.New("no candidate uploader found")

	// errNotEnoughContracts is returned when there is not enough
	// contracts to upload the required number of shards.
	errNotEnoughContracts = errors.New("not enough contracts to support requested redundancy")
)

type (
	// uploadManager manages all uploads.
	uploadManager struct {
		contractor *Contractor

		maxOverdrive     uint64
		overdriveTimeout time.Duration

		statsOverdrivePct              *dataPoints
		statsSlabUploadSpeedBytesPerMS *dataPoints
		stopChan                       chan struct{}

		mu            sync.Mutex
		uploaders     []*uploader
		lastRecompute time.Time
	}

	// uploader represents an individual worker that uploads
	// sectors to a host for a renter.
	uploader struct {
		mgr *uploadManager

		renterKey types.PublicKey
		hostKey   types.PublicKey

		statsSectorUploadEstimateInMS    *dataPoints
		statsSectorUploadSpeedBytesPerMS *dataPoints // Keep track of this separately for stats (no decay is applied).
		signalNewUpload                  chan struct{}
		stopChan                         chan struct{}

		mu                  sync.Mutex
		fcid                types.FileContractID
		renewedFrom         types.FileContractID
		endHeight           uint64
		bh                  uint64
		consecutiveFailures uint64
		queue               []*sectorUploadReq
	}

	// upload contains the information about an upload.
	upload struct {
		mgr       *uploadManager
		renterKey types.PublicKey

		allowed          map[types.FileContractID]struct{}
		doneShardTrigger chan struct{}

		mu      sync.Mutex
		ongoing []slabID
		used    map[slabID]map[types.FileContractID]struct{}
	}

	// slabUpload contains the information about a slab upload.
	slabUpload struct {
		mgr    *uploadManager
		upload *upload

		sID     slabID
		created time.Time
		shards  [][]byte

		mu          sync.Mutex
		numInflight uint64
		numLaunched uint64

		lastOverdrive time.Time
		overdriving   map[int]int
		remaining     map[int]sectorCtx
		sectors       []object.Sector
		errs          hostErrorSet
	}

	// slabUploadResponse contains the result of a slab upload.
	slabUploadResponse struct {
		slab  object.SlabSlice
		index int
		err   error
	}

	// sectorCtx combines a context and its cancel func.
	sectorCtx struct {
		ctx    context.Context
		cancel context.CancelFunc
	}

	// sectorUploadReq contains a sector upload request.
	sectorUploadReq struct {
		upload *upload

		sID slabID
		ctx context.Context

		overdrive    bool
		sector       *[rhpv2.SectorSize]byte
		sectorIndex  int
		responseChan chan sectorUploadResp

		// Set by the uploader performing the upload.
		hostKey types.PublicKey
	}

	// sectorUploadResp contains a response to a sector upload request.
	sectorUploadResp struct {
		req  *sectorUploadReq
		root types.Hash256
		err  error
	}

	// dataPoints contains the datapoints for the stats.
	dataPoints struct {
		stats.Float64Data
		halfLife time.Duration
		size     int

		mu            sync.Mutex
		cnt           int
		p90           float64
		lastDatapoint time.Time
		lastDecay     time.Time
	}
)

// newDataPoints returns an initialized dataPoints object.
func newDataPoints(halfLife time.Duration) *dataPoints {
	return &dataPoints{
		size:        20,
		Float64Data: make([]float64, 0),
		halfLife:    halfLife,
		lastDecay:   time.Now(),
	}
}

// newUploadManager returns an initialized upload manager.
func newUploadManager(c *Contractor, maxOverdrive uint64, overdriveTimeout time.Duration) *uploadManager {
	return &uploadManager{
		contractor: c,

		maxOverdrive:     maxOverdrive,
		overdriveTimeout: overdriveTimeout,

		statsOverdrivePct:              newDataPoints(0),
		statsSlabUploadSpeedBytesPerMS: newDataPoints(0),

		stopChan: make(chan struct{}),

		uploaders: make([]*uploader, 0),
	}
}

// newUploader returns a new uploader.
func (mgr *uploadManager) newUploader(c modules.RenterContract, rpk, hpk types.PublicKey) *uploader {
	return &uploader{
		mgr:       mgr,
		renterKey: rpk,
		hostKey:   hpk,

		fcid:      c.ID,
		endHeight: c.EndHeight,

		queue:           make([]*sectorUploadReq, 0),
		signalNewUpload: make(chan struct{}, 1),

		statsSectorUploadEstimateInMS:    newDataPoints(statsDecayHalfTime),
		statsSectorUploadSpeedBytesPerMS: newDataPoints(0), // No decay for exposed stats.
		stopChan:                         make(chan struct{}),
	}
}

// migrate uploads shards to the new hosts.
func (mgr *uploadManager) migrate(ctx context.Context, rpk types.PublicKey, shards [][]byte, contracts []modules.RenterContract, bh uint64) ([]object.Sector, error) {
	// Initiate the upload.
	upload, err := mgr.newUpload(rpk, len(shards), contracts, bh)
	if err != nil {
		return nil, err
	}

	// Upload the shards.
	return upload.uploadShards(ctx, shards, nil)
}

// managedUploadObject uploads an object and returns its metadata.
func (c *Contractor) managedUploadObject(r io.Reader, rpk types.PublicKey, bucket, path, mimeType []byte, encrypted string) (fm modules.FileMetadata, err error) {
	// Create the context and setup its cancelling.
	ctx, cancel := context.WithCancel(context.Background())
	pk := make([]byte, 32)
	copy(pk, rpk[:])
	mapKey := string(pk) + string(bucket) + ":" + string(path)
	c.mu.Lock()
	c.runningUploads[mapKey] = cancel
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.runningUploads, mapKey)
		cancel()
		c.mu.Unlock()
	}()

	// Fetch necessary params.
	c.mu.RLock()
	bh := c.tip.Height
	c.mu.RUnlock()
	contracts := c.staticContracts.ByRenter(rpk)

	// Upload the object.
	obj, ps, eTag, err := c.um.upload(ctx, r, rpk, contracts, bh)
	if err != nil {
		return
	}

	// Construct the metadata object.
	key, _ := convertEncryptionKey(obj.Key)
	fm = modules.FileMetadata{
		Key:       key,
		Bucket:    bucket,
		Path:      path,
		ETag:      eTag,
		MimeType:  mimeType,
		Encrypted: encrypted,
		Data:      ps,
	}
	for _, slab := range obj.Slabs {
		key, _ := convertEncryptionKey(slab.Key)
		s := modules.Slab{
			Key:       key,
			MinShards: slab.MinShards,
			Offset:    uint64(slab.Offset),
			Length:    uint64(slab.Length),
			Partial:   false,
		}
		for _, shard := range slab.Shards {
			s.Shards = append(s.Shards, modules.Shard{
				Host: shard.LatestHost,
				Root: shard.Root,
			})
		}
		fm.Slabs = append(fm.Slabs, s)
	}
	if len(ps) > 0 {
		key, _ := convertEncryptionKey(object.GenerateEncryptionKey())
		fm.Slabs = append(fm.Slabs, modules.Slab{
			Key:     key,
			Offset:  0,
			Length:  uint64(len(ps)),
			Partial: true,
		})
	}

	return
}

// managedUploadPackedSlab uploads a packed slab to the network.
func (c *Contractor) managedUploadPackedSlab(rpk types.PublicKey, data []byte, key object.EncryptionKey, offset uint64) (modules.Slab, error) {
	// Create a context and set up its cancelling.
	ctx, cancel := context.WithTimeout(context.Background(), packedSlabUploadTimeout)
	defer cancel()

	// Fetch the renter.
	c.mu.RLock()
	bh := c.tip.Height
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return modules.Slab{}, ErrRenterNotFound
	}

	// Upload packed slab.
	contracts := c.staticContracts.ByRenter(rpk)
	shards := encryptPartialSlab(data, key, uint8(renter.Allowance.MinShards), uint8(renter.Allowance.TotalShards))
	sectors, err := c.um.uploadShards(ctx, rpk, shards, contracts, bh)
	if err != nil {
		return modules.Slab{}, err
	}

	id, err := convertEncryptionKey(key)
	if err != nil {
		return modules.Slab{}, err
	}

	slab := modules.Slab{
		Key:       id,
		MinShards: uint8(renter.Allowance.MinShards),
		Offset:    offset,
		Length:    uint64(len(data)),
		Partial:   false,
	}
	for _, s := range sectors {
		slab.Shards = append(slab.Shards, modules.Shard{
			Host: s.LatestHost,
			Root: s.Root,
		})
	}

	return slab, nil
}

// encryptPartialSlab encrypts data and splits them into shards.
func encryptPartialSlab(data []byte, key object.EncryptionKey, minShards, totalShards uint8) [][]byte {
	slab := object.Slab{
		Key:       key,
		MinShards: minShards,
		Shards:    make([]object.Sector, totalShards),
	}
	encodedShards := make([][]byte, totalShards)
	slab.Encode(data, encodedShards)
	slab.Encrypt(encodedShards)
	return encodedShards
}

// stop stops the upload manager and all running uploads.
func (mgr *uploadManager) stop() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	close(mgr.stopChan)
	for _, u := range mgr.uploaders {
		u.stop()
	}
}

// upload uploads data to the hosts.
func (mgr *uploadManager) upload(ctx context.Context, r io.Reader, rpk types.PublicKey, contracts []modules.RenterContract, bh uint64) (_ object.Object, partialSlab []byte, eTag string, err error) {
	// Get the renter.
	mgr.contractor.mu.RLock()
	renter, exists := mgr.contractor.renters[rpk]
	mgr.contractor.mu.RUnlock()
	if !exists {
		return object.Object{}, nil, "", ErrRenterNotFound
	}

	// Cancel all in-flight requests when the upload is done.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create the object.
	o := object.NewObject(object.GenerateEncryptionKey())

	// Create the hash reader.
	hr := newHashReader(r)

	// Create the cipher reader.
	cr, err := o.Encrypt(hr, 0)
	if err != nil {
		return object.Object{}, nil, "", err
	}

	// Create the upload.
	u, err := mgr.newUpload(rpk, int(renter.Allowance.TotalShards), contracts, bh)
	if err != nil {
		return object.Object{}, nil, "", err
	}

	// Create the next slab channel.
	nextSlabChan := make(chan struct{}, 1)
	defer close(nextSlabChan)

	// Create the response channel.
	var ongoingUploads uint64
	respChan := make(chan slabUploadResponse)
	defer func() {
		// Wait for ongoing uploads to send their responses
		// down the channel before closing it.
		if atomic.LoadUint64(&ongoingUploads) > 0 {
			for range respChan {
				if atomic.LoadUint64(&ongoingUploads) == 0 {
					break
				}
			}
		}
		close(respChan)
	}()

	// Collect the responses.
	var responses []slabUploadResponse
	var slabIndex int
	numSlabs := -1

	// Prepare slab size.
	size := int64(renter.Allowance.MinShards) * rhpv2.SectorSize
loop:
	for {
		select {
		case <-mgr.stopChan:
			return object.Object{}, nil, "", errors.New("manager was stopped")
		case <-ctx.Done():
			return object.Object{}, nil, "", errors.New("upload timed out")
		case nextSlabChan <- struct{}{}:
			// Read next slab's data.
			data := make([]byte, size)
			length, err := io.ReadFull(io.LimitReader(cr, size), data)
			if err == io.EOF {
				if slabIndex == 0 {
					break loop
				}
				numSlabs = slabIndex
				if partialSlab != nil {
					numSlabs-- // Don't wait on partial slab.
				}
				if len(responses) == numSlabs {
					break loop
				}
				continue
			} else if err != nil && err != io.ErrUnexpectedEOF {
				return object.Object{}, nil, "", err
			}
			if renter.Allowance.UploadPacking && errors.Is(err, io.ErrUnexpectedEOF) {
				// If upload packing is enabled, we return the partial slab without
				// uploading.
				partialSlab = data[:length]
				<-nextSlabChan // Trigger next iteration.
			} else {
				// Otherwise we upload it.
				atomic.AddUint64(&ongoingUploads, 1)
				go func(min, total uint64, data []byte, length, slabIndex int) {
					u.uploadSlab(ctx, min, total, data, length, slabIndex, respChan, nextSlabChan)
					atomic.AddUint64(&ongoingUploads, ^uint64(0))
				}(renter.Allowance.MinShards, renter.Allowance.TotalShards, data, length, slabIndex)
			}
			slabIndex++
		case res := <-respChan:
			if res.err != nil {
				return object.Object{}, nil, "", res.err
			}

			// Collect the responses and potentially break out of the loop.
			responses = append(responses, res)
			if len(responses) == numSlabs {
				break loop
			}
		}
	}

	// Sort the slabs by index.
	sort.Slice(responses, func(i, j int) bool {
		return responses[i].index < responses[j].index
	})

	// Decorate the object with the slabs.
	for _, resp := range responses {
		o.Slabs = append(o.Slabs, resp.slab)
	}

	return o, partialSlab, hr.Hash(), nil
}

// uploadShards uploads the shards of a packed slab.
func (mgr *uploadManager) uploadShards(ctx context.Context, rpk types.PublicKey, shards [][]byte, contracts []modules.RenterContract, bh uint64) ([]object.Sector, error) {
	// Initiate the upload.
	upload, err := mgr.newUpload(rpk, len(shards), contracts, bh)
	if err != nil {
		return nil, err
	}

	// Upload the shards.
	sectors, err := upload.uploadShards(ctx, shards, nil)
	if err != nil {
		return nil, err
	}

	return sectors, nil
}

// launch starts an upload.
func (mgr *uploadManager) launch(req *sectorUploadReq) error {
	// Recompute stats.
	mgr.tryRecomputeStats()

	// Find a candidate uploader.
	uploader := mgr.candidate(req)
	if uploader == nil {
		return errNoCandidateUploader
	}
	uploader.enqueue(req)
	return nil
}

// newUpload initiates a new upload.
func (mgr *uploadManager) newUpload(rpk types.PublicKey, totalShards int, contracts []modules.RenterContract, bh uint64) (*upload, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// Refresh the uploaders.
	mgr.refreshUploaders(rpk, contracts, bh)

	// Check if we have enough contracts.
	if len(contracts) < totalShards {
		return nil, fmt.Errorf("%v < %v: %w", len(contracts), totalShards, errNotEnoughContracts)
	}

	// Create allowed map.
	allowed := make(map[types.FileContractID]struct{})
	for _, c := range contracts {
		allowed[c.ID] = struct{}{}
	}

	// Create upload.
	return &upload{
		mgr:       mgr,
		renterKey: rpk,

		allowed:          allowed,
		doneShardTrigger: make(chan struct{}, 1),

		ongoing: make([]slabID, 0),
		used:    make(map[slabID]map[types.FileContractID]struct{}),
	}, nil
}

// numUploaders returns the number of current uploaders.
func (mgr *uploadManager) numUploaders() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return len(mgr.uploaders)
}

// candidate selects an uploader from the upload manager.
func (mgr *uploadManager) candidate(req *sectorUploadReq) *uploader {
	// Fetch candidates.
	candidates := func() []*uploader {
		mgr.mu.Lock()
		defer mgr.mu.Unlock()

		// Sort the uploaders by their estimate.
		sort.Slice(mgr.uploaders, func(i, j int) bool {
			return mgr.uploaders[i].estimate() < mgr.uploaders[j].estimate()
		})

		// Select top ten candidates.
		var candidates []*uploader
		for _, uploader := range mgr.uploaders {
			if req.upload.canUseUploader(req.sID, uploader) {
				candidates = append(candidates, uploader)
				if len(candidates) == 10 {
					break
				}
			}
		}
		return candidates
	}()

	// Return early if we have no queues left.
	if len(candidates) == 0 {
		return nil
	}

loop:
	for {
		// If this slab does not have more than 1 parent, we return the best
		// candidate.
		if len(req.upload.parents(req.sID)) <= 1 {
			return candidates[0]
		}

		// Otherwise we wait, allowing the parents to complete, after which we
		// re-sort the candidates.
		select {
		case <-req.upload.doneShardTrigger:
			sort.Slice(candidates, func(i, j int) bool {
				return candidates[i].estimate() < candidates[j].estimate()
			})
			continue loop
		case <-req.ctx.Done():
			break loop
		}
	}

	return nil
}

// renewUploader updates the uploader after the contract is renewed.
func (mgr *uploadManager) renewUploader(u *uploader) {
	// Fetch renewed contract.
	fcid, _, _ := u.contractInfo()
	mgr.contractor.mu.RLock()
	renewed, exists := mgr.contractor.renewedTo[fcid]
	contract, ok := mgr.contractor.staticContracts.View(renewed)
	mgr.contractor.mu.RUnlock()

	// Remove the uploader if we can't renew it.
	mgr.mu.Lock()
	if !exists || !ok {
		mgr.contractor.log.Error("failed to fetch renewed contract for uploader", zap.Stringer("fcid", fcid))
		for i := 0; i < len(mgr.uploaders); i++ {
			if mgr.uploaders[i] == u {
				mgr.uploaders = append(mgr.uploaders[:i], mgr.uploaders[i+1:]...)
				u.stop()
				break
			}
		}
		mgr.mu.Unlock()
		return
	}
	mgr.mu.Unlock()

	// Update the uploader if we found the renewed contract.
	u.mu.Lock()
	u.fcid = renewed
	u.renewedFrom = fcid
	u.endHeight = contract.EndHeight
	u.mu.Unlock()

	u.signalWork()
}

// refreshUploaders updates the uploaders map.
func (mgr *uploadManager) refreshUploaders(rpk types.PublicKey, contracts []modules.RenterContract, bh uint64) {
	// Build map.
	c2m := make(map[types.FileContractID]modules.RenterContract)
	c2r := make(map[types.FileContractID]struct{})
	for _, c := range contracts {
		c2m[c.ID] = c
		c2r[mgr.contractor.renewedFrom[c.ID]] = struct{}{}
	}

	// Prune expired or renewed contracts.
	var refreshed []*uploader
	for _, uploader := range mgr.uploaders {
		fcid, _, endHeight := uploader.contractInfo()
		_, renewed := c2r[fcid]
		if renewed || bh > endHeight {
			uploader.stop()
			continue
		}
		refreshed = append(refreshed, uploader)
		delete(c2m, fcid)
	}

	// Create new uploaders for missing contracts.
	for _, c := range c2m {
		uploader := mgr.newUploader(c, rpk, c.HostPublicKey)
		refreshed = append(refreshed, uploader)
		go uploader.start(mgr.contractor)
	}

	// Update blockheight.
	for _, u := range refreshed {
		u.updateBlockHeight(bh)
	}
	mgr.uploaders = refreshed
}

// tryRecomputeStats tries to recompute the upload manager's stats.
func (mgr *uploadManager) tryRecomputeStats() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if time.Since(mgr.lastRecompute) < statsRecomputeMinInterval {
		return
	}

	for _, u := range mgr.uploaders {
		u.statsSectorUploadEstimateInMS.recompute()
		u.statsSectorUploadSpeedBytesPerMS.recompute()
	}
	mgr.lastRecompute = time.Now()
}

// parents returns the parents of the slab.
func (u *upload) parents(sID slabID) []slabID {
	u.mu.Lock()
	defer u.mu.Unlock()

	var parents []slabID
	for _, ongoing := range u.ongoing {
		if ongoing == sID {
			break
		}
		parents = append(parents, ongoing)
	}
	return parents
}

// finishSlabUpload completes the slab upload.
func (u *upload) finishSlabUpload(upload *slabUpload) {
	// Update ongoing slab history.
	u.mu.Lock()
	for i, prev := range u.ongoing {
		if prev == upload.sID {
			u.ongoing = append(u.ongoing[:i], u.ongoing[i+1:]...)
			break
		}
	}
	u.mu.Unlock()

	// Cleanup contexts.
	upload.mu.Lock()
	for _, shard := range upload.remaining {
		shard.cancel()
	}
	upload.mu.Unlock()
}

// newSlabUpload initiates a new slab upload.
func (u *upload) newSlabUpload(ctx context.Context, shards [][]byte) (*slabUpload, []*sectorUploadReq, chan sectorUploadResp) {
	// Create slab ID.
	var sID slabID
	frand.Read(sID[:])

	// Add to ongoing uploads.
	u.mu.Lock()
	u.ongoing = append(u.ongoing, sID)
	u.mu.Unlock()

	// Create slab upload.
	slab := &slabUpload{
		mgr: u.mgr,

		upload:  u,
		sID:     sID,
		created: time.Now(),
		shards:  shards,

		overdriving: make(map[int]int, len(shards)),
		remaining:   make(map[int]sectorCtx, len(shards)),
		sectors:     make([]object.Sector, len(shards)),
	}

	// Prepare sector uploads.
	responseChan := make(chan sectorUploadResp)
	requests := make([]*sectorUploadReq, len(shards))
	for sI, shard := range shards {
		// Create the sector upload's cancel func.
		sCtx, cancel := context.WithCancel(ctx)
		slab.remaining[sI] = sectorCtx{ctx: sCtx, cancel: cancel}

		// Create the sector upload.
		requests[sI] = &sectorUploadReq{
			upload: u,
			sID:    sID,
			ctx:    sCtx,

			sector:       (*[rhpv2.SectorSize]byte)(shard),
			sectorIndex:  sI,
			responseChan: responseChan,
		}
	}

	return slab, requests, responseChan
}

// canUseUploader returns if the uploader can be used for uploads.
func (u *upload) canUseUploader(sID slabID, ul *uploader) bool {
	fcid, renewedFrom, _ := ul.contractInfo()

	u.mu.Lock()
	defer u.mu.Unlock()

	// Check if the uploader belongs to the same renter.
	if ul.renterKey != u.renterKey {
		return false
	}

	// Check if the uploader is allowed.
	_, allowed := u.allowed[fcid]
	if !allowed {
		_, allowed = u.allowed[renewedFrom]
	}
	if !allowed {
		return false
	}

	// Check whether we've used it already.
	_, used := u.used[sID][fcid]
	if !used {
		_, used = u.used[sID][renewedFrom]
	}
	return !used
}

// uploadSlab uploads a slab to the host.
func (u *upload) uploadSlab(ctx context.Context, min, total uint64, data []byte, length, index int, respChan chan slabUploadResponse, nextSlabChan chan struct{}) {
	// Cancel any sector uploads once the slab is done.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create the response.
	resp := slabUploadResponse{
		slab: object.SlabSlice{
			Slab:   object.NewSlab(uint8(min)),
			Offset: 0,
			Length: uint32(length),
		},
		index: index,
	}

	// Create the shards.
	shards := make([][]byte, total)
	resp.slab.Slab.Encode(data, shards)
	resp.slab.Slab.Encrypt(shards)

	// Upload the shards.
	resp.slab.Slab.Shards, resp.err = u.uploadShards(ctx, shards, nextSlabChan)

	// Send the response.
	select {
	case <-ctx.Done():
	case respChan <- resp:
	}
}

// markUsed marks the host as used.
func (u *upload) markUsed(sID slabID, fcid types.FileContractID) {
	u.mu.Lock()
	defer u.mu.Unlock()

	_, exists := u.used[sID]
	if !exists {
		u.used[sID] = make(map[types.FileContractID]struct{})
	}
	u.used[sID][fcid] = struct{}{}
}

// uploadShards uses a series of requests to upload a slab.
func (u *upload) uploadShards(ctx context.Context, shards [][]byte, nextSlabChan chan struct{}) ([]object.Sector, error) {
	// Prepare the upload.
	slab, requests, respChan := u.newSlabUpload(ctx, shards)
	defer u.finishSlabUpload(slab)

	// Launch all shard uploads.
	for _, upload := range requests {
		if _, err := slab.launch(upload); err != nil {
			return nil, err
		}
	}

	// Launch overdrive.
	resetOverdrive := slab.overdrive(ctx, respChan)

	// Collect responses.
	var done bool
	var next bool
	var triggered bool
	for slab.inflight() > 0 && !done {
		var resp sectorUploadResp
		select {
		case <-u.mgr.stopChan:
			return nil, errors.New("upload stopped")
		case <-ctx.Done():
			return nil, ctx.Err()
		case resp = <-respChan:
		}

		resetOverdrive()

		// Receive the response.
		done, next = slab.receive(resp)

		// Try and trigger next slab.
		if next && !triggered {
			select {
			case <-nextSlabChan:
				triggered = true
			default:
			}
		}

		// Relaunch non-overdrive uploads.
		if !done && resp.err != nil && !resp.req.overdrive {
			if overdriving, err := slab.launch(resp.req); err != nil {
				u.mgr.contractor.log.Error("failed to relaunch a sector upload", zap.Error(err))
				if !overdriving {
					break // Fail the upload.
				}
			}
		}

		// Handle the response.
		if resp.err == nil {
			// Signal the upload a shard was received.
			select {
			case u.doneShardTrigger <- struct{}{}:
			default:
			}
		}
	}

	// Make sure next slab is triggered.
	if done && !triggered {
		select {
		case <-nextSlabChan:
			triggered = true
		default:
		}
	}

	// Track stats.
	u.mgr.statsOverdrivePct.track(slab.overdrivePct())
	u.mgr.statsSlabUploadSpeedBytesPerMS.track(float64(slab.uploadSpeed()))
	return slab.finish()
}

// contractInfo returns the contract information related to the upload.
func (u *uploader) contractInfo() (types.FileContractID, types.FileContractID, uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.fcid, u.renewedFrom, u.endHeight
}

// signalWork signals that there is an upload request waiting.
func (u *uploader) signalWork() {
	select {
	case u.signalNewUpload <- struct{}{}:
	default:
	}
}

// start starts the upload.
func (u *uploader) start(c *Contractor) {
outer:
	for {
		// Wait for work.
		select {
		case <-u.signalNewUpload:
		case <-u.stopChan:
			return
		}

		for {
			// Check if we are stopped.
			select {
			case <-u.stopChan:
				return
			default:
			}

			// Pop the next upload.
			req := u.pop()
			if req == nil {
				continue outer
			}

			// Skip if upload is done.
			if req.done() {
				continue
			}

			// Execute it.
			start := time.Now()
			root, err := u.execute(req)

			// The uploader's contract got renewed, requeue the request, try and refresh the contract.
			if errors.Is(err, errMaxRevisionReached) {
				u.requeue(req)
				u.mgr.renewUploader(u)
				continue outer
			}

			// Send the response.
			if err != nil {
				req.fail(err)
			} else {
				req.succeed(root)
			}

			// Track the error, ignore gracefully closed streams and canceled overdrives.
			canceledOverdrive := req.done() && req.overdrive && err != nil
			if !canceledOverdrive && !errors.Is(err, mux.ErrClosedStream) && !errors.Is(err, net.ErrClosed) {
				u.trackSectorUpload(err, time.Since(start))
			}
		}
	}
}

// stop stops an uploader and the associated uploading process.
func (u *uploader) stop() {
	close(u.stopChan)

	// Clear the queue.
	for {
		upload := u.pop()
		if upload == nil {
			break
		}
		if !upload.done() {
			upload.fail(errors.New("uploader stopped"))
		}
	}
}

// execute executes an upload request.
func (u *uploader) execute(req *sectorUploadReq) (types.Hash256, error) {
	u.mu.Lock()
	rpk := u.renterKey
	hpk := u.hostKey
	u.mu.Unlock()

	// Upload the sector.
	root, err := u.mgr.contractor.managedUploadSector(req.ctx, rpk, hpk, req.sector)
	if err != nil {
		return types.Hash256{}, err
	}

	return root, nil
}

// estimate returns the estimated upload duration.
func (u *uploader) estimate() float64 {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Fetch estimated duration per sector.
	estimateP90 := u.statsSectorUploadEstimateInMS.getP90()
	if estimateP90 == 0 {
		estimateP90 = 1
	}

	// Calculate estimated time.
	numSectors := float64(len(u.queue) + 1)
	return numSectors * estimateP90
}

// requeue adds an upload request to the beginning of the queue.
func (u *uploader) requeue(req *sectorUploadReq) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.queue = append([]*sectorUploadReq{req}, u.queue...)
}

// enqueue adds an upload request to the end of the queue.
func (u *uploader) enqueue(req *sectorUploadReq) {
	// Set the host key and enqueue the request.
	u.mu.Lock()
	req.hostKey = u.hostKey
	u.queue = append(u.queue, req)
	u.mu.Unlock()

	// Mark as used.
	fcid, _, _ := u.contractInfo()
	req.upload.markUsed(req.sID, fcid)

	// Signal there's work.
	u.signalWork()
}

// trackSectorUpload records the upload stats.
func (u *uploader) trackSectorUpload(err error, d time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err != nil {
		u.consecutiveFailures++
		u.statsSectorUploadEstimateInMS.track(float64(time.Hour.Milliseconds()))
	} else {
		ms := d.Milliseconds()
		u.consecutiveFailures = 0
		u.statsSectorUploadEstimateInMS.track(float64(ms))                       // Duration in ms.
		u.statsSectorUploadSpeedBytesPerMS.track(float64(rhpv2.SectorSize / ms)) // Bytes per ms.
	}
}

// updateBlockHeight updates the uploader's block height.
func (u *uploader) updateBlockHeight(bh uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.bh = bh
}

// pop removes a request from the queue and returns it.
func (u *uploader) pop() *sectorUploadReq {
	u.mu.Lock()
	defer u.mu.Unlock()

	if len(u.queue) > 0 {
		j := u.queue[0]
		u.queue[0] = nil
		u.queue = u.queue[1:]
		return j
	}
	return nil
}

// succeed reports a successful upload.
func (req *sectorUploadReq) succeed(root types.Hash256) {
	select {
	case <-req.ctx.Done():
	case req.responseChan <- sectorUploadResp{
		req:  req,
		root: root,
	}:
	}
}

// fail reports a failed upload.
func (req *sectorUploadReq) fail(err error) {
	select {
	case <-req.ctx.Done():
	case req.responseChan <- sectorUploadResp{
		req: req,
		err: err,
	}:
	}
}

// done returns whether the upload request is completed.
func (req *sectorUploadReq) done() bool {
	select {
	case <-req.ctx.Done():
		return true
	default:
		return false
	}
}

// uploadSpeed returns the measured upload speed.
func (s *slabUpload) uploadSpeed() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	totalShards := len(s.sectors)
	completedShards := totalShards - len(s.remaining)
	bytes := completedShards * rhpv2.SectorSize
	ms := time.Since(s.created).Milliseconds()
	return int64(bytes) / ms
}

// finish completes the upload.
func (s *slabUpload) finish() ([]object.Sector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	remaining := len(s.remaining)
	if remaining > 0 {
		return nil, fmt.Errorf("failed to upload slab: remaining=%d, inflight=%d, launched=%d uploaders=%d errors=%d %w", remaining, s.numInflight, s.numLaunched, s.mgr.numUploaders(), len(s.errs), s.errs)
	}
	return s.sectors, nil
}

// inflight returns the number of inflight requests
func (s *slabUpload) inflight() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.numInflight
}

// launch starts an upload request.
func (s *slabUpload) launch(req *sectorUploadReq) (overdriving bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Nothing to do.
	if req == nil {
		return false, nil
	}

	// Launch the req.
	err = s.mgr.launch(req)
	if err != nil {
		overdriving = req.overdrive && s.overdriving[req.sectorIndex] > 0
		return
	}

	// Update the state.
	s.numInflight++
	s.numLaunched++
	if req.overdrive {
		s.lastOverdrive = time.Now()
		s.overdriving[req.sectorIndex]++
		overdriving = true
	}

	return
}

// overdrive spins up new workers when required.
func (s *slabUpload) overdrive(ctx context.Context, respChan chan sectorUploadResp) (resetTimer func()) {
	// Overdrive is disabled.
	if s.mgr.overdriveTimeout == 0 {
		return func() {}
	}

	// Create a timer to trigger overdrive.
	timer := time.NewTimer(s.mgr.overdriveTimeout)
	resetTimer = func() {
		timer.Stop()
		select {
		case <-timer.C:
		default:
		}
		timer.Reset(s.mgr.overdriveTimeout)
	}

	// Create a function to check whether overdrive is possible.
	canOverdrive := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Overdrive is not kicking in yet.
		if uint64(len(s.remaining)) >= s.mgr.maxOverdrive {
			return false
		}

		// Overdrive is not due yet.
		if time.Since(s.lastOverdrive) < s.mgr.overdriveTimeout {
			return false
		}

		// Overdrive is maxed out.
		if s.numInflight-uint64(len(s.remaining)) >= s.mgr.maxOverdrive {
			return false
		}

		return true
	}

	// Try overdriving every time the timer fires.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if canOverdrive() {
					_, _ = s.launch(s.nextRequest(respChan)) // Ignore result.
				}
				resetTimer()
			}
		}
	}()

	return
}

// nextRequest returns the next request from the queue.
func (s *slabUpload) nextRequest(responseChan chan sectorUploadResp) *sectorUploadReq {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Overdrive the remaining sector with the least number of overdrives.
	lowestSI := -1
	s.overdriving[lowestSI] = math.MaxInt
	for sI := range s.remaining {
		if s.overdriving[sI] < s.overdriving[lowestSI] {
			lowestSI = sI
		}
	}
	if lowestSI == -1 {
		return nil
	}

	return &sectorUploadReq{
		upload: s.upload,
		sID:    s.sID,
		ctx:    s.remaining[lowestSI].ctx,

		overdrive:    true,
		responseChan: responseChan,

		sectorIndex: lowestSI,
		sector:      (*[rhpv2.SectorSize]byte)(s.shards[lowestSI]),
	}
}

// overdrivePct returns the percentage of the overdrive workers.
func (s *slabUpload) overdrivePct() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	numOverdrive := int(s.numLaunched) - len(s.sectors)
	if numOverdrive <= 0 {
		return 0
	}

	return float64(numOverdrive) / float64(len(s.sectors))
}

// receive processes an upload response.
func (s *slabUpload) receive(resp sectorUploadResp) (finished bool, next bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update the state.
	if resp.req.overdrive {
		s.overdriving[resp.req.sectorIndex]--
	}

	// Failed reqs can't complete the upload.
	s.numInflight--
	if resp.err != nil {
		s.errs = append(s.errs, &hostError{resp.req.hostKey, resp.err})
		return false, false
	}

	// Redundant sectors can't complete the upload.
	if s.sectors[resp.req.sectorIndex].Root != (types.Hash256{}) {
		return false, false
	}

	// Store the sector and call cancel on the sector ctx.
	s.sectors[resp.req.sectorIndex] = object.Sector{
		LatestHost: resp.req.hostKey,
		Root:       resp.root,
	}
	s.remaining[resp.req.sectorIndex].cancel()

	// Update remaining sectors.
	delete(s.remaining, resp.req.sectorIndex)
	finished = len(s.remaining) == 0
	next = len(s.remaining) <= int(s.mgr.maxOverdrive)
	return
}

// average returns the average.
func (a *dataPoints) average() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	avg, err := a.Mean()
	if err != nil {
		avg = 0
	}
	return avg
}

// getP90 returns the 90th percentile.
func (a *dataPoints) getP90() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.p90
}

// recompute recalculates the 90th percentile.
func (a *dataPoints) recompute() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Apply decay.
	a.tryDecay()

	// Recalculate the p90.
	p90, err := a.Percentile(90)
	if err != nil {
		p90 = 0
	}
	a.p90 = p90
}

// track adds a new datapoint.
func (a *dataPoints) track(p float64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cnt < a.size {
		a.Float64Data = append(a.Float64Data, p)
	} else {
		a.Float64Data[a.cnt%a.size] = p
	}

	a.lastDatapoint = time.Now()
	a.cnt++
}

// tryDecay applies a decay if required.
func (a *dataPoints) tryDecay() {
	// Return if decay is disabled.
	if a.halfLife == 0 {
		return
	}

	// Return if decay is not needed.
	if time.Since(a.lastDatapoint) < statsDecayThreshold {
		return
	}

	// Return if decay is not due.
	decayFreq := a.halfLife / 5
	timePassed := time.Since(a.lastDecay)
	if timePassed < decayFreq {
		return
	}

	// Calculate decay and apply it.
	strength := float64(timePassed) / float64(a.halfLife)
	decay := math.Floor(math.Pow(0.5, strength)*100) / 100 // round down to 2 decimals
	for i := range a.Float64Data {
		a.Float64Data[i] *= decay
	}

	// Update the last decay time.
	a.lastDecay = time.Now()
}

type hashReader struct {
	r io.Reader
	h *types.Hasher
}

func newHashReader(r io.Reader) *hashReader {
	return &hashReader{
		r: r,
		h: types.NewHasher(),
	}
}

func (e *hashReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if _, wErr := e.h.E.Write(p[:n]); wErr != nil {
		return 0, wErr
	}
	return n, err
}

func (e *hashReader) Hash() string {
	sum := e.h.Sum()
	return hex.EncodeToString(sum[:])
}

// managedUploadSector uploads a single sector to the host.
func (c *Contractor) managedUploadSector(ctx context.Context, rpk, hpk types.PublicKey, sector *[rhpv2.SectorSize]byte) (root types.Hash256, err error) {
	// Get the renter.
	c.mu.RLock()
	renter, exists := c.renters[rpk]
	c.mu.RUnlock()
	if !exists {
		return types.Hash256{}, ErrRenterNotFound
	}

	// Get the contract ID.
	var fcid types.FileContractID
	contracts := c.staticContracts.ByRenter(rpk)
	for _, contract := range contracts {
		if contract.HostPublicKey == hpk {
			fcid = contract.ID
			break
		}
	}
	if fcid == (types.FileContractID{}) {
		return types.Hash256{}, errors.New("couldn't find the contract")
	}

	// Fetch the host.
	host, exists, err := c.hdb.Host(hpk)
	if err != nil {
		return types.Hash256{}, modules.AddContext(err, "error getting host from hostdb")
	}
	if !exists {
		return types.Hash256{}, errHostNotFound
	}
	hostName, _, err := net.SplitHostPort(string(host.Settings.NetAddress))
	if err != nil {
		return types.Hash256{}, modules.AddContext(err, "failed to get host name")
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

	// Derive the ephemeral keys.
	esk := modules.DeriveEphemeralKey(renter.PrivateKey, hpk)
	ak := modules.DeriveAccountKey(renter.AccountKey, hpk)

	// Derive the account ID.
	accountID := rhpv3.Account(ak.PublicKey())

	// Initiate the protocol.
	err = proto.WithTransportV3(ctx, siamuxAddr, hpk, func(t *rhpv3.Transport) (err error) {
		// Fetch the latest revision.
		rev, err := proto.RPCLatestRevision(ctx, t, fcid)
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get latest revision")
		}
		if rev.RevisionNumber == math.MaxUint64 {
			return errMaxRevisionReached
		}

		// Get the cost estimation.
		cost, _, _, err := proto.UploadSectorCost(host.PriceTable, rev.WindowEnd)
		if err != nil {
			return modules.AddContext(err, "unable to estimate costs")
		}

		// If the cost is greater than MaxEphemeralAccountBalance, we pay by contract.
		var byContract bool
		if cost.Cmp(host.Settings.MaxEphemeralAccountBalance) > 0 {
			byContract = true
		}

		// Fetch the price table.
		pt, err := proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
			payment := rhpv3.PayByEphemeralAccount(accountID, pt.UpdatePriceTableCost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
			return &payment, nil
		})
		if errors.Is(err, errBalanceInsufficient) {
			pt, err = proto.RPCPriceTable(ctx, t, func(pt rhpv3.HostPriceTable) (rhpv3.PaymentMethod, error) {
				payment, err := payByContract(&rev, pt.UpdatePriceTableCost, accountID, esk)
				if err != nil {
					return nil, err
				}
				return &payment, nil
			})
		}
		if err != nil {
			hostFault = true
			return modules.AddContext(err, "unable to get price table")
		}

		if byContract {
			payment, ok := rhpv3.PayByContract(&rev, cost, accountID, esk)
			if !ok {
				return errors.New("failed to create payment")
			}

			root, _, err = proto.RPCAppendSector(ctx, t, esk, pt, rev, &payment, sector)
			if err != nil {
				hostFault = true
				return err
			}

			// Update the contract.
			err = c.UpdateContract(rev, nil, cost, types.ZeroCurrency, types.ZeroCurrency)
			if err != nil {
				return modules.AddContext(err, "unable to update contract")
			}

			return nil
		}

		// Fetch the account balance.
		payment := rhpv3.PayByEphemeralAccount(accountID, pt.AccountBalanceCost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
		curr, err := proto.RPCAccountBalance(ctx, t, &payment, accountID, pt.UID)
		if errors.Is(err, errBalanceInsufficient) {
			payment, err := payByContract(&rev, pt.AccountBalanceCost, accountID, esk)
			if err != nil {
				return err
			}
			curr, err = proto.RPCAccountBalance(ctx, t, &payment, accountID, pt.UID)
			if err != nil {
				return err
			}
		}
		if err != nil {
			hostFault = true
			return err
		}

		// Fund the account if the balance is insufficient.
		if curr.Cmp(cost) < 0 {
			amount := cost.Sub(curr)

			// Cap the amount by the amount of money left in the contract.
			renterFunds := rev.ValidRenterPayout()
			possibleFundCost := pt.FundAccountCost.Add(pt.UpdatePriceTableCost)
			if renterFunds.Cmp(possibleFundCost) <= 0 {
				return fmt.Errorf("insufficient funds to fund account: %v <= %v", renterFunds, possibleFundCost)
			} else if maxAmount := renterFunds.Sub(possibleFundCost); maxAmount.Cmp(amount) < 0 {
				amount = maxAmount
			}

			amount = amount.Add(pt.FundAccountCost)
			pmt, err := payByContract(&rev, amount, rhpv3.Account{}, esk) // No account needed for funding.
			if err != nil {
				return err
			}
			if err := proto.RPCFundAccount(ctx, t, &pmt, accountID, pt.UID); err != nil {
				hostFault = true
				return fmt.Errorf("failed to fund account with %v; %w", amount, err)
			}

			// Update the contract.
			err = c.UpdateContract(rev, nil, types.ZeroCurrency, types.ZeroCurrency, amount)
			if err != nil {
				return modules.AddContext(err, "unable to update contract")
			}
		}

		// Upload the data.
		payment = rhpv3.PayByEphemeralAccount(accountID, cost, pt.HostBlockHeight+defaultWithdrawalExpiryBlocks, ak)
		root, _, err = proto.RPCAppendSector(ctx, t, esk, pt, rev, &payment, sector)
		if err != nil {
			hostFault = true
			return err
		}

		return nil
	})

	return
}
