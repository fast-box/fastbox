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
	"github.com/hpb-project/sphinx/consensus/snapshots"
	"github.com/hpb-project/sphinx/consensus/voting"
	"github.com/hpb-project/sphinx/network/p2p"
	"github.com/hpb-project/sphinx/network/p2p/discover"
	"github.com/hpb-project/sphinx/network/rpc"
	"github.com/hpb-project/sphinx/node/db"

	//"strconv"
	"errors"
	"math"
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

	snap, err := voting.GetHpbNodeSnap(c.db, c.recents, c.signatures, c.config, chain, number, header.ParentHash, nil)
	if err != nil {
		return err
	}
	SetNetNodeType(snap)

	if 0 == len(snap.Signers) {
		return errors.New("prepare header get hpbnodesnap success, but snap`s singers is 0")
	}
	header.Difficulty = diffNoTurn
	if snap.CalculateCurrentMinerorigin(new(big.Int).SetBytes(header.HardwareRandom).Uint64(), c.GetSinger()) {
		header.Difficulty = diffInTurn
	}

	if header.Difficulty == diffNoTurn {
		// check mine backup block.
		var chooseBackupMiner = 100
		if header.Number.Int64() > int64(chooseBackupMiner) {
			signersgenblks := make([]types.Header, 0, chooseBackupMiner)
			for i := uint64(0); i < uint64(chooseBackupMiner); i++ {
				oldHeader := chain.GetHeaderByNumber(number - i - 1)
				if oldHeader != nil {
					signersgenblks = append(signersgenblks, *oldHeader)
				}
			}
			if !snap.CalculateBackupMiner(header.Number.Uint64(), c.GetSinger(), signersgenblks) {
				return errors.New("Not in turn")
			}
		} else {
			return errors.New("Not in turn")
		}
	}

	c.lock.RLock()

	if cadWinner, err := c.GetSelectPrehp(state, chain, header, number, false); nil == err {

		if cadWinner == nil || len(cadWinner) != 2 {
			//if no peers, add itself Coinbase to CandAddress and ComdAddress, or when candidate nodes is less len(hpbsnap.signers), the zero address will become the hpb node
			header.CandAddress = header.Coinbase
			header.ComdAddress = header.Coinbase
			header.VoteIndex = new(big.Int).SetUint64(0)
		} else {
			header.CandAddress = cadWinner[0].Address
			header.VoteIndex = new(big.Int).SetUint64(cadWinner[0].VoteIndex)
			header.ComdAddress = cadWinner[1].Address
		}
		log.Trace(">>>>>>>>>>>>>header.CandAddress<<<<<<<<<<<<<<<<<", "addr", header.CandAddress, "number", number) //for test

	} else {
		c.lock.RUnlock()
		return err
	}
	c.lock.RUnlock()

	// check the header
	if len(header.Extra) < consensus.ExtraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, consensus.ExtraVanity-len(header.Extra))...)
	}

	header.Extra = header.Extra[:consensus.ExtraVanity]

	// get all the hpb node address
	if number%consensus.HpbNodeCheckpointInterval == 0 {
		for _, signer := range snap.GetHpbNodes() {
			header.Extra = append(header.Extra, signer[:]...)
		}
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

	snap, err := voting.GetHpbNodeSnap(c.db, c.recents, c.signatures, c.config, chain, number, header.ParentHash, nil)

	SetNetNodeType(snap)

	if err != nil {
		return nil, err
	}

	if _, authorized := snap.Signers[signer]; !authorized {
		return nil, consensus.ErrUnauthorized
	}

	delay := time.Unix(header.Time.Int64(), 0).Sub(time.Now())
	if delay < 0 {
		delay = 0
		header.Time = big.NewInt(time.Now().Unix())
	}
	// set delay time for out-turn hpb nodes
	if header.Difficulty.Cmp(diffNoTurn) == 0 {
		//It's not our turn explicitly to sign, delay it a bit
		currentminer := new(big.Int).SetBytes(header.HardwareRandom).Uint64() % uint64(len(snap.Signers)) //miner position
		myoffset := snap.GetOffset(header.Number.Uint64(), signer)
		distance := int(math.Abs(float64(int64(myoffset) - int64(currentminer))))
		if distance > len(snap.Signers)/2 {
			distance = len(snap.Signers) - distance
		}
		delay = time.Second*time.Duration(c.config.Period*2) + time.Duration(distance)*wiggleTime
	}

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

func SetNetNodeType(snapa *snapshots.HpbNodeSnap) error {
	addresses := snapa.GetHpbNodes()

	if p2p.PeerMgrInst().GetLocalType() == discover.PreNode || p2p.PeerMgrInst().GetLocalType() == discover.HpNode {
		newlocaltyp := discover.PreNode
		if flag := FindHpbNode(p2p.PeerMgrInst().DefaultAddr(), addresses); flag {
			newlocaltyp = discover.HpNode
		}
		if p2p.PeerMgrInst().GetLocalType() != newlocaltyp {
			p2p.PeerMgrInst().SetLocalType(newlocaltyp)
		}
	}

	peers := p2p.PeerMgrInst().PeersAll()
	for _, peer := range peers {
		switch peer.RemoteType() {
		case discover.PreNode:
			if flag := FindHpbNode(peer.Address(), addresses); flag {
				peer.SetRemoteType(discover.HpNode)
			}
		case discover.HpNode:
			if flag := FindHpbNode(peer.Address(), addresses); !flag {
				peer.SetRemoteType(discover.PreNode)
			}
		default:
			break
		}
	}
	return nil
}

func FindHpbNode(address common.Address, addresses []common.Address) bool {
	for _, addresstemp := range addresses {
		if addresstemp == address {
			return true
		}
	}
	return false
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
	if header.Number.Uint64()%consensus.HpbNodeCheckpointInterval != 0 && header.Number.Uint64() > consensus.StageNumberIV {
		log.Debug("CalculateRewards number is not 200 mulitple, do not reward", "number", header.Number)
		return nil
	}
	// Select the correct block reward based on chain progression
	var bigIntblocksoneyear = new(big.Int)
	secondsoneyesr := big.NewFloat(60 * 60 * 24 * 365)                         //seconds in one year
	secondsoneyesr.Quo(secondsoneyesr, big.NewFloat(float64(c.config.Period))) //blocks mined by miners in one year

	secondsoneyesr.Int(bigIntblocksoneyear) //from big.Float to big.Int

	bigrewards := big.NewFloat(float64(100000000 * 0.03)) //hpb coins additional issue one year
	bigrewards.Mul(bigrewards, big.NewFloat(float64(consensus.Nodenumfirst)))
	bigrewards.Quo(bigrewards, big.NewFloat(float64(consensus.Nodenumfirst)))

	bigIntblocksoneyearfloat := new(big.Float)
	bigIntblocksoneyearfloat.SetInt(bigIntblocksoneyear)      //from big.Int to big.Float
	A := bigrewards.Quo(bigrewards, bigIntblocksoneyearfloat) //calc reward mining one block

	if header.Number.Uint64() >= consensus.StageNumberIII {
		seconds := big.NewInt(0)
		tempheader := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
		fromtime := tempheader.Time

		for l := 0; l < 200; l++ {
			tempheader = chain.GetHeader(tempheader.ParentHash, tempheader.Number.Uint64()-1)
		}

		seconds.Sub(fromtime, tempheader.Time)
		secondsfloat := big.NewFloat(0)
		secondsfloat.SetInt(seconds)
		if header.Number.Uint64() <= consensus.StageNumberIV {
			secondsfloat.Quo(secondsfloat, big.NewFloat(200))
		}

		A.Quo(A, big.NewFloat(float64(c.config.Period)))
		A.Mul(A, secondsfloat)
	}
	log.Trace("CalculateRewards calc reward mining one block", "hpb coin", A)

	//mul 2/3
	A.Mul(A, big.NewFloat(2))
	A.Quo(A, big.NewFloat(3))

	//new two vars using below codes
	var bigA23 = new(big.Float) //2/3 one block reward
	var bigA13 = new(big.Float) //1/3 one block reward
	bigA23.Set(A)
	bigA13.Set(A)
	if consensus.StageNumberVI < header.Number.Uint64() {
		bigA13.Quo(bigA13, big.NewFloat(200.0))
	}

	bighobBlockReward := A.Mul(A, big.NewFloat(0.35)) //reward hpb coin for hpb nodes

	ether2weis := big.NewInt(10)
	ether2weis.Exp(ether2weis, big.NewInt(18), nil) //one hpb coin to weis

	ether2weisfloat := new(big.Float)
	ether2weisfloat.SetInt(ether2weis)
	bighobBlockRewardwei := bighobBlockReward.Mul(bighobBlockReward, ether2weisfloat) //reward weis for hpb nodes

	number := header.Number.Uint64()
	if number == 0 {
		return consensus.ErrUnknownBlock
	}

	var hpsnap *snapshots.HpbNodeSnap
	var err error
	if number < consensus.StageNumberII {
		finalhpbrewards := new(big.Int)
		bighobBlockRewardwei.Int(finalhpbrewards) //from big.Float to big.Int
		state.AddBalance(header.Coinbase, finalhpbrewards)
	} else {
		if hpsnap, err = voting.GetHpbNodeSnap(c.db, c.recents, c.signatures, c.config, chain, number, header.ParentHash, nil); err == nil {
			bighobBlockRewardwei.Quo(bighobBlockRewardwei, big.NewFloat(float64(len(hpsnap.Signers))))
			finalhpbrewards := new(big.Int)
			bighobBlockRewardwei.Int(finalhpbrewards) //from big.Float to big.Int
			for _, v := range hpsnap.GetHpbNodes() {
				state.AddBalance(v, finalhpbrewards)
				log.Trace(">>>>>>>>>reward hpnode in the snapshot<<<<<<<<<<<<", "addr", v, "reward value", finalhpbrewards)
			}
		} else {
			return err
		}
	}

	if csnap, err := voting.GetCadNodeSnap(c.db, c.recents, chain, number, header.ParentHash); err == nil {
		if csnap != nil {
			if number < consensus.StageNumberII {
				bigA23.Mul(bigA23, big.NewFloat(0.65))
				canBlockReward := bigA23.Quo(bigA23, big.NewFloat(float64(len(csnap.VotePercents)))) //calc average reward coin part about cadidate nodes

				bigcadRewardwei := new(big.Float)
				bigcadRewardwei.SetInt(ether2weis)
				bigcadRewardwei.Mul(bigcadRewardwei, canBlockReward) //calc average reward weis part about candidate nodes

				cadReward := new(big.Int)
				bigcadRewardwei.Int(cadReward) //from big.Float to big.Int

				for caddress, _ := range csnap.VotePercents {
					state.AddBalance(caddress, cadReward) //reward every cad node average
				}
			} else if len(csnap.CanAddresses) > 0 {
				bigA23.Mul(bigA23, big.NewFloat(0.65))
				canBlockReward := bigA23.Quo(bigA23, big.NewFloat(float64(len(csnap.CanAddresses)))) //calc average reward coin part about cadidate nodes

				bigcadRewardwei := new(big.Float)
				bigcadRewardwei.SetInt(ether2weis)
				bigcadRewardwei.Mul(bigcadRewardwei, canBlockReward) //calc average reward weis part about candidate nodes

				cadReward := new(big.Int)
				bigcadRewardwei.Int(cadReward) //from big.Float to big.Int

				for _, caddress := range csnap.CanAddresses {
					state.AddBalance(caddress, cadReward) //reward every cad node average
					log.Trace("<<<<<<<<<<<<<<<reward prenode in the snapshot>>>>>>>>>>", "addr", caddress, "reward value", cadReward)
				}
			}

			if number%consensus.HpbNodeCheckpointInterval == 0 && number <= consensus.NewContractVersion && number >= consensus.StageNumberII {
				var errreward error
				loopcount := 3
			GETCONTRACTLOOP:
				if errreward = c.rewardvotepercentcad(chain, header, state, bigA13, ether2weisfloat, csnap, hpsnap); errreward != nil {
					log.Info("rewardvotepercent get contract fail", "info", errreward)
					loopcount -= 1
					if 0 != loopcount {
						goto GETCONTRACTLOOP
					}
				}
				return errreward
			}
			if number%consensus.HpbNodeCheckpointInterval == 0 && number > consensus.NewContractVersion {
				var errreward error
				loopcount := 3
				for i := 0; i < loopcount; i++ {
					errreward = c.rewardvotepercentcadByNewContrac(chain, header, state, bigA13, ether2weisfloat, csnap, hpsnap)
					if errreward == nil {
						break
					}
				}
				return errreward
			}
		}
	} else {
		return err
	}
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

func (c *Prometheus) rewardvotepercentcad(chain consensus.ChainReader, header *types.Header, state *state.StateDB, bigA13 *big.Float, ether2weisfloat *big.Float, csnap *snapshots.CadNodeSnap, hpsnap *snapshots.HpbNodeSnap) error {
	return errors.New("unsupported function")
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

func (c *Prometheus) rewardvotepercentcadByNewContrac(chain consensus.ChainReader, header *types.Header, state *state.StateDB, bigA13 *big.Float, ether2weisfloat *big.Float, csnap *snapshots.CadNodeSnap, hpsnap *snapshots.HpbNodeSnap) error {

	if csnap == nil {
		return errors.New("input param csnap is nil")
	}
	if hpsnap == nil {
		return errors.New("input param hpsnap is nil")
	}
	err, voteres := c.GetVoteResFromNewContract(chain, header, state)
	if err != nil {
		return err
	}
	VotePercents := make(map[common.Address]int64)
	for _, v := range csnap.CanAddresses {
		VotePercents[v] = 1
	}

	for addr := range voteres {
		_, ok1 := VotePercents[addr]
		_, ok2 := hpsnap.Signers[addr]
		if !ok1 && !ok2 {
			delete(voteres, addr)
		}
	}

	// get all the voting result
	votecounts := new(big.Int)
	for _, votes := range voteres {
		votecounts.Add(votecounts, &votes)
	}

	if votecounts.Cmp(big.NewInt(0)) == 0 {
		return nil
	}
	votecountsfloat := new(big.Float)
	votecountsfloat.SetInt(votecounts)

	bigA13.Quo(bigA13, big.NewFloat(2))
	bigA13.Mul(bigA13, ether2weisfloat)
	bigA13.Mul(bigA13, big.NewFloat(float64(consensus.HpbNodeCheckpointInterval))) //mul interval number
	log.Trace("Reward vote", "totalvote", votecountsfloat, "total reawrd", bigA13)
	for addr, votes := range voteres {
		tempaddrvotefloat := new(big.Float)
		tempreward := new(big.Int)
		tempaddrvotefloat.SetInt(&votes)
		tempaddrvotefloat.Quo(tempaddrvotefloat, votecountsfloat)
		log.Trace("Reward percent", "votes", votes, "percent", tempaddrvotefloat)
		tempaddrvotefloat.Mul(tempaddrvotefloat, bigA13)
		tempaddrvotefloat.Int(tempreward)
		state.AddBalance(addr, tempreward) //reward every cad node by vote percent
		log.Trace("++++++++++reward node with the vote contract++++++++++++", "addr", addr, "reward float", tempaddrvotefloat, "reward value", tempreward)
	}

	return nil
}
