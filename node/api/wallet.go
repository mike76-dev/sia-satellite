package api

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/julienschmidt/httprouter"
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/modules/wallet"

	"go.sia.tech/core/types"

	"golang.org/x/crypto/blake2b"

	"lukechampine.com/frand"
)

type (
	// WalletGET contains general information about the wallet.
	WalletGET struct {
		Encrypted  bool   `json:"encrypted"`
		Height     uint64 `json:"height"`
		Rescanning bool   `json:"rescanning"`
		Unlocked   bool   `json:"unlocked"`

		ConfirmedSiacoinBalance     types.Currency `json:"confirmedsiacoinbalance"`
		UnconfirmedOutgoingSiacoins types.Currency `json:"unconfirmedoutgoingsiacoins"`
		UnconfirmedIncomingSiacoins types.Currency `json:"unconfirmedincomingsiacoins"`

		DustThreshold types.Currency `json:"dustthreshold"`
	}

	// WalletAddressGET contains an address returned by a GET call to
	// /wallet/address.
	WalletAddressGET struct {
		Address types.Address `json:"address"`
	}

	// WalletAddressesGET contains the list of wallet addresses returned by a
	// GET call to /wallet/addresses.
	WalletAddressesGET struct {
		Addresses []types.Address `json:"addresses"`
	}

	// WalletInitPOST contains the primary seed that gets generated during a
	// POST call to /wallet/init.
	WalletInitPOST struct {
		PrimarySeed string `json:"primaryseed"`
	}

	// WalletSiacoinsPOST contains the transaction sent in the POST call to
	// /wallet/siacoins.
	WalletSiacoinsPOST struct {
		Transactions   []types.Transaction   `json:"transactions"`
		TransactionIDs []types.TransactionID `json:"transactionids"`
	}

	// WalletSignPOSTParams contains the unsigned transaction and a set of
	// inputs to sign.
	WalletSignPOSTParams struct {
		Transaction types.Transaction `json:"transaction"`
		ToSign      []types.Hash256   `json:"tosign"`
	}

	// WalletSignPOSTResp contains the signed transaction.
	WalletSignPOSTResp struct {
		Transaction types.Transaction `json:"transaction"`
	}

	// WalletSeedsGET contains the seeds used by the wallet.
	WalletSeedsGET struct {
		PrimarySeed        string   `json:"primaryseed"`
		AddressesRemaining int      `json:"addressesremaining"`
		AllSeeds           []string `json:"allseeds"`
	}

	// WalletSweepPOST contains the coins returned by a call to
	// /wallet/sweep.
	WalletSweepPOST struct {
		Coins types.Currency `json:"coins"`
	}

	// WalletTransactionGETid contains the transaction returned by a call to
	// /wallet/transaction/:id
	WalletTransactionGETid struct {
		Transaction modules.ProcessedTransaction `json:"transaction"`
	}

	// WalletTransactionsGET contains the specified set of confirmed and
	// unconfirmed transactions.
	WalletTransactionsGET struct {
		ConfirmedTransactions   []modules.ProcessedTransaction `json:"confirmedtransactions"`
		UnconfirmedTransactions []modules.ProcessedTransaction `json:"unconfirmedtransactions"`
	}

	// WalletTransactionsGETaddr contains the set of wallet transactions
	// relevant to the input address provided in the call to
	// /wallet/transaction/:addr.
	WalletTransactionsGETaddr struct {
		ConfirmedTransactions   []modules.ProcessedTransaction `json:"confirmedtransactions"`
		UnconfirmedTransactions []modules.ProcessedTransaction `json:"unconfirmedtransactions"`
	}

	// WalletUnlockConditionsGET contains a set of unlock conditions.
	WalletUnlockConditionsGET struct {
		UnlockConditions types.UnlockConditions `json:"unlockconditions"`
	}

	// WalletUnlockConditionsPOSTParams contains a set of unlock conditions.
	WalletUnlockConditionsPOSTParams struct {
		UnlockConditions types.UnlockConditions `json:"unlockconditions"`
	}

	// WalletUnspentGET contains the unspent outputs tracked by the wallet.
	// The MaturityHeight field of each output indicates the height of the
	// block that the output appeared in.
	WalletUnspentGET struct {
		Outputs []modules.UnspentOutput `json:"outputs"`
	}

	// WalletVerifyAddressGET contains a bool indicating if the address passed to
	// /wallet/verify/address/:addr is a valid address.
	WalletVerifyAddressGET struct {
		Valid bool `json:"valid"`
	}

	// WalletVerifyPasswordGET contains a bool indicating if the password passed
	// to /wallet/verifypassword is the password being used to encrypt the
	// wallet.
	WalletVerifyPasswordGET struct {
		Valid bool `json:"valid"`
	}

	// WalletWatchPOST contains the set of addresses to add or remove from the
	// watch set.
	WalletWatchPOST struct {
		Addresses []types.Address `json:"addresses"`
		Remove    bool            `json:"remove"`
		Unused    bool            `json:"unused"`
	}

	// WalletWatchGET contains the set of addresses that the wallet is
	// currently watching.
	WalletWatchGET struct {
		Addresses []types.Address `json:"addresses"`
	}
)

// RegisterRoutesWallet is a helper function to register all wallet routes.
func RegisterRoutesWallet(router *httprouter.Router, wallet modules.Wallet, requiredPassword string) {
	router.GET("/wallet", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletHandler(wallet, w, req, ps)
	})
	router.GET("/wallet/address", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletAddressHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.GET("/wallet/addresses", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletAddressesHandler(wallet, w, req, ps)
	})
	router.GET("/wallet/seedaddrs", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletSeedAddressesHandler(wallet, w, req, ps)
	})
	router.POST("/wallet/init", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletInitHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/init/seed", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletInitSeedHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/lock", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletLockHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/seed", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletSeedHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.GET("/wallet/seeds", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletSeedsHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/siacoins", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletSiacoinsHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/sweep/seed", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletSweepSeedHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.GET("/wallet/transaction/:id", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletTransactionHandler(wallet, w, req, ps)
	})
	router.GET("/wallet/transactions", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletTransactionsHandler(wallet, w, req, ps)
	})
	router.GET("/wallet/transactions/:addr", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletTransactionsAddrHandler(wallet, w, req, ps)
	})
	router.GET("/wallet/verify/address/:addr", func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletVerifyAddressHandler(w, req, ps)
	})
	router.POST("/wallet/unlock", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletUnlockHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/changepassword", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletChangePasswordHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.GET("/wallet/verifypassword", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletVerifyPasswordHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.GET("/wallet/unlockconditions/:addr", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletUnlockConditionsHandlerGET(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/unlockconditions", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletUnlockConditionsHandlerPOST(wallet, w, req, ps)
	}, requiredPassword))
	router.GET("/wallet/unspent", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletUnspentHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/sign", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletSignHandler(wallet, w, req, ps)
	}, requiredPassword))
	router.GET("/wallet/watch", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletWatchHandlerGET(wallet, w, req, ps)
	}, requiredPassword))
	router.POST("/wallet/watch", RequirePassword(func(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
		walletWatchHandlerPOST(wallet, w, req, ps)
	}, requiredPassword))
}

// encryptionKeys enumerates the possible encryption keys that can be derived
// from an input string.
func encryptionKeys(seedStr string) (validKeys []modules.WalletKey, seed modules.Seed) {
	key, err := modules.KeyFromPhrase(seedStr)
	if err == nil {
		h := blake2b.Sum256(key[:])
		wk := make([]byte, len(h))
		copy(wk, h[:])
		validKeys = append(validKeys, modules.WalletKey(wk))
		frand.Read(h[:])
	}
	h := blake2b.Sum256([]byte(seedStr))
	buf := make([]byte, 32 + 8)
	copy(buf[:32], h[:])
	binary.LittleEndian.PutUint64(buf[32:], 0)
	h = blake2b.Sum256(buf)
	key = types.NewPrivateKeyFromSeed(h[:])
	h = blake2b.Sum256(key[:])
	wk := make([]byte, len(h))
	copy(wk, h[:])
	validKeys = append(validKeys, modules.WalletKey(wk))
	frand.Read(h[:])
	seed, err = modules.DecodeBIP39Phrase(seedStr)
	if err != nil {
		return nil, modules.Seed{}
	}
	return
}

// walletHander handles API calls to /wallet.
func walletHandler(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	siacoinBal, _, _, err := wallet.ConfirmedBalance()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet: %v", err)}, http.StatusBadRequest)
		return
	}
	siacoinsOut, siacoinsIn, err := wallet.UnconfirmedBalance()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet: %v", err)}, http.StatusBadRequest)
		return
	}
	dustThreshold, err := wallet.DustThreshold()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet: %v", err)}, http.StatusBadRequest)
		return
	}
	encrypted, err := wallet.Encrypted()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet: %v", err)}, http.StatusBadRequest)
		return
	}
	unlocked, err := wallet.Unlocked()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet: %v", err)}, http.StatusBadRequest)
		return
	}
	rescanning, err := wallet.Rescanning()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet: %v", err)}, http.StatusBadRequest)
		return
	}
	height, err := wallet.Height()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet: %v", err)}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletGET{
		Encrypted:  encrypted,
		Unlocked:   unlocked,
		Rescanning: rescanning,
		Height:     height,

		ConfirmedSiacoinBalance:     siacoinBal,
		UnconfirmedOutgoingSiacoins: siacoinsOut,
		UnconfirmedIncomingSiacoins: siacoinsIn,

		DustThreshold: dustThreshold,
	})
}

// walletAddressHandler handles API calls to /wallet/address.
func walletAddressHandler(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	unlockConditions, err := wallet.NextAddress()
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/addresses: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletAddressGET{
		Address: unlockConditions.UnlockHash(),
	})
}

// walletSeedAddressesHandler handles the requests to /wallet/seedaddrs.
func walletSeedAddressesHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Parse the count argument. If it isn't specified we return as many
	// addresses as possible.
	count := uint64(math.MaxUint64)
	c := req.FormValue("count")
	if c != "" {
		_, err := fmt.Sscan(c, &count)
		if err != nil {
			WriteError(w, Error{"Failed to parse count: " + err.Error()}, http.StatusBadRequest)
			return
		}
	}
	// Get the last count addresses.
	addresses, err := wallet.LastAddresses(count)
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet/addresses: %v", err)}, http.StatusBadRequest)
		return
	}
	// Send the response.
	WriteJSON(w, WalletAddressesGET{
		Addresses: addresses,
	})
}

// walletAddressHandler handles API calls to /wallet/addresses.
func walletAddressesHandler(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	addresses, err := wallet.AllAddresses()
	if err != nil {
		WriteError(w, Error{fmt.Sprintf("Error when calling /wallet/addresses: %v", err)}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletAddressesGET{
		Addresses: addresses,
	})
}

// walletInitHandler handles API calls to /wallet/init.
func walletInitHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var encryptionKey modules.WalletKey
	if req.FormValue("encryptionpassword") != "" {
		h := blake2b.Sum256([]byte(req.FormValue("encryptionpassword")))
		buf := make([]byte, 32 + 8)
		copy(buf[:32], h[:])
		binary.LittleEndian.PutUint64(buf[32:], 0)
		h = blake2b.Sum256(buf)
		key := types.NewPrivateKeyFromSeed(h[:])
		h = blake2b.Sum256(key[:])
		wk := make([]byte, len(h))
		copy(wk, h[:])
		encryptionKey = modules.WalletKey(wk)
		frand.Read(h[:])
	}

	if req.FormValue("force") == "true" {
		err := wallet.Reset()
		if err != nil {
			WriteError(w, Error{"error when calling /wallet/init: " + err.Error()}, http.StatusBadRequest)
			return
		}
	}
	seed, err := wallet.Encrypt(encryptionKey)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/init: " + err.Error()}, http.StatusBadRequest)
		return
	}

	seedStr := modules.EncodeBIP39Phrase(seed)
	WriteJSON(w, WalletInitPOST{
		PrimarySeed: seedStr,
	})
}

// walletInitSeedHandler handles API calls to /wallet/init/seed.
func walletInitSeedHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var encryptionKey modules.WalletKey
	if req.FormValue("encryptionpassword") != "" {
		h := blake2b.Sum256([]byte(req.FormValue("encryptionpassword")))
		buf := make([]byte, 32 + 8)
		copy(buf[:32], h[:])
		binary.LittleEndian.PutUint64(buf[32:], 0)
		h = blake2b.Sum256(buf)
		key := types.NewPrivateKeyFromSeed(h[:])
		h = blake2b.Sum256(key[:])
		wk := make([]byte, len(h))
		copy(wk, h[:])
		encryptionKey = modules.WalletKey(wk)
		frand.Read(h[:])
	}
	seed, err := modules.DecodeBIP39Phrase(req.FormValue("seed"))
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/init/seed: " + err.Error()}, http.StatusBadRequest)
		return
	}

	if req.FormValue("force") == "true" {
		err = wallet.Reset()
		if err != nil {
			WriteError(w, Error{"error when calling /wallet/init/seed: " + err.Error()}, http.StatusBadRequest)
			return
		}
	}

	err = wallet.InitFromSeed(encryptionKey, seed)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/init/seed: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// walletSeedHandler handles API calls to /wallet/seed.
func walletSeedHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get the seed using the dictionary.
	seed, err := modules.DecodeBIP39Phrase(req.FormValue("seed"))
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/seed: " + err.Error()}, http.StatusBadRequest)
		return
	}

	potentialKeys, _ := encryptionKeys(req.FormValue("encryptionpassword"))
	for _, key := range potentialKeys {
		err := wallet.LoadSeed(key, seed)
		if err == nil {
			WriteSuccess(w)
			return
		}
		if !modules.ContainsError(err, modules.ErrBadEncryptionKey) {
			WriteError(w, Error{"error when calling /wallet/seed: " + err.Error()}, http.StatusBadRequest)
			return
		}
	}
	WriteError(w, Error{"error when calling /wallet/seed: " + modules.ErrBadEncryptionKey.Error()}, http.StatusBadRequest)
}

// walletLockHandler handles API calls to /wallet/lock.
func walletLockHandler(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	err := wallet.Lock()
	if err != nil {
		WriteError(w, Error{err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// walletSeedsHandler handles API calls to /wallet/seeds.
func walletSeedsHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get the primary seed information.
	primarySeed, addrsRemaining, err := wallet.PrimarySeed()
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/seeds: " + err.Error()}, http.StatusBadRequest)
		return
	}
	primarySeedStr := modules.EncodeBIP39Phrase(primarySeed)

	// Get the list of seeds known to the wallet.
	allSeeds, err := wallet.AllSeeds()
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/seeds: " + err.Error()}, http.StatusBadRequest)
		return
	}
	var allSeedsStrs []string
	for _, seed := range allSeeds {
		str := modules.EncodeBIP39Phrase(seed)
		allSeedsStrs = append(allSeedsStrs, str)
	}
	WriteJSON(w, WalletSeedsGET{
		PrimarySeed:        primarySeedStr,
		AddressesRemaining: int(addrsRemaining),
		AllSeeds:           allSeedsStrs,
	})
}

// walletSiacoinsHandler handles API calls to /wallet/siacoins.
func walletSiacoinsHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var txns []types.Transaction
	if req.FormValue("outputs") != "" {
		// Multiple amounts + destinations.
		if req.FormValue("amount") != "" || req.FormValue("destination") != "" || req.FormValue("feeIncluded") != "" {
			WriteError(w, Error{"cannot supply both 'outputs' and single amount+destination pair and/or feeIncluded parameter"}, http.StatusInternalServerError)
			return
		}

		var outputs []types.SiacoinOutput
		err := json.Unmarshal([]byte(req.FormValue("outputs")), &outputs)
		if err != nil {
			WriteError(w, Error{"could not decode outputs: " + err.Error()}, http.StatusInternalServerError)
			return
		}
		txns, err = wallet.SendSiacoinsMulti(outputs)
		if err != nil {
			WriteError(w, Error{"error when calling /wallet/siacoins: " + err.Error()}, http.StatusInternalServerError)
			return
		}
	} else {
		// Single amount + destination.
		amount, ok := scanAmount(req.FormValue("amount"))
		if !ok {
			WriteError(w, Error{"could not read amount from POST call to /wallet/siacoins"}, http.StatusBadRequest)
			return
		}
		dest, err := scanAddress(req.FormValue("destination"))
		if err != nil {
			WriteError(w, Error{"could not read address from POST call to /wallet/siacoins"}, http.StatusBadRequest)
			return
		}
		feeIncluded, err := scanBool(req.FormValue("feeIncluded"))
		if err != nil {
			WriteError(w, Error{"could not read feeIncluded from POST call to /wallet/siacoins"}, http.StatusBadRequest)
			return
		}

		if feeIncluded {
			txns, err = wallet.SendSiacoinsFeeIncluded(amount, dest)
		} else {
			txns, err = wallet.SendSiacoins(amount, dest)
		}
		if err != nil {
			WriteError(w, Error{"error when calling /wallet/siacoins: " + err.Error()}, http.StatusInternalServerError)
			return
		}
	}

	var txids []types.TransactionID
	for _, txn := range txns {
		txids = append(txids, txn.ID())
	}
	WriteJSON(w, WalletSiacoinsPOST{
		Transactions:   txns,
		TransactionIDs: txids,
	})
}

// walletSweepSeedHandler handles API calls to /wallet/sweep/seed.
func walletSweepSeedHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	// Get the seed using the dictionary + phrase.
	seed, err := modules.DecodeBIP39Phrase(req.FormValue("seed"))
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/sweep/seed: " + err.Error()}, http.StatusBadRequest)
		return
	}

	coins, _, err := wallet.SweepSeed(seed)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/sweep/seed: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletSweepPOST{
		Coins: coins,
	})
}

// walletTransactionHandler handles API calls to /wallet/transaction/:id.
func walletTransactionHandler(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	// Parse the id from the url.
	var id types.TransactionID
	jsonID := "\"" + ps.ByName("id") + "\""
	err := json.Unmarshal([]byte(jsonID), &id)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/transaction/id: " + err.Error()}, http.StatusBadRequest)
		return
	}

	txn, ok, err := wallet.Transaction(id)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/transaction/id: " + err.Error()}, http.StatusBadRequest)
		return
	}
	if !ok {
		WriteError(w, Error{"error when calling /wallet/transaction/id  :  transaction not found"}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletTransactionGETid{
		Transaction: txn,
	})
}

// walletTransactionsHandler handles API calls to /wallet/transactions.
func walletTransactionsHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	startheightStr, endheightStr := req.FormValue("startheight"), req.FormValue("endheight")
	if startheightStr == "" || endheightStr == "" {
		WriteError(w, Error{"startheight and endheight must be provided to a /wallet/transactions call."}, http.StatusBadRequest)
		return
	}
	// Get the start and end blocks.
	start, err := strconv.ParseUint(startheightStr, 10, 64)
	if err != nil {
		WriteError(w, Error{"parsing integer value for parameter `startheight` failed: " + err.Error()}, http.StatusBadRequest)
		return
	}
	// Check if endheightStr is set to -1. If it is, we use MaxUint64 as the
	// end. Otherwise we parse the argument as an unsigned integer.
	var end uint64
	if endheightStr == "-1" {
		end = math.MaxUint64
	} else {
		end, err = strconv.ParseUint(endheightStr, 10, 64)
	}
	if err != nil {
		WriteError(w, Error{"parsing integer value for parameter `endheight` failed: " + err.Error()}, http.StatusBadRequest)
		return
	}
	confirmedTxns, err := wallet.Transactions(start, end)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/transactions: " + err.Error()}, http.StatusBadRequest)
		return
	}
	unconfirmedTxns, err := wallet.UnconfirmedTransactions()
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/transactions: " + err.Error()}, http.StatusBadRequest)
		return
	}

	WriteJSON(w, WalletTransactionsGET{
		ConfirmedTransactions:   confirmedTxns,
		UnconfirmedTransactions: unconfirmedTxns,
	})
}

// walletTransactionsAddrHandler handles API calls to
// /wallet/transactions/:addr.
func walletTransactionsAddrHandler(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	// Parse the address being input.
	jsonAddr := "\"" + ps.ByName("addr") + "\""
	var addr types.Address
	err := json.Unmarshal([]byte(jsonAddr), &addr)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/transactions: " + err.Error()}, http.StatusBadRequest)
		return
	}

	confirmedATs, err := wallet.AddressTransactions(addr)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/transactions: " + err.Error()}, http.StatusBadRequest)
		return
	}
	unconfirmedATs, err := wallet.AddressUnconfirmedTransactions(addr)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/transactions: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletTransactionsGETaddr{
		ConfirmedTransactions:   confirmedATs,
		UnconfirmedTransactions: unconfirmedATs,
	})
}

// walletUnlockHandler handles API calls to /wallet/unlock.
func walletUnlockHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	potentialKeys, _ := encryptionKeys(req.FormValue("encryptionpassword"))
	var err error
	for _, key := range potentialKeys {
		errChan := wallet.UnlockAsync(key)
		var unlockErr error
		select {
		case unlockErr = <-errChan:
		default:
		}
		if unlockErr == nil {
			WriteSuccess(w)
			return
		}
		err = modules.ComposeErrors(err, unlockErr)
	}
	WriteError(w, Error{"error when calling /wallet/unlock: " + err.Error()}, http.StatusBadRequest)
}

// walletChangePasswordHandler handles API calls to /wallet/changepassword
func walletChangePasswordHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var newKey modules.WalletKey
	newPassword := req.FormValue("newpassword")
	if newPassword == "" {
		WriteError(w, Error{"a password must be provided to newpassword"}, http.StatusBadRequest)
		return
	}

	h := blake2b.Sum256([]byte(newPassword))
	buf := make([]byte, 32 + 8)
	copy(buf[:32], h[:])
	binary.LittleEndian.PutUint64(buf[32:], 0)
	h = blake2b.Sum256(buf)
	key := types.NewPrivateKeyFromSeed(h[:])
	h = blake2b.Sum256(key[:])
	frand.Read(key[:])
	wk := make([]byte, len(h))
	copy(wk, h[:])
	frand.Read(h[:])
	newKey = modules.WalletKey(wk)

	originalKeys, seed := encryptionKeys(req.FormValue("encryptionpassword"))
	var err error
	for _, key := range originalKeys {
		keyErr := wallet.ChangeKey(key, newKey)
		if keyErr == nil {
			WriteSuccess(w)
			return
		}
		err = modules.ComposeErrors(err, keyErr)
	}
	seedErr := wallet.ChangeKeyWithSeed(seed, newKey)
	if seedErr == nil {
		WriteSuccess(w)
		return
	}
	err = modules.ComposeErrors(err, seedErr)
	WriteError(w, Error{"error when calling /wallet/changepassword: " + err.Error()}, http.StatusBadRequest)
	return
}

// walletVerifyPasswordHandler handles API calls to /wallet/verifypassword
func walletVerifyPasswordHandler(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	originalKeys, _ := encryptionKeys(req.FormValue("password"))
	var err error
	for _, key := range originalKeys {
		valid, keyErr := wallet.IsMasterKey(key)
		if keyErr == nil {
			WriteJSON(w, WalletVerifyPasswordGET{
				Valid: valid,
			})
			return
		}
		err = modules.ComposeErrors(err, keyErr)
	}
	WriteError(w, Error{"error when calling /wallet/verifypassword: " + err.Error()}, http.StatusBadRequest)
}

// walletVerifyAddressHandler handles API calls to /wallet/verify/address/:addr.
func walletVerifyAddressHandler(w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	addrString := ps.ByName("addr")

	err := new(types.Address).UnmarshalText([]byte(addrString))
	WriteJSON(w, WalletVerifyAddressGET{Valid: err == nil})
}

// walletUnlockConditionsHandlerGET handles GET calls to /wallet/unlockconditions.
func walletUnlockConditionsHandlerGET(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, ps httprouter.Params) {
	var addr types.Address
	err := addr.UnmarshalText([]byte(ps.ByName("addr")))
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/unlockconditions: " + err.Error()}, http.StatusBadRequest)
		return
	}
	uc, err := wallet.UnlockConditions(addr)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/unlockconditions: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletUnlockConditionsGET{
		UnlockConditions: uc,
	})
}

// walletUnlockConditionsHandlerPOST handles POST calls to /wallet/unlockconditions.
func walletUnlockConditionsHandlerPOST(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var params WalletUnlockConditionsPOSTParams
	err := json.NewDecoder(req.Body).Decode(&params)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}
	err = wallet.AddUnlockConditions(params.UnlockConditions)
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/unlockconditions: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}

// walletUnspentHandler handles API calls to /wallet/unspent.
func walletUnspentHandler(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	outputs, err := wallet.UnspentOutputs()
	if err != nil {
		WriteError(w, Error{"error when calling /wallet/unspent: " + err.Error()}, http.StatusInternalServerError)
		return
	}
	WriteJSON(w, WalletUnspentGET{
		Outputs: outputs,
	})
}

// walletSignHandler handles API calls to /wallet/sign.
func walletSignHandler(wt modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var params WalletSignPOSTParams
	err := json.NewDecoder(req.Body).Decode(&params)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}
	err = wt.SignTransaction(&params.Transaction, params.ToSign, wallet.FullCoveredFields())
	if err != nil {
		WriteError(w, Error{"failed to sign transaction: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletSignPOSTResp{
		Transaction: params.Transaction,
	})
}

// walletWatchHandlerGET handles GET calls to /wallet/watch.
func walletWatchHandlerGET(wallet modules.Wallet, w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	addrs, err := wallet.WatchAddresses()
	if err != nil {
		WriteError(w, Error{"failed to get watch addresses: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteJSON(w, WalletWatchGET{
		Addresses: addrs,
	})
}

// walletWatchHandlerPOST handles POST calls to /wallet/watch.
func walletWatchHandlerPOST(wallet modules.Wallet, w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	var wwpp WalletWatchPOST
	err := json.NewDecoder(req.Body).Decode(&wwpp)
	if err != nil {
		WriteError(w, Error{"invalid parameters: " + err.Error()}, http.StatusBadRequest)
		return
	}
	if wwpp.Remove {
		err = wallet.RemoveWatchAddresses(wwpp.Addresses, wwpp.Unused)
	} else {
		err = wallet.AddWatchAddresses(wwpp.Addresses, wwpp.Unused)
	}
	if err != nil {
		WriteError(w, Error{"failed to update watch set: " + err.Error()}, http.StatusBadRequest)
		return
	}
	WriteSuccess(w)
}
