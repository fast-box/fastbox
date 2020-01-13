// Copyright 2018 The go-hpb Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The go-hpb is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-hpb is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-hpb. If not, see <http://www.gnu.org/licenses/>.
package prometheus

import (
	"bytes"
	"math/big"
	"sync"
	"time"

	"github.com/hpb-project/sphinx/account"
	"github.com/hpb-project/sphinx/blockchain/state"
	"github.com/hpb-project/sphinx/blockchain/types"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/consensus"

	"github.com/hashicorp/golang-lru"
	"github.com/hpb-project/sphinx/blockchain/storage"
	"github.com/hpb-project/sphinx/common/log"
	"github.com/hpb-project/sphinx/config"
	"github.com/hpb-project/sphinx/network/p2p"
	"github.com/hpb-project/sphinx/network/rpc"
	"github.com/hpb-project/sphinx/node/db"

	//"strconv"
	"errors"
	"strings"
)

// constant parameter definition
const (
	checkpointInterval    = 1024 // voting interval
	inmemoryHistorysnaps  = 128
	inmemorySignatures    = 4096
	wiggleTime            = 1000 * time.Millisecond
	comCheckpointInterval = 2
	cadCheckpointInterval = 2
)

// Prometheus protocol constants.
var (
	epochLength   = uint64(30000)
	blockPeriod   = uint64(15)               // default block interval is 15 seconds
	uncleHash     = types.CalcUncleHash(nil) //
	diffInTurn    = big.NewInt(2)            // the node is in turn, and its diffcult number is 2
	diffNoTurn    = big.NewInt(1)            // the node is not in turn, and its diffcult number is 1
	reentryMux    sync.Mutex
	insPrometheus *Prometheus
)

type Prometheus struct {
	config *config.PrometheusConfig // Consensus config
	db     hpbdb.Database           // Database

	recents    *lru.ARCCache // the recent signature
	signatures *lru.ARCCache // the last signature

	proposals map[common.Address]bool // current proposals (hpb nodes)

	signer    common.Address
	randomStr string
	signFn    SignerFn     // Callback function
	lock      sync.RWMutex // Protects the signerHash fields
}

func New(config *config.PrometheusConfig, db hpbdb.Database) *Prometheus {

	conf := *config

	if conf.Epoch == 0 {
		conf.Epoch = epochLength
	}

	recents, _ := lru.NewARC(inmemoryHistorysnaps)
	signatures, _ := lru.NewARC(inmemorySignatures)

	return &Prometheus{
		config:     &conf,
		db:         db,
		recents:    recents,
		signatures: signatures,
		proposals:  make(map[common.Address]bool),
	}
}

// InstanceBlockChain returns the singleton of BlockChain.
func InstancePrometheus() *Prometheus {
	if nil == insPrometheus {
		reentryMux.Lock()
		if nil == insPrometheus {
			insPrometheus = New(&config.GetHpbConfigInstance().Prometheus, db.GetHpbDbInstance())
		}
		reentryMux.Unlock()
	}
	return insPrometheus
}

type SignerFn func(accounts.Account, []byte) ([]byte, error)

// Prepare function for Block
func (c *Prometheus) PrepareBlockHeader(chain consensus.ChainReader, header *types.Header, state *state.StateDB) error {

	header.Nonce = types.BlockNonce{}
	number := header.Number.Uint64()

	parentnum := number - 1
	parentheader := chain.GetHeaderByNumber(parentnum)
	if parentheader == nil {
		return errors.New("-----PrepareBlockHeader parentheader------ is nil")
	}

	// check the header
	if len(header.Extra) < consensus.ExtraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, consensus.ExtraVanity-len(header.Extra))...)
	}

	header.Extra = header.Extra[:consensus.ExtraVanity]

	// get all the hpb node address
	if number%consensus.HpbNodeCheckpointInterval == 0 {
	}

	header.Extra = append(header.Extra, make([]byte, consensus.ExtraSeal)...)
	header.MixDigest = common.Hash{}

	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}

	header.Time = new(big.Int).Add(parent.Time, new(big.Int).SetUint64(c.config.Period))

	if header.Time.Int64() < time.Now().Unix() {
		header.Time = big.NewInt(time.Now().Unix())
	}

	return nil
}

// generate blocks by giving the signature
func (c *Prometheus) GenBlockWithSig(chain consensus.ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error) {
	header := block.Header()

	log.Info("HPB Prometheus Seal is starting")

	number := header.Number.Uint64()
	if number == 0 {
		return nil, consensus.ErrUnknownBlock
	}
	// For 0-period chains, refuse to seal empty blocks (no reward but would spin sealing)
	if c.config.Period == 0 && len(block.Transactions()) == 0 {
		return nil, consensus.ErrWaitTransactions
	}

	c.lock.RLock()
	signer, signFn := c.signer, c.signFn

	log.Debug("GenBlockWithSig signer's address", "signer", signer.Hex(), "number", number)

	c.lock.RUnlock()

	delay := time.Unix(header.Time.Int64(), 0).Sub(time.Now())
	if delay < 0 {
		delay = 0
		header.Time = big.NewInt(time.Now().Unix())
	}
	// set delay time for out-turn hpb nodes
	log.Debug("Waiting for slot to sign and propagate", "delay", common.PrettyDuration(delay), "number", number)

	select {
	case <-stop:
		return nil, nil
	case <-time.After(delay):
	}

	header.Coinbase = signer

	// signing to get the signature
	sighash, err := signFn(accounts.Account{Address: signer}, consensus.SigHash(header).Bytes())
	if err != nil {
		return nil, err
	}

	// put the signature result to the Extra field
	copy(header.Extra[len(header.Extra)-consensus.ExtraSeal:], sighash)

	return block.WithSeal(header), nil
}

// Authorize injects a private key into the consensus engine to mint new blocks
// with.
func (c *Prometheus) Authorize(signer common.Address, signFn SignerFn) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.signer = signer
	c.signFn = signFn
}

// retrieve the signer from the signature
func (c *Prometheus) Author(header *types.Header) (common.Address, error) {
	return consensus.Ecrecover(header, c.signatures)
}

func (c *Prometheus) Finalize(chain consensus.ChainReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	err := c.CalculateRewards(chain, state, header, uncles)
	if err != nil {
		log.Info("CalculateRewards return", "info", err)
		if config.GetHpbConfigInstance().Node.TestMode != 1 && consensus.IgnoreRetErr != true {
			return nil, err
		}
	}
	header.Root = state.IntermediateRoot(true)
	header.UncleHash = types.CalcUncleHash(nil)
	return types.NewBlock(header, txs, nil, receipts), nil
}

func (c *Prometheus) CalculateRewards(chain consensus.ChainReader, state *state.StateDB, header *types.Header, uncles []*types.Header) error {

	return nil
}

// API for the terminal
func (c *Prometheus) APIs(chain consensus.ChainReader) []rpc.API {
	return []rpc.API{{
		Namespace: "prometheus",
		Version:   "1.0",
		Service:   &API{chain: chain, prometheus: c},
		Public:    false,
	}}
}

func (c *Prometheus) GetVoteRes(chain consensus.ChainReader, header *types.Header, state *state.StateDB) (error, *big.Int, map[common.Address]big.Int) {
	return errors.New("unsupported function"), nil, nil
}

type BandWithStatics struct {
	AverageValue uint64
	Num          uint64
}

func (c *Prometheus) GetAllBalances(addrlist []common.Address, state *state.StateDB) (map[common.Address]big.Int, error) {

	if addrlist == nil || len(addrlist) == 0 || state == nil {
		return nil, consensus.ErrBadParam
	}

	mapBalance := make(map[common.Address]big.Int)
	arrayaddrwith := make([]common.Address, 0, len(addrlist))
	for _, v := range addrlist {
		arrayaddrwith = append(arrayaddrwith, v)
	}
	for _, v := range arrayaddrwith {
		mapBalance[v] = *state.GetBalance(v)
		log.Trace("GetBalanceRes ranking", "string addr", common.Bytes2Hex(v[:]), "state get", state.GetBalance(v))
	}
	return mapBalance, nil
}

/*
 *  GetAllBalancesByCoin
 *
 *  In:   addrlist  hpnode addresses from contract
		  coinlist  hpHolderCoin addresses from contract,Correspond addrlist by index
		  state     a pointer to stateDB
 *  Out:  mapBalance   hpnode address->hpHolderCoin address's balance
 *
 *  This function will get the balance of the coinaddress corresponding to the hpnode address.
 *  To separate Coinbase account and holdercoin address
*/
func (c *Prometheus) GetAllBalancesByCoin(addrlist []common.Address, coinlist []common.Address, state *state.StateDB) (map[common.Address]big.Int, error) {

	if addrlist == nil || len(addrlist) == 0 || state == nil || len(addrlist) != len(coinlist) {
		return nil, consensus.ErrBadParam
	}

	mapBalance := make(map[common.Address]big.Int)
	arrayaddrwith := make([]common.Address, 0, len(addrlist))
	for _, v := range addrlist {
		arrayaddrwith = append(arrayaddrwith, v)
	}
	for i, v := range arrayaddrwith {
		mapBalance[v] = *state.GetBalance(coinlist[i])
		log.Trace("GetBalanceRes ranking", "string addr", common.Bytes2Hex(v[:]), "state get", state.GetBalance(coinlist[i]))
	}
	return mapBalance, nil
}
func (c *Prometheus) GetRankingRes(voteres map[common.Address]big.Int, addrlist []common.Address) (map[common.Address]int, error) {

	if addrlist == nil || len(addrlist) == 0 {
		return nil, consensus.ErrBadParam
	}

	mapVotes := make(map[common.Address]*big.Int)
	arrayaddrwith := make([]common.Address, 0, len(addrlist))
	for _, v := range addrlist {
		arrayaddrwith = append(arrayaddrwith, v)
	}
	for _, v := range arrayaddrwith {
		if votes, ok := voteres[v]; ok {
			mapVotes[v] = &votes
		} else {
			mapVotes[v] = big.NewInt(0)
		}
		log.Trace("GetAllVoteRes ranking", "string addr", common.Bytes2Hex(v[:]), "votes", mapVotes[v])
	}

	arrayaddrlen := len(arrayaddrwith)
	for i := 0; i <= arrayaddrlen-1; i++ {
		for j := arrayaddrlen - 1; j >= i+1; j-- {
			if mapVotes[arrayaddrwith[j-1]].Cmp(mapVotes[arrayaddrwith[j]]) < 0 {
				arrayaddrwith[j-1], arrayaddrwith[j] = arrayaddrwith[j], arrayaddrwith[j-1]
			}
		}
	}

	mapintaddr := make(map[int][]common.Address)
	offset := 0
	tempaddrslice := make([]common.Address, 0, 151)
	tempaddrslice = append(tempaddrslice, arrayaddrwith[0])
	mapintaddr[0] = tempaddrslice

	//set map, key is int ,value is []addr
	for i := 1; i < len(arrayaddrwith); i++ {
		if mapv, ok := mapintaddr[offset]; ok {
			if mapVotes[arrayaddrwith[i]].Cmp(mapVotes[arrayaddrwith[i-1]]) == 0 {
				mapv = append(mapv, arrayaddrwith[i])
				mapintaddr[offset] = mapv
			} else {
				offset++
				tempaddrslice := make([]common.Address, 0, 151)
				tempaddrslice = append(tempaddrslice, arrayaddrwith[i])
				mapintaddr[offset] = tempaddrslice
			}
		} else {
			tempaddrslice := make([]common.Address, 0, 151)
			tempaddrslice = append(tempaddrslice, arrayaddrwith[i])
			mapintaddr[offset] = tempaddrslice
		}
	}

	res := make(map[common.Address]int)
	for k, v := range mapintaddr {
		for _, addr := range v {
			res[addr] = k
		}
	}

	return res, nil
}

func (c *Prometheus) GetSinger() common.Address {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.signer
}

func (c *Prometheus) GetNodeinfoFromContract(chain consensus.ChainReader, header *types.Header, state *state.StateDB) (error, []p2p.HwPair) {
	return errors.New("unsupported function"), nil
}

func PreDealNodeInfo(pairs []p2p.HwPair) (error, []p2p.HwPair) {
	if nil == pairs {
		return consensus.Errnilparam, nil
	}
	res := make([]p2p.HwPair, 0, len(pairs))
	log.Trace("PrepareBlockHeader from p2p.PeerMgrInst().HwInfo() return", "value", pairs) //for test
	for i := 0; i < len(pairs); i++ {
		if len(pairs[i].Adr) != 0 {
			pairs[i].Adr = strings.Replace(pairs[i].Adr, " ", "", -1)
			res = append(res, pairs[i])
		}
	}
	if 0 == len(res) {
		return errors.New("input node info addr all zero"), nil
	}
	log.Trace(">>>>>>>>>>>>> PreDealNodeInfo <<<<<<<<<<<<<<<<", "res", res, "length", len(res))

	return nil, res
}

/*
 *  GetCoinAddressFromNewContract
 *
 *  This function will get all coinbase addresses and holdercoin addresses.
 *  coinbase address and holdercoin address correspond by index
 */
func (c *Prometheus) GetCoinAddressFromNewContract(chain consensus.ChainReader, header *types.Header, state *state.StateDB) (error, []common.Address, []common.Address) {
	return errors.New("unsupported function"), nil, nil
}

/*
 *  GetVoteResFromNewContract
 *
 *  This function will get voteresult by contract.
 */
func (c *Prometheus) GetVoteResFromNewContract(chain consensus.ChainReader, header *types.Header, state *state.StateDB) (error, map[common.Address]big.Int) {
	return errors.New("unsupported function"), nil
}

/*
 *  GetNodeinfoFromNewContract
 *
 *  This function will get all boenodes by contract.
 */
func (c *Prometheus) GetNodeinfoFromNewContract(chain consensus.ChainReader, header *types.Header, state *state.StateDB) (error, []p2p.HwPair) {
	return errors.New("unsupported function"), nil
}
